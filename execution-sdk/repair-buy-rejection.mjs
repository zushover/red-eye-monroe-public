import {readFile,writeFile,rename,unlink} from 'node:fs/promises';
import {resolve} from 'node:path';
import {executionLock} from './execution-lock.mjs';
import {definiteReject} from './exit-engine.mjs';
const root=resolve(import.meta.dirname,'..'),path=resolve(root,'data/live-ledger.json');
const lockPath=resolve(root,'.secrets/execution.lock');
const lock=await executionLock(lockPath);
try {
 const last=JSON.parse(await readFile(resolve(root,'data/live-runner-last.json'),'utf8'));
 if(last.accepted!==false||!definiteReject(last.message))throw new Error('No explicit rejection evidence; not modified');
 const ledger=JSON.parse(await readFile(path,'utf8'));
 const attempt=ledger.attempts.find(a=>a.market_id===last.market_id&&last.file===a.intent_id+'.json'&&a.status==='SUBMISSION_UNCERTAIN'&&!a.order_id);
 if(!attempt)throw new Error('No matching uncertain attempt; not modified');
 attempt.status='REJECTED';attempt.reason='Confirmed exchange FOK rejection: no full fill; fresh entry may retry';
 attempt.rejection_evidence_at=last.at;attempt.updated_at=new Date().toISOString();
 await writeFile(path+'.repair.tmp',JSON.stringify(ledger,null,2),{mode:0o600});await rename(path+'.repair.tmp',path);
 console.log('Explicit rejection repaired; audit record retained. No order submitted.');
}finally{await lock.close();await unlink(lockPath).catch(()=>{});}
