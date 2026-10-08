import {createSecureClient,OrderSide,OrderType,relayerApiKey} from '@polymarket/client';
import {fetchBalanceAllowance} from '@polymarket/client/actions';
import {privateKey} from '@polymarket/client/viem';
import {AssetType} from '@polymarket/bindings/clob';
import {access,readFile,rename,unlink,writeFile,mkdir} from 'node:fs/promises';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {validateControl,validateIntent,positionExposure,blocksSellRetry,groupOccupied} from './executor-core.mjs';
import {runExitEngine,definiteReject} from './exit-engine.mjs';
import {executionLock} from './execution-lock.mjs';
import {entryLiquidity,reentryGuard} from './entry-guards.mjs';
import {summarizeFills} from './accounting.mjs';
const open=path=>executionLock(path);

const projectRoot=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const dataRoot=resolve(projectRoot,'data');
const secretsRoot=resolve(projectRoot,'.secrets');
const jsonReplacer=(_,value)=>typeof value==='bigint'?value.toString():value;

async function loadJSON(path,fallback) {
 try {
  // Windows PowerShell 5 writes UTF-8 files with a BOM. Accept it at the
  // execution boundary so control files remain readable after local scripts
  // update them.
  const text=(await readFile(path,'utf8')).replace(/^\uFEFF/,'');
  return JSON.parse(text);
 } catch(error) {
  if(error.code==='ENOENT') return fallback;
  throw error;
 }
}
async function atomicJSON(path,value) { await mkdir(dirname(path),{recursive:true});const temp=`${path}.tmp`;await writeFile(temp,JSON.stringify(value,jsonReplacer,2),{mode:0o600});await rename(temp,path); }
function credentials() {
 const wallet=process.env.POLYMARKET_WALLET_ADDRESS?.trim();
 const address=process.env.RELAYER_API_KEY_ADDRESS?.trim();
 const key=process.env.RELAYER_API_KEY?.trim();
 let secret=process.env.SIGNER_PRIVATE_KEY?.trim();
 if (/^[0-9a-fA-F]{64}$/.test(secret||'')) secret=`0x${secret}`;
 if (!/^0x[0-9a-fA-F]{40}$/.test(wallet||'')||!/^0x[0-9a-fA-F]{40}$/.test(address||'')||!key||!/^0x[0-9a-fA-F]{64}$/.test(secret||'')) throw new Error('wallet environment is incomplete');
 return {wallet,address,key,secret};
}
async function secureClient() { const c=credentials();return createSecureClient({wallet:c.wallet,signer:privateKey(c.secret),apiKey:relayerApiKey({key:c.key,address:c.address})}); }
async function firstPage(paginated) { const items=[];for await (const page of paginated) items.push(...(page.items||[]));return items; }
async function status() {
 const client=await secureClient();
 const [balance,orders,positions]=await Promise.all([
  fetchBalanceAllowance(client,{assetType:AssetType.COLLATERAL}),
  firstPage(client.listOpenOrders()),
  firstPage(client.listPositions()),
 ]);
 const positionSummary=positions.map(p=>({asset_id:p.assetId,current_size:Number(p.currentSize),avg_price:Number(p.avgPrice),current_value:Number(p.currentValue),title:p.title||'',outcome:p.outcome||'',status:p.status}));
 const orderSummary=orders.map(o=>({order_id:o.id||o.orderId||'',asset_id:o.assetId||o.tokenId||'',market_id:o.market||o.marketId||'',side:o.side||'',price:Number(o.price),size:Number(o.originalSize||o.size),size_matched:Number(o.sizeMatched||0),status:o.status||'OPEN'}));
 const report={checked_at:new Date().toISOString(),account:client.account,balance,open_order_count:orders.length,position_count:positions.length,positions:positionSummary,orders:orderSummary,trading_position_exposure_dollars:positionExposure(positions),exit_status:await loadJSON(resolve(dataRoot,'live-exit-status.json'),null),execution_last:await loadJSON(resolve(dataRoot,'live-runner-last.json'),null),live_control:await loadJSON(resolve(dataRoot,'live-control.json'),{enabled:false})};
 await atomicJSON(resolve(dataRoot,'live-status.json'),report);
 try{
  const ledger=await loadJSON(resolve(dataRoot,'live-ledger.json'),{attempts:[]});
  const oldest=ledger.attempts.filter(a=>a.order_id).reduce((n,a)=>Math.min(n,Date.parse(a.created_at)),Date.now());
  const trades=await firstPage(client.listAccountTrades({after:String(Math.floor(oldest/1000)-60)}));
  report.accounting={checked_at:new Date().toISOString(),orders:summarizeFills(ledger.attempts,trades)};
  await atomicJSON(resolve(dataRoot,'live-accounting.json'),report.accounting);
 }catch{report.accounting=await loadJSON(resolve(dataRoot,'live-accounting.json'),null);report.accounting_error='Fill history unavailable; previous accounting retained';}
 report.execution_by_market=await loadJSON(resolve(dataRoot,'live-execution-results.json'),{});
 await atomicJSON(resolve(dataRoot,'live-status.json'),report);
 console.log('Live gateway status written. No order sent.');
}
async function execute(intentPath) {
 const absolute=resolve(intentPath||'');
 if (!absolute.startsWith(dataRoot+'\\')&&!absolute.startsWith(dataRoot+'/')) throw new Error('intent must be inside data directory');
 const intent=validateIntent(await loadJSON(absolute,null));
 const control=validateControl(await loadJSON(resolve(dataRoot,'live-control.json'),null));
 if(intent.max_spend_dollars>control.maximum_order_dollars) throw new Error('order exceeds authorized fee-inclusive cap');
 try { await access(resolve(dataRoot,'STOP'));throw new Error('STOP marker exists'); } catch (error) { if (error.code!=='ENOENT') throw error; }
 const geo=await fetch('https://polymarket.com/api/geoblock',{signal:AbortSignal.timeout(10000)}).then(r=>{if(!r.ok)throw new Error('geoblock check unavailable');return r.json();});
 if (geo.blocked!==false) throw new Error('live execution blocked by location check');
 const book=await fetch(`https://clob.polymarket.com/book?token_id=${encodeURIComponent(intent.asset_id)}`,{signal:AbortSignal.timeout(10000)}).then(r=>{if(!r.ok)throw new Error('fresh order book unavailable');return r.json();});
 const bookTime=Number(book.timestamp);
 if (!Number.isFinite(bookTime)||Math.abs(Date.now()-bookTime)>60000) throw new Error('order book is stale');
 const asks=(book.asks||[]).map(x=>({price:Number(x.price),size:Number(x.size)})).filter(x=>Number.isFinite(x.price)&&Number.isFinite(x.size)&&x.price>0&&x.price<1&&x.size>0).sort((a,b)=>a.price-b.price);
 if (!asks.length||asks[0].price>intent.max_price) throw new Error('best ask exceeds max price');
 const affordableShares=intent.amount_dollars/asks[0].price;
 if (affordableShares<Number(book.min_order_size||0)) throw new Error('exchange minimum shares exceed $1 cap');
 const executableDollars=asks.filter(x=>x.price<=intent.max_price).reduce((sum,x)=>sum+x.price*x.size,0);
 if (executableDollars+1e-9<intent.amount_dollars) throw new Error('insufficient fresh ask depth');
 await mkdir(secretsRoot,{recursive:true});
 const lock=await open(resolve(secretsRoot,'execution.lock'),'wx',0o600);
 try {
  const ledgerPath=resolve(dataRoot,'live-ledger.json');
  const ledger=await loadJSON(ledgerPath,{version:1,attempts:[]});
  const today=new Date().toISOString().slice(0,10);
  if (ledger.attempts.some(x=>x.intent_id===intent.intent_id&&x.status!=='REJECTED')) throw new Error('duplicate intent');
  const client=await secureClient();
  const [openOrders,positions]=await Promise.all([firstPage(client.listOpenOrders()),firstPage(client.listPositions())]);
  const latest=await loadJSON(resolve(dataRoot,'latest.json'),null),tokenGroups=new Map();let group;
  for(const r of latest?.reports||[]){const g=r.rule.station_icao+'|'+r.rule.local_date;for(const s of r.signals||[]){tokenGroups.set(String(s.yes_token_id),g);if(s.market_id===intent.market_id)group=g}}
  if(!group)throw new Error('dated city market group unavailable');
  const currentReport=latest?.reports?.find(r=>r.signals?.some(s=>String(s.yes_token_id)===String(intent.asset_id)));
  const currentSignal=currentReport?.signals.find(s=>String(s.yes_token_id)===String(intent.asset_id));
  if(!latest||Math.abs(Date.now()-Date.parse(latest.generated_at))>120000||currentSignal?.action!=='BUY_LIVE'||!currentSignal.new_entry_allowed||!currentReport.distribution?.calibration_ready||currentReport.distribution.probability_status!=='CALIBRATED'||!(currentSignal.net_edge>0))throw new Error('latest live entry eligibility unavailable or withdrawn');
  const rate=Number(currentSignal.fee_per_share)/(Number(currentSignal.best_ask)*(1-Number(currentSignal.best_ask)));
  const freshEdge=Number(currentSignal.net_edge)+Number(currentSignal.best_ask)-intent.max_price+Number(currentSignal.fee_per_share)-rate*intent.max_price*(1-intent.max_price);
  if(!Number.isFinite(freshEdge)||freshEdge<=0)throw new Error('fresh executable price removes net edge');
  const entryCheck=entryLiquidity({bids:book.bids||[],entry:intent.max_price,shares:affordableShares,rate});
  reentryGuard({attempts:ledger.attempts,group,tokenGroups,signal:currentSignal,snapshotAt:latest.generated_at});
  const recentReject=ledger.attempts.filter(a=>a.side==='BUY'&&a.group_key===group&&a.status==='REJECTED').at(-1);
  if(recentReject&&Date.now()-Date.parse(recentReject.updated_at||recentReject.created_at)<30000)throw new Error('buy rejection cooldown; reevaluate after 30 seconds');
  if(groupOccupied(group,tokenGroups,positions,openOrders,ledger.attempts))throw new Error('city/date already has a position or pending order');
  if (openOrders.length>=control.maximum_open_orders) throw new Error('maximum open orders reached');
  const activePositionExposure=positionExposure(positions);
  const activePositions=positions.filter(p=>Number(p.currentSize)>0&&String(p.status||'').toUpperCase()!=='REDEEMABLE');
  const reservedExposure=openOrders.filter(o=>String(o.side).toUpperCase()==='BUY').reduce((sum,o)=>sum+Math.max(0,Number(o.originalSize||o.size)-Number(o.sizeMatched||0))*Math.max(0,Number(o.price)||0)*1.10,0);
  const unresolved=ledger.attempts.filter(a=>a.side==='BUY'&&['SUBMITTING','SUBMISSION_UNCERTAIN','PREPARING','POSTED','LIVE','OPEN','DELAYED'].includes(String(a.status).toUpperCase())&&!positions.some(p=>String(p.assetId)===String(a.asset_id)&&Number(p.currentSize)>0)&&!openOrders.some(o=>String(o.assetId||o.tokenId)===String(a.asset_id)));
  if(openOrders.length+unresolved.length>=control.maximum_open_orders)throw new Error('maximum open or unresolved buy requests reached');
  const uncertainReserve=unresolved.reduce((sum,a)=>sum+Number(a.max_spend_dollars||1.10),0);
  if(new Set(activePositions.map(p=>tokenGroups.get(String(p.assetId))||String(p.assetId))).size+openOrders.filter(o=>String(o.side).toUpperCase()==='BUY').length+unresolved.length>=control.maximum_simultaneous_positions)throw new Error('maximum simultaneous positions reached');
  if (activePositionExposure+reservedExposure+uncertainReserve+intent.max_spend_dollars>control.maximum_total_exposure_dollars+1e-9) throw new Error('maximum total exposure reached');
  const balance=await fetchBalanceAllowance(client,{assetType:AssetType.COLLATERAL});
  if (Number(balance.balance)/1e6<intent.max_spend_dollars) throw new Error('insufficient collateral balance');
  const attempt={group_key:group,intent_id:intent.intent_id,market_id:intent.market_id,asset_id:intent.asset_id,side:'BUY',day:today,max_spend_dollars:intent.max_spend_dollars,status:'SUBMITTING',model_probability:intent.model_probability,entry_price:asks[0].price,highest_bid:0,strategy:intent.strategy||'CONVERGENCE',created_at:new Date().toISOString()};
  ledger.attempts.push(attempt);
  attempt.entry_bid=entryCheck.bid;attempt.entry_liquidation_loss=entryCheck.liquidation_loss;
  attempt.worst_entry_price=intent.max_price;attempt.entry_phase=currentReport.distribution.phase;
  await atomicJSON(ledgerPath,ledger);
  let response,signed;
  attempt.status='PREPARING';await atomicJSON(ledgerPath,ledger);
  try {signed=await client.createMarketOrder({assetId:intent.asset_id,side:OrderSide.BUY,amount:intent.amount_dollars,maxSpend:intent.max_spend_dollars,maxPrice:intent.max_price,orderType:OrderType.FOK});}
  catch(error){attempt.status='REJECTED';attempt.reason='Buy signing/preparation failed before submission';attempt.updated_at=new Date().toISOString();await atomicJSON(ledgerPath,ledger);throw error;}
  attempt.max_price=intent.max_price;attempt.signed_order_file=`${intent.intent_id}.buy.signed.json`;
  await atomicJSON(resolve(secretsRoot,attempt.signed_order_file),signed);
  try{await access(resolve(dataRoot,'STOP'));throw new Error('STOP marker exists');}catch(error){if(error.code!=='ENOENT'){attempt.status='REJECTED';attempt.reason='Stopped before submission';await atomicJSON(ledgerPath,ledger);throw error;}}
  attempt.status='SUBMISSION_UNCERTAIN';await atomicJSON(ledgerPath,ledger);
  try {
   response=await client.postOrder(signed);
  } catch(error) {
   const minimumReject=String(error?.message||'').includes('invalid amount for a marketable BUY order');
   const rejected=definiteReject(error);
   attempt.status=rejected?'REJECTED':'SUBMISSION_UNCERTAIN';
   attempt.reason=minimumReject?'Exchange rejected minimum buy amount':rejected?'Exchange explicitly rejected order; no fill; fresh entry may retry':'Submission outcome uncertain; reconcile before retry';
   attempt.updated_at=new Date().toISOString();
   await atomicJSON(ledgerPath,ledger);
   throw error;
  }
  attempt.status=response.status||'POSTED';attempt.order_id=response.orderId||response.orderID||null;attempt.updated_at=new Date().toISOString();
  if(response.success===false||['REJECTED','UNMATCHED'].includes(String(response.status).toUpperCase())){attempt.status='REJECTED';attempt.reason='Exchange explicitly rejected buy';}
  else if(!attempt.order_id){attempt.status='SUBMISSION_UNCERTAIN';attempt.reason='Response missing order ID; reconcile original signed order';}
  await atomicJSON(ledgerPath,ledger);
  console.log(`Order request accepted with status ${attempt.status}.`);
 } finally { await lock.close(); await unlink(resolve(secretsRoot,'execution.lock')).catch(()=>{}); }
}

async function reconcile() {
 validateControl(await loadJSON(resolve(dataRoot,'live-control.json'),null));
 try{await access(resolve(dataRoot,'STOP'));throw new Error('STOP marker exists');}catch(error){if(error.code!=='ENOENT')throw error;}
 const geo=await fetch('https://polymarket.com/api/geoblock',{signal:AbortSignal.timeout(10000)}).then(r=>{if(!r.ok)throw new Error('geoblock check unavailable');return r.json();});
 if(geo.blocked!==false)throw new Error('live execution blocked by location check');
 const result=await runExitEngine({client:await secureClient(),load:loadJSON,save:atomicJSON,dataRoot,secretsRoot,resolve,open,unlink,OrderSide,OrderType,stopped:async()=>{try{await access(resolve(dataRoot,'STOP'));return true;}catch(e){if(e.code==='ENOENT')return false;throw e;}},fetchBook:async asset=>fetch(`https://clob.polymarket.com/book?token_id=${encodeURIComponent(asset)}`,{signal:AbortSignal.timeout(10000)}).then(r=>{if(!r.ok)throw new Error('exit book unavailable');return r.json();})});
 await atomicJSON(resolve(dataRoot,'live-exit-status.json'),{checked_at:new Date().toISOString(),...result});console.log(JSON.stringify(result));
}

async function retryFreshMarket(marketId) {
 const snapshot=await loadJSON(resolve(dataRoot,'latest.json'),null);
 if(!snapshot||Date.now()-Date.parse(snapshot.generated_at)>120000) throw new Error('latest weather snapshot is stale; retry not sent');
 const report=(snapshot.reports||[]).find(r=>(r.signals||[]).some(s=>s.market_id===marketId));
 const s=report?.signals.find(s=>s.market_id===marketId);
 if(!s||s.action!=='BUY_LIVE'||!s.new_entry_allowed||report.distribution?.calibration_ready!==true||report.distribution?.probability_status!=='CALIBRATED') throw new Error('market no longer passes live entry conditions');
 const book=await fetch(`https://clob.polymarket.com/book?token_id=${encodeURIComponent(s.yes_token_id)}`,{signal:AbortSignal.timeout(10000)}).then(r=>{if(!r.ok)throw new Error('retry book unavailable');return r.json();});
 if(Math.abs(Date.now()-Number(book.timestamp))>60000||!Number.isFinite(Number(book.timestamp))) throw new Error('retry book stale');
 const ask=Math.min(...(book.asks||[]).filter(x=>Number(x.size)>0&&Number(x.price)>0).map(x=>Number(x.price)));
 if(!Number.isFinite(ask)||ask<=0||ask>=1) throw new Error('retry ask unavailable');
 const rate=s.fee_per_share/(s.best_ask*(1-s.best_ask)),fee=rate*ask*(1-ask),net=s.net_edge+s.best_ask-ask+s.fee_per_share-fee;
 // Allow at most one cent to span multiple ask levels, only while the
 // worst permitted execution price retains positive buffered net edge.
 const tick=Number(book.tick_size)||.01;
 const candidate=Math.min(.999,Math.round((ask+.01)/tick)*tick);
 const capNet=net+ask-candidate+fee-rate*candidate*(1-candidate);
 const cap=candidate<=ask+.010000001&&capNet>0?candidate:ask;
 const intent={version:1,phase:report.distribution.phase,intent_id:`retry-${marketId}-${Date.now()}`,asset_id:s.yes_token_id,market_id:marketId,side:'BUY',amount_dollars:1,max_spend_dollars:1.1,max_price:ask,net_edge:net,model_probability:s.model_probability,market_consensus_probability:s.market_consensus_probability,expected_trade_edge:s.expected_trade_edge,entry_window:s.entry_window,data_quality:report.distribution.data_quality,model_calibrated:true,probability_status:'CALIBRATED',book_verified:true,generated_at:new Date().toISOString()};
 intent.max_price=cap;intent.net_edge=cap===ask?net:capNet;
 validateIntent(intent);
 const path=resolve(dataRoot,'live-retry',`${intent.intent_id}.json`);await atomicJSON(path,intent);
 try {await execute(path);await atomicJSON(resolve(dataRoot,'live-runner-last.json'),{at:new Date().toISOString(),market_id:marketId,file:intent.intent_id,accepted:true,message:'Fresh authorized retry submitted; see live ledger for exchange status'});}
 catch(error){await atomicJSON(resolve(dataRoot,'live-runner-last.json'),{at:new Date().toISOString(),market_id:marketId,file:intent.intent_id,accepted:false,message:String(error?.message||'retry failed')});throw error;}
}
async function adoptPosition(assetId){
 validateControl(await loadJSON(resolve(dataRoot,'live-control.json'),null));
 if(!/^\d{10,}$/.test(String(assetId||'')))throw new Error('invalid adoption asset id');
 const client=await secureClient(),positions=await firstPage(client.listPositions());
 const p=positions.find(x=>String(x.assetId)===String(assetId)&&Number(x.currentSize)>0&&String(x.status).toUpperCase()!=='REDEEMABLE');
 if(!p)throw new Error('no active account position for adoption');
 const snapshot=await loadJSON(resolve(dataRoot,'latest.json'),null),report=(snapshot?.reports||[]).find(r=>(r.signals||[]).some(s=>String(s.yes_token_id)===String(assetId)));
 const signal=report?.signals?.find(s=>String(s.yes_token_id)===String(assetId));
 if(!report||!signal)throw new Error('position is not mapped to a current weather market');
 const ledgerPath=resolve(dataRoot,'live-ledger.json'),lock=await open(resolve(secretsRoot,'execution.lock'),'wx',0o600);
 try{
  const ledger=await loadJSON(ledgerPath,{version:1,attempts:[]});
  if(ledger.attempts.some(a=>a.side==='BUY'&&String(a.asset_id)===String(assetId)&&a.origin==='MANUAL_ADOPTED'&&!a.position_closed))throw new Error('position is already adopted');
  const at=new Date().toISOString(),group=report.rule.station_icao+'|'+report.rule.local_date;
  ledger.attempts.push({intent_id:`adopt-${signal.market_id}-${Date.now()}`,market_id:signal.market_id,group_key:group,asset_id:String(assetId),side:'BUY',day:at.slice(0,10),max_spend_dollars:0,status:'matched',origin:'MANUAL_ADOPTED',strategy:'MANUAL_ADOPTED',order_id:`adopted:${assetId}`,created_at:at,adopted_at:at,entry_price:Number(p.avgPrice),entry_price_confirmed:true,entry_bid:Number(signal.best_bid)||0,highest_bid:Number(signal.best_bid)||0,model_probability:Number(signal.model_probability),entry_phase:report.distribution?.phase,exit_state:'HOLD',exit_reason:null,exit_retry_count:0,exit_next_retry_at:null});
  await atomicJSON(ledgerPath,ledger);
  console.log(`Existing position adopted for automatic exit management: ${signal.market_id}. No order sent.`);
 }finally{await lock.close();await unlink(resolve(secretsRoot,'execution.lock')).catch(()=>{});}
}
const [command,arg]=process.argv.slice(2);
async function releaseManualClose(assetId){
 if(String(assetId)!=='104623019946538484758378472544099395856905487542550978218530591151050791704069')throw new Error('manual release not authorized for this asset');
 const lock=await open(resolve(secretsRoot,'execution.lock'),'wx',0o600);
 try{
  const client=await secureClient();const [positions,orders]=await Promise.all([firstPage(client.listPositions()),firstPage(client.listOpenOrders())]);
  if(positions.some(p=>String(p.assetId)===assetId&&Number(p.currentSize)>0&&String(p.status).toUpperCase()!=='REDEEMABLE')||orders.some(o=>String(o.assetId||o.tokenId)===assetId))throw new Error('position or order still active; cannot release');
  const path=resolve(dataRoot,'live-ledger.json'),ledger=await loadJSON(path,{attempts:[]});
  if(ledger.attempts.some(a=>a.side==='SELL'&&a.asset_id===assetId&&!['CLOSED','REJECTED','CANCELED','CANCELLED','EXPIRED','PARTIAL'].includes(String(a.status).toUpperCase())))throw new Error('sell outcome unresolved; cannot release');
  let count=0;for(const a of ledger.attempts){if(a.side==='BUY'&&a.asset_id===assetId&&a.origin==='MANUAL_ADOPTED'&&!a.position_closed){a.position_closed=true;a.closed_at=new Date().toISOString();a.exit_state='CLOSED';a.exit_reason='MANUAL_CLOSE_CONFIRMED';a.manual_close_confirmed_at=a.closed_at;count++;}}
  await atomicJSON(path,ledger);console.log(`Manual closure confirmed; ${count} adopted position occupancy released. No orders sent.`);
 }finally{await lock.close();await unlink(resolve(secretsRoot,'execution.lock')).catch(()=>{});}
}
try {
 if (command==='status') await status();
 else if(command==='audit-trades'){const c=await secureClient();const ts=await firstPage(c.listAccountTrades());console.log(JSON.stringify({count:ts.length,trades:ts.slice(0,12).map(t=>({id:t.id,takerOrderId:t.takerOrderId,side:t.side,traderSide:t.traderSide,price:t.price,size:t.size,status:t.status,feeRateBps:t.feeRateBps,matchedAt:t.matchedAt,makerOrders:t.makerOrders?.map(m=>({orderId:m.orderId,matchedAmount:m.matchedAmount,price:m.price}))}))}));}
 else if (command==='reconcile') await reconcile();
 else if (command==='validate') { validateIntent(await loadJSON(resolve(arg||''),null));console.log('Intent is valid. No order sent.'); }
 else if (command==='execute') await execute(arg);
 else if (command==='retry') await retryFreshMarket(arg);
 else if(command==='adopt')await adoptPosition(arg);
 else if(command==='release-manual-close')await releaseManualClose(arg);
 else throw new Error('use status, validate, or execute');
} catch (error) { console.error(`Execution gateway stopped: ${error?.message||'unknown error'}`);process.exitCode=1; }
