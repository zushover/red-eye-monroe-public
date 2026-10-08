import {mkdir,readdir,readFile,rename,writeFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {reentryGuard} from './entry-guards.mjs';
import {groupOccupied} from './executor-core.mjs';

const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const data=resolve(root,'data');
const outbox=resolve(data,'live-outbox');
const processed=resolve(data,'live-processed');
const rejected=resolve(data,'live-rejected');
const stop=resolve(data,'STOP');
const statusIntervalMs=60000;
let lastStatusAt=0;
const retryChecks=new Map();
async function recordResult(log){
 let results={};try{results=JSON.parse(await readFile(resolve(data,'live-execution-results.json'),'utf8'));}catch{}
 const exchange=/couldn't be fully filled|fully filled or killed|Exchange explicitly rejected|invalid amount/i.test(log.message||'');
 log.stage=log.accepted?'SUBMITTED':exchange?'REJECTED':'BLOCKED';
 results[log.market_id]=log;
 for(const [k,v] of Object.entries(results))if(Date.now()-Date.parse(v.at)>86400000)delete results[k];
 const path=resolve(data,'live-execution-results.json');await writeFile(path+'.tmp',JSON.stringify(results));await rename(path+'.tmp',path);
 await writeFile(resolve(data,'live-runner-last.json'),JSON.stringify(log,null,2));
}

async function exists(path){try{await readFile(path);return true;}catch(e){if(e.code==='ENOENT')return false;throw e;}}
async function runIntent(path){
 return await new Promise(resolveRun=>{
  const child=spawn(process.execPath,['--use-env-proxy',resolve(root,'execution-sdk','executor.mjs'),'execute',path],{cwd:root,stdio:['ignore','pipe','pipe'],windowsHide:true});
  let output='';child.stdout.on('data',x=>output+=x);child.stderr.on('data',x=>output+=x);
  child.on('close',code=>resolveRun({code,output:output.trim().slice(-2000)}));
 });
}
async function move(path,targetDir){await mkdir(targetDir,{recursive:true});await rename(path,resolve(targetDir,`${Date.now()}-${path.split(/[\\/]/).pop()}`));}
async function cycle(){
 const now=Date.now();
 if(now-lastStatusAt>=statusIntervalMs){
  const status=await runIntentCommand('status');
  await writeFile(resolve(data,'live-status-poll.json'),JSON.stringify({at:new Date().toISOString(),accepted:status.code===0,message:status.output},null,2));
  lastStatusAt=now;
 }
 const files=(await readdir(outbox).catch(e=>e.code==='ENOENT'?[]:Promise.reject(e))).filter(x=>x.endsWith('.json')).sort();
 for(const file of files){
  if(await exists(stop)) return;
  const path=resolve(outbox,file);const result=await runIntent(path);
  const intent=JSON.parse((await readFile(path,'utf8')).replace(/^\uFEFF/,''));
  const log={at:new Date().toISOString(),file,market_id:intent.market_id,accepted:result.code===0,message:result.output};
  await recordResult(log);
  await move(path,result.code===0?processed:rejected);
 }
 // The global collection cycle may take minutes. Re-evaluate latest eligible
 // candidates against a new book rather than executing an aged outbox quote.
 const latest=JSON.parse((await readFile(resolve(data,'latest.json'),'utf8')).replace(/^\uFEFF/,''));
 if(Math.abs(Date.now()-Date.parse(latest.generated_at))>120000)return;
 const ledger=JSON.parse((await readFile(resolve(data,'live-ledger.json'),'utf8')).replace(/^\uFEFF/,''));
 const candidates=(latest.reports||[]).flatMap(r=>(r.signals||[]).filter(s=>s.action==='BUY_LIVE'&&s.new_entry_allowed&&r.distribution?.calibration_ready&&r.distribution.probability_status==='CALIBRATED').map(s=>({...s,group:r.rule.station_icao+'|'+r.rule.local_date}))).sort((a,b)=>b.net_edge-a.net_edge);
 const tokens=new Map((latest.reports||[]).flatMap(r=>(r.signals||[]).map(s=>[String(s.yes_token_id),r.rule.station_icao+'|'+r.rule.local_date])));
 const account=JSON.parse(await readFile(resolve(data,'live-status.json'),'utf8'));
 const positions=(account.positions||[]).map(p=>({...p,assetId:p.asset_id,currentSize:p.current_size})),orders=(account.orders||[]).map(o=>({...o,assetId:o.asset_id}));
 for(const s of candidates){
  if(await exists(stop))return;
  if(groupOccupied(s.group,tokens,positions,orders,ledger.attempts))continue;
  if(Date.now()-(retryChecks.get(s.market_id)||0)<30000)continue;
  retryChecks.set(s.market_id,Date.now());
  try{reentryGuard({attempts:ledger.attempts,group:s.group,tokenGroups:tokens,signal:s,snapshotAt:latest.generated_at});}
  catch(e){await recordResult({at:new Date().toISOString(),market_id:s.market_id,accepted:false,message:e.message});continue;}
  const result=await runIntentCommand('retry',s.market_id);
  await recordResult({at:new Date().toISOString(),market_id:s.market_id,file:'fresh-candidate-retry',accepted:result.code===0,message:result.output});
  break; // One fresh candidate per cycle; gateway independently enforces all caps.
 }
}

async function runIntentCommand(command,path){
 return await new Promise(resolveRun=>{
  const args=['--use-env-proxy',resolve(root,'execution-sdk','executor.mjs'),command];if(path)args.push(path);
  const child=spawn(process.execPath,args,{cwd:root,stdio:['ignore','pipe','pipe'],windowsHide:true});
  let output='';child.stdout.on('data',x=>output+=x);child.stderr.on('data',x=>output+=x);
  child.on('close',code=>resolveRun({code,output:output.trim().slice(-2000)}));
 });
}

await mkdir(outbox,{recursive:true});
console.log('Live runner started. It only processes calibrated intents while authorization is valid.');
async function exitLoop(){while(!(await exists(stop))){try{const r=await runIntentCommand('reconcile');await writeFile(resolve(data,'live-reconcile-last.json'),JSON.stringify({at:new Date().toISOString(),accepted:r.code===0,message:r.output},null,2));}catch{await writeFile(resolve(data,'live-exit-error.txt'),`${new Date().toISOString()} exit check failed; retry scheduled\n`);}await new Promise(r=>setTimeout(r,15000));}}
const exitTask=exitLoop();
while(!(await exists(stop))){
 try{await cycle();}catch(error){await writeFile(resolve(data,'live-runner-error.txt'),`${new Date().toISOString()} ${error?.message||error}\n`);}
 await new Promise(r=>setTimeout(r,15000));
}
console.log('STOP marker found. Live runner stopped.');
await exitTask;
