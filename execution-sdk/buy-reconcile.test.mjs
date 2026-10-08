import test from 'node:test';
import assert from 'node:assert/strict';
import {reconcileBuys} from './buy-reconcile.mjs';
const t=Date.parse('2026-09-15T09:00:00Z');
function setup(a,client){return {client,ledger:{attempts:[a]},load:async()=>({signature:'original'}),resolve:(...x)=>x.join('/'),secretsRoot:'local',rows:[{signal:{yes_token_id:'a',action:'BUY_LIVE',new_entry_allowed:true,net_edge:.1,best_ask:.2},distribution:{calibration_ready:true,probability_status:'CALIBRATED'}}],weatherFresh:true,now:()=>t,stopped:async()=>false};}
test('uncertain buy reposts only original signed payload and respects timer',async()=>{
 let posts=0;const a={side:'BUY',asset_id:'a',status:'SUBMISSION_UNCERTAIN',signed_order_file:'signed',max_price:.2};
 const env=setup(a,{postOrder:async payload=>{assert.equal(payload.signature,'original');posts++;throw new Error('network timeout');}});
 await reconcileBuys(env);await reconcileBuys(env);assert.equal(posts,1);assert.equal(a.status,'SUBMISSION_UNCERTAIN');
 env.now=()=>t+31000;env.rows[0].signal.action='SKIP';await reconcileBuys(env);assert.equal(posts,1);
});
test('explicit FOK rejection releases slot; missing checkpoint never reposts',async()=>{
 const a={side:'BUY',asset_id:'a',status:'SUBMISSION_UNCERTAIN',signed_order_file:'signed',max_price:.2};
 await reconcileBuys(setup(a,{postOrder:async()=>{throw new Error("order couldn't be fully filled");}}));assert.equal(a.status,'REJECTED');
 const b={side:'BUY',status:'SUBMISSION_UNCERTAIN'};await reconcileBuys(setup(b,{postOrder:async()=>{throw new Error('should not run');}}));assert.equal(b.status,'SUBMISSION_UNCERTAIN');
});
test('cancelled partially filled buy remains occupied; unfilled cancellation releases',async()=>{
 for(const qty of [0,2]){const a={side:'BUY',status:'LIVE',order_id:'id'};await reconcileBuys(setup(a,{fetchOrder:async()=>({status:'CANCELED',sizeMatched:qty})}));assert.equal(a.status,qty?'matched':'REJECTED');}
});
