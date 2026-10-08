import test from 'node:test';
import assert from 'node:assert/strict';
import {validateControl,validateIntent,positionExposure,blocksSellRetry,groupOccupied} from './executor-core.mjs';
test('one dated city slot, reentry only after full close',()=>{
 const groups=new Map([['a','CITY|15'],['b','CITY|15'],['c','CITY|16']]);
 assert.equal(groupOccupied('CITY|15',groups,[{assetId:'b',currentSize:5,status:'OPEN'}],[],[]),true);
 assert.equal(groupOccupied('CITY|16',groups,[{assetId:'b',currentSize:5,status:'OPEN'}],[],[]),false);
 const buy={side:'BUY',asset_id:'a',status:'matched',created_at:'2026-09-15T00:00:00Z'},sell={side:'SELL',asset_id:'a',status:'matched',created_at:'2026-09-15T01:00:00Z'};
 assert.equal(groupOccupied('CITY|15',groups,[],[],[buy]),true);
 assert.equal(groupOccupied('CITY|15',groups,[],[],[buy,sell]),true);
 sell.status='CLOSED';
 assert.equal(groupOccupied('CITY|15',groups,[],[],[buy,sell]),false);
 assert.equal(groupOccupied('CITY|15',groups,[{assetId:'a',currentSize:.1,status:'OPEN'}],[],[buy,sell]),true);
 assert.equal(groupOccupied('CITY|15',groups,[],[],[buy,sell,{...buy,created_at:'2026-09-15T02:00:00Z'}]),true);
});
test('uncertain sell is never submitted twice',()=>{
 assert.equal(blocksSellRetry({side:'SELL',asset_id:'a',status:'SUBMISSION_UNCERTAIN'},'a'),true);
 assert.equal(blocksSellRetry({side:'SELL',asset_id:'a',status:'SUBMITTING'},'a'),true);
 assert.equal(blocksSellRetry({side:'SELL',asset_id:'a',status:'REJECTED'},'a'),false);
});
test('settled positions release exposure, live zero marks do not',()=>{
 assert.equal(positionExposure([{status:'REDEEMABLE',currentSize:10,avgPrice:.7},{status:'OPEN',currentSize:5,avgPrice:.4,currentValue:0}]),2.2);
 assert.equal(positionExposure([{currentSize:5,avgPrice:.4}]),2.2);
 assert.throws(()=>positionExposure([{status:'OPEN',currentSize:'bad',avgPrice:.4}]));
});
const now=new Date('2026-09-14T06:00:00Z');
const intent={version:1,intent_id:'intent-123',asset_id:'12345678901',market_id:'42',side:'BUY',amount_dollars:1,max_spend_dollars:1.1,max_price:.4,net_edge:.13,expected_trade_edge:.02,market_consensus_probability:.20,data_quality:.8,model_calibrated:true,probability_status:'CALIBRATED',book_verified:true,generated_at:now.toISOString()};
test('accepts a bounded fresh intent',()=>assert.equal(validateIntent(intent,now).market_id,'42'));
test('all phases require positive net edge but experimental exit is not a gate',()=>{
 assert.doesNotThrow(()=>validateIntent({...intent,phase:'INTRADAY',net_edge:.001,expected_trade_edge:.001},now));
 assert.doesNotThrow(()=>validateIntent({...intent,phase:'PRE_DAY',net_edge:.001,expected_trade_edge:.001},now));
 assert.throws(()=>validateIntent({...intent,net_edge:0,expected_trade_edge:.01},now));
 assert.doesNotThrow(()=>validateIntent({...intent,net_edge:.01,expected_trade_edge:-.02},now));
});
test('rejects oversize and stale intents',()=>{
 assert.throws(()=>validateIntent({...intent,max_spend_dollars:1.01},now));
 assert.throws(()=>validateIntent({...intent,generated_at:'2026-09-14T05:58:00Z'},now));
});
test('live control expires and cannot widen caps',()=>{
 assert.doesNotThrow(()=>validateControl({version:1,enabled:true,persistent_authorization:true,expires_at:null,maximum_order_dollars:1.1,daily_submission_limit_dollars:null,maximum_open_orders:2,maximum_simultaneous_positions:6,maximum_total_exposure_dollars:8},now));
 assert.throws(()=>validateControl({version:1,enabled:true,persistent_authorization:true,expires_at:null,maximum_order_dollars:2,daily_submission_limit_dollars:null,maximum_open_orders:2,maximum_simultaneous_positions:6,maximum_total_exposure_dollars:8},now));
});
test('rejects an uncalibrated or weak-data intent',()=>{
 assert.throws(()=>validateIntent({...intent,model_calibrated:false},now));
 assert.throws(()=>validateIntent({...intent,data_quality:.64},now));
});
test('rejects market consensus below 10 percent',()=>assert.throws(()=>validateIntent({...intent,market_consensus_probability:.099},now)));
