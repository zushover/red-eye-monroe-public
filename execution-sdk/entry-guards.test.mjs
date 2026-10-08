import test from 'node:test';
import assert from 'node:assert/strict';
import {entryLiquidity,reentryGuard} from './entry-guards.mjs';
test('entry spread cannot already consume stop budget; full exit depth required',()=>{
 assert.throws(()=>entryLiquidity({entry:.12,shares:8.33,rate:.05,bids:[{price:.08,size:100}]}),/20%/);
 assert.ok(entryLiquidity({entry:.16,shares:6.25,rate:.05,bids:[{price:.145,size:100}]}).liquidation_loss<.20);
 assert.throws(()=>entryLiquidity({entry:.16,shares:NaN,rate:.05,bids:[]}),/inputs/);
 assert.throws(()=>entryLiquidity({entry:.16,shares:6.25,rate:.05,bids:[{price:.16,size:2}]}),/depth/);
 assert.ok(entryLiquidity({entry:.16,shares:6.25,rate:.05,bids:[{price:.15,size:100}]}).liquidation_loss<.15);
});
test('stop reentry requires cooldown and real probability change, not a new timestamp',()=>{
 const t=Date.parse('2026-09-15T09:00:00Z');
 const attempts=[{side:'BUY',asset_id:'a',model_probability:.2,created_at:'2026-09-15T08:00:00Z',closed_at:'2026-09-15T09:00:00Z'},{side:'SELL',asset_id:'a',status:'CLOSED',reason:'STOP_25',created_at:'2026-09-15T08:59:00Z'}];
 const env={attempts,group:'CITY|15',tokenGroups:new Map([['a','CITY|15']]),signal:{model_probability:.24},snapshotAt:'2026-09-15T09:31:00Z',now:t+1000};
 assert.throws(()=>reentryGuard(env),/cooldown/);
 env.now=t+31*60000;env.signal.model_probability=.2;assert.throws(()=>reentryGuard(env),/improvement/);
 env.signal.model_probability=.24;assert.doesNotThrow(()=>reentryGuard(env));
});
