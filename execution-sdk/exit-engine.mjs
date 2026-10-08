import {reconcileBuys} from './buy-reconcile.mjs';
export const RETRY_MS=30000;
const filled=s=>['matched','filled'].includes(String(s).toLowerCase());
const terminal=s=>['REJECTED','CANCELED','CANCELLED','EXPIRED','CLOSED','PARTIAL'].includes(String(s).toUpperCase());
export function definiteReject(error){return /couldn't be fully filled|fully filled or killed|no orders found to match|insufficient liquidity|not enough balance|lower than the minimum|invalid amount|invalid order payload/i.test(String(error?.message||error));}
export function exitDecision({bid,buy,row,fresh,now=Date.now(),feeRate=.10}){
 const entry=Number(buy.entry_price),p=Number(row?.signal?.model_probability),phase=row?.distribution?.phase;
 const high=Math.max(Number(buy.highest_bid)||0,bid),gain=high-entry;
 const early=['PRE_DAY','OVERNIGHT','MORNING'].includes(phase)||(phase==='INTRADAY'&&Number(row.distribution.local_minute)<600);
 const grace=(now-Date.parse(buy.created_at))/60000>=(phase==='PRE_DAY'?45:30);
 const elapsed=now-Date.parse(buy.created_at);
 const stopBaseline=Number(buy.entry_bid)>0?Number(buy.entry_bid):entry;
 const net=bid-entry-feeRate*entry*(1-entry)-feeRate*bid*(1-bid);
 const activation=Math.max(.02,Math.min(.04,entry*.15));
 let trail=Math.max(.015,Math.min(.04,gain*.40)),conv=Number(row?.signal?.market_convergence)||0;
 if(fresh&&conv>=.5)trail=Math.min(trail,.03);if(fresh&&conv>=.65)trail=Math.min(trail,.02);
 if(fresh&&['LATE_DAY','AWAITING_SETTLEMENT'].includes(phase))trail=Math.min(trail,.015);
 let reason=null;
 if(entry>0&&bid<=entry*.60)reason='EMERGENCY_STOP_40';
 else if(entry>0&&bid<=stopBaseline*.75&&elapsed>=((buy.entry_phase==='PRE_DAY'||fresh&&phase==='PRE_DAY')?45:30)*60000)reason='STOP_25';
 else if(fresh&&phase==='AWAITING_SETTLEMENT')reason='DAY_ENDED';
 else if(fresh&&entry>0&&bid<entry&&Number(buy.model_probability)-p>=.04&&(!early||grace))reason='MODEL_DROP_4PP';
 else if(fresh&&Number(buy.model_failure_count)>=2&&Number(buy.model_probability)-p>=.03&&bid<entry&&(!early||grace))reason='MODEL_DROP_PERSISTENT';
 else if(fresh&&bid>p+.03&&net>0)reason='BID_ABOVE_MODEL';
 else if(gain>=activation&&bid<=high-trail&&bid>=entry+.01&&net>0&&bid<=Number(buy.last_bid||bid))reason='DYNAMIC_TRAIL';
 else if(fresh&&phase==='LATE_DAY'&&conv>=.70)reason='LATE_CONVERGENCE';
 return {reason,high,net};
}
export function exitPlan(bids,shares,minimum,reason,entry,feeRate=.10,partial=false){
 if(!bids.length||shares<minimum)return null;
 let floor=Math.max(.001,bids[0].price-.01);
 if(['DYNAMIC_TRAIL','BID_ABOVE_MODEL'].includes(reason)){
  const profitable=x=>x-entry-feeRate*entry*(1-entry)-feeRate*x*(1-x)>0;
  if(!profitable(bids[0].price))return null;
  if(!profitable(floor))floor=bids[0].price;
 }
 const levels=bids.filter(x=>x.price>=floor),depth=levels.reduce((s,x)=>s+x.size,0);
 if(depth+1e-8>=shares)return {shares,floor:levels.at(-1).price,partial:false};
 if(partial&&depth>=minimum){let quantity=Math.floor(Math.min(shares,depth)*100)/100;if(shares-quantity<minimum)quantity=Math.floor((shares-minimum)*100)/100;if(quantity>=minimum)return {shares:quantity,floor:levels.at(-1).price,partial:true};}
 return null;
}

// Dependency-injected for offline tests. No secrets or network on import.
export async function runExitEngine({client,load,save,dataRoot,secretsRoot,resolve,open,unlink,fetchBook,OrderSide,OrderType,now=()=>Date.now(),stopped=async()=>false}){
 const path=resolve(dataRoot,'live-ledger.json'),snapshot=await load(resolve(dataRoot,'latest.json'),null);
 const weatherFresh=!!snapshot&&Math.abs(now()-Date.parse(snapshot.generated_at))<=120000;
 const rows=(snapshot?.reports||[]).flatMap(r=>(r.signals||[]).map(signal=>({signal,distribution:r.distribution,status:r.status})));
 async function all(p){const a=[];for await(const page of p)a.push(...(page.items||[]));return a;}
 const lock=await open(resolve(secretsRoot,'execution.lock'),'wx',0o600);
 try{
  const ledger=await load(path,{version:1,attempts:[]});
  for(const a of ledger.attempts){const r=(snapshot?.reports||[]).find(r=>(r.signals||[]).some(s=>String(s.yes_token_id)===String(a.asset_id)));if(r&&!a.group_key)a.group_key=r.rule.station_icao+'|'+r.rule.local_date;}
  const [positions,orders]=await Promise.all([all(client.listPositions()),all(client.listOpenOrders())]);
  await reconcileBuys({client,ledger,load,save,resolve,secretsRoot,rows,weatherFresh,now,stopped,fetchBook});
  const buys=[...ledger.attempts].reverse().filter((a,i,arr)=>a.side==='BUY'&&a.order_id&&a.status!=='REJECTED'&&!a.position_closed&&arr.findIndex(x=>x.side==='BUY'&&x.asset_id===a.asset_id&&x.order_id&&x.status!=='REJECTED'&&!x.position_closed)===i);
  for(const buy of buys){
   try{
    const asset=String(buy.asset_id),p=positions.find(p=>String(p.assetId)===asset),size=Number(p?.currentSize||0),row=rows.find(x=>String(x.signal.yes_token_id)===asset);
    const active=ledger.attempts.filter(a=>a.side==='SELL'&&a.asset_id===asset&&Date.parse(a.created_at)>=Date.parse(buy.created_at)&&!terminal(a.status)).at(-1);
    if(active){
     if(active.status==='PREPARING'){active.status='REJECTED';}
     let state=active.exchange_status;
     if(active.order_id){try{const order=await client.fetchOrder({orderId:active.order_id});state=order.status;active.exchange_status=state;active.confirmation_error=null;}catch{active.confirmation_error='Order query unavailable; no new sell permitted';}}
     if(size===0&&!orders.some(o=>String(o.assetId||o.tokenId)===asset)){
      active.zero_checks=Number(active.zero_checks||0)+1;
      if(active.zero_checks>=2&&now()-Date.parse(active.created_at)>=30000){active.status='CLOSED';buy.position_closed=true;buy.closed_at=new Date(now()).toISOString();buy.exit_state='CLOSED';}
      else buy.exit_state='CONFIRMING';
      continue;
     }
     active.zero_checks=0;
     if(['canceled','cancelled','expired','unmatched','rejected'].includes(String(state).toLowerCase())){active.status='REJECTED';buy.exit_next_retry_at=new Date(now()+RETRY_MS).toISOString();}
     else if(filled(state)&&size>0&&size<Number(active.before_shares)-1e-6&&!orders.some(o=>String(o.assetId||o.tokenId)===asset)){active.status='PARTIAL';buy.exit_next_retry_at=new Date(now()+RETRY_MS).toISOString();}
     else if(!terminal(active.status)&&active.status!=='SUBMISSION_UNCERTAIN'){
      if(active.order_id&&['live','open','delayed'].includes(String(state).toLowerCase())){
       const check=await fetchBook(asset),best=Math.max(0,...(check.bids||[]).filter(x=>Number(x.size)>0).map(x=>Number(x.price)));
       if(best>0&&Number.isFinite(Number(check.timestamp))&&Math.abs(now()-Number(check.timestamp))<=60000&&!exitDecision({bid:best,buy,row,fresh:weatherFresh&&row?.status==='READY',now:now(),feeRate:Number(buy.exit_fee_rate??.10)}).reason){
        const cancel=await client.cancelOrder({orderId:active.order_id});active.cancel_requested_at=new Date(now()).toISOString();
        if((cancel.canceled||[]).includes(active.order_id))active.status='CANCELED';buy.exit_state='CANCEL_PENDING';continue;
       }
      }
      buy.exit_state='CONFIRMING';continue;
     }
    }
    if(size<=0||String(p?.status).toUpperCase()==='REDEEMABLE'){buy.exit_state='NO_TRADABLE_POSITION';continue;}
    if(Number(p?.avgPrice)>0&&Number(p.avgPrice)<1){buy.entry_price=Number(p.avgPrice);buy.entry_price_confirmed=true;}
    const book=await fetchBook(asset);
    if(!Number.isFinite(Number(book.timestamp))||Math.abs(now()-Number(book.timestamp))>60000){buy.exit_state='BOOK_STALE';continue;}
    const bids=(book.bids||[]).map(x=>({price:Number(x.price),size:Number(x.size)})).filter(x=>Number.isFinite(x.price)&&Number.isFinite(x.size)&&x.price>0&&x.price<1&&x.size>0).sort((a,b)=>b.price-a.price);
    if(!bids.length){buy.exit_state='NO_BUYERS';buy.exit_last_checked_at=new Date(now()).toISOString();continue;}
    const ask=Number(row?.signal?.best_ask),inferred=Number(row?.signal?.fee_per_share)/(ask*(1-ask));
    const rate=Number.isFinite(inferred)&&inferred>=0?inferred:Number(buy.exit_fee_rate??.10);buy.exit_fee_rate=rate;
    const decision=exitDecision({bid:bids[0].price,buy,row,fresh:weatherFresh&&row?.status==='READY',now:now(),feeRate:rate});
    buy.highest_bid=decision.high;buy.last_bid=bids[0].price;buy.estimated_net_unit_profit=decision.net;buy.last_checked_at=new Date(now()).toISOString();
    if(weatherFresh&&row&&buy.last_strategy_snapshot!==snapshot.generated_at){const drop=Number(buy.model_probability)-Number(row.signal.model_probability);buy.model_failure_count=drop>=.03&&bids[0].price<Number(buy.entry_price)?Number(buy.model_failure_count||0)+1:0;buy.last_strategy_snapshot=snapshot.generated_at;}
    buy.exit_reason=decision.reason;
    if(!decision.reason){buy.exit_state=active?.status==='SUBMISSION_UNCERTAIN'?'HOLD_PENDING_CONFIRMATION':'HOLD';buy.exit_retry_count=0;buy.exit_next_retry_at=null;continue;}
    if(Date.parse(buy.exit_next_retry_at||0)>now()){buy.exit_state='RETRY_WAIT';continue;}
    const plan=exitPlan(bids,size,Number(book.min_order_size||0),decision.reason,Number(buy.entry_price),rate,Number(buy.exit_retry_count||0)>=3);
    if(!plan){buy.exit_state=size<Number(book.min_order_size||0)?'BELOW_MINIMUM':'INSUFFICIENT_DEPTH';buy.exit_retry_count=Number(buy.exit_retry_count||0)+1;buy.exit_next_retry_at=new Date(now()+RETRY_MS).toISOString();continue;}
    if(await stopped()){buy.exit_state='STOPPED';continue;}
    let attempt,signed;
    if(active?.status==='SUBMISSION_UNCERTAIN'){
     // Never create a second order for an ambiguous first submission.
     signed=active.signed_file?await load(resolve(secretsRoot,active.signed_file),null):null;
     if(!signed||size+1e-6<Number(active.shares)||plan.floor<Number(active.min_price)){buy.exit_state='UNCERTAIN_VERIFY';continue;}
     attempt=active;
    }else{
     attempt={intent_id:`exit-${now()}-${asset.slice(-8)}`,market_id:buy.market_id,group_key:buy.group_key,asset_id:asset,side:'SELL',day:new Date(now()).toISOString().slice(0,10),shares:plan.shares,before_shares:size,min_price:plan.floor,status:'PREPARING',reason:decision.reason,created_at:new Date(now()).toISOString()};
     ledger.attempts.push(attempt);await save(path,ledger);
     try{signed=await client.createMarketOrder({assetId:asset,side:OrderSide.SELL,shares:plan.shares,minPrice:plan.floor,orderType:plan.partial?OrderType.FAK:OrderType.FOK});}
     catch(error){attempt.status='REJECTED';attempt.execution_error='Order preparation failed before submission';buy.exit_retry_count=Number(buy.exit_retry_count||0)+1;buy.exit_next_retry_at=new Date(now()+RETRY_MS).toISOString();continue;}
     attempt.signed_file=`${attempt.intent_id}.signed.json`;await save(resolve(secretsRoot,attempt.signed_file),signed);
    }
    if(await stopped()){if(attempt.status==='PREPARING')attempt.status='REJECTED';buy.exit_state='STOPPED';continue;}
    attempt.status='SUBMISSION_UNCERTAIN';buy.exit_state='REQUESTING';await save(path,ledger);
    try{
     const response=await client.postOrder(signed);
     attempt.order_id=response.orderId||response.orderID||attempt.order_id||null;attempt.exchange_status=response.status;
     if(response.success===false||['unmatched','rejected'].includes(String(response.status).toLowerCase()))attempt.status='REJECTED';
     else attempt.status=attempt.order_id?'CONFIRMING':'SUBMISSION_UNCERTAIN';
     attempt.updated_at=new Date(now()).toISOString();
    }catch(error){
     const message=String(error?.message||'');
     const duplicate=/order\s+(0x[0-9a-fA-F]{64}).*duplicated/i.exec(message);
     if(duplicate){attempt.order_id=duplicate[1];attempt.status='CONFIRMING';}
     else if(definiteReject(error)){attempt.status='REJECTED';attempt.execution_error='Exchange explicitly rejected sell; fresh retry scheduled';}
     else {attempt.status='SUBMISSION_UNCERTAIN';attempt.execution_error='Transport/result uncertain; only identical signed order may be retried';}
    }
    buy.exit_retry_count=Number(buy.exit_retry_count||0)+1;buy.exit_next_retry_at=new Date(now()+RETRY_MS).toISOString();
    await save(path,ledger);
   }catch{buy.exit_state='CHECK_FAILED';buy.exit_last_checked_at=new Date(now()).toISOString();}
  }
  await save(path,ledger);
  return {weather_fresh:weatherFresh,positions:buys.map(b=>({market_id:b.market_id,state:b.exit_state,reason:b.exit_reason,next_retry:b.exit_next_retry_at}))};
 }finally{await lock.close();await unlink(resolve(secretsRoot,'execution.lock')).catch(()=>{});}
}
