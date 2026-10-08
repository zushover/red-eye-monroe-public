import test from 'node:test';
import assert from 'node:assert/strict';
import {exitDecision,exitPlan,runExitEngine} from './exit-engine.mjs';
const now=Date.parse('2026-09-15T12:00:00Z');
const buy={intent_id:'buy',asset_id:'a',market_id:'m',side:'BUY',status:'matched',order_id:'buy-order',entry_price:.16,model_probability:.3,created_at:'2026-09-15T11:55:00Z'};
test('stale/missing weather does not disable price stops',()=>{
 assert.equal(exitDecision({bid:.09,buy,fresh:false,now}).reason,'EMERGENCY_STOP_40');
 assert.equal(exitDecision({bid:.12,buy,fresh:false,now}).reason,null);
 assert.equal(exitDecision({bid:.12,buy:{...buy,created_at:'2026-09-15T11:00:00Z'},fresh:false,now}).reason,'STOP_25');
 assert.equal(exitDecision({bid:.12,buy:{...buy,entry_bid:.13,created_at:'2026-09-15T11:00:00Z'},fresh:false,now}).reason,null);
 assert.equal(exitDecision({bid:.15,buy,fresh:false,now}).reason,null);
});
test('dynamic trail holds a recovering price and only protects net profit',()=>{
 const b={...buy,highest_bid:.25,last_bid:.21};
 assert.equal(exitDecision({bid:.20,buy:b,fresh:false,now,feeRate:.05}).reason,'DYNAMIC_TRAIL');
 assert.equal(exitDecision({bid:.20,buy:{...b,last_bid:.19},fresh:false,now,feeRate:.05}).reason,null);
});
test('multi-level FOK first, bounded partial fallback, minimum enforced',()=>{
 const bids=[{price:.2,size:4},{price:.195,size:6},{price:.1,size:100}];
 assert.deepEqual(exitPlan(bids,10,5,'STOP_25',.3),{shares:10,floor:.195,partial:false});
 assert.equal(exitPlan(bids,15,5,'STOP_25',.3),null);
 assert.equal(exitPlan(bids,15,5,'STOP_25',.3,.1,true).partial,true);
 assert.equal(exitPlan(bids,4,5,'STOP_25',.3),null);
 assert.equal(exitPlan([{price:.17,size:10}],10,5,'DYNAMIC_TRAIL',.16,.1),null);
 assert.equal(exitPlan([{price:.2,size:5}],6.25,5,'STOP_25',.3,.1,true),null);
});
function harness(){let time=now,bid=.09,size=10,postCount=0,createCount=0;const files=new Map([['data/live-ledger.json',{version:1,attempts:[structuredClone(buy)]}],['data/latest.json',{generated_at:new Date(now-999999).toISOString(),reports:[]}]]);let post=async()=>{throw Error("order couldn't be fully filled. FOK orders are fully filled or killed.");};const paged=items=>({async *[Symbol.asyncIterator](){yield {items};}}),client={listPositions:()=>paged(size?[{assetId:'a',currentSize:size,status:'OPEN'}]:[]),listOpenOrders:()=>paged([]),createMarketOrder:async r=>{createCount++;return {...r,salt:String(createCount)};},postOrder:async r=>{postCount++;return post(r);},fetchOrder:async()=>({status:'matched'})};const deps={client,load:async(p,f)=>structuredClone(files.get(p)??f),save:async(p,v)=>files.set(p,structuredClone(v)),dataRoot:'data',secretsRoot:'secret',resolve:(...s)=>s.join('/'),open:async()=>({close:async()=>{}}),unlink:async()=>{},fetchBook:async()=>({timestamp:time,min_order_size:5,bids:[{price:bid,size:10}]}),OrderSide:{SELL:'SELL'},OrderType:{FOK:'FOK',FAK:'FAK'},now:()=>time};return {run:()=>runExitEngine(deps),files,get posts(){return postCount},get creates(){return createCount},advance:ms=>time+=ms,setBid:x=>bid=x,setSize:x=>size=x,setPost:f=>post=f};}
test('explicit reject retries after 30s, recovery cancels retry',async()=>{
 const h=harness();await h.run();assert.equal(h.posts,1);await h.run();assert.equal(h.posts,1);h.advance(31000);await h.run();assert.equal(h.posts,2);h.advance(31000);h.setBid(.15);await h.run();assert.equal(h.posts,2);assert.equal(h.files.get('data/live-ledger.json').attempts[0].exit_state,'HOLD');
});
test('ambiguous transport retries identical signed order, never creates another',async()=>{
 const h=harness();h.setPost(async()=>{throw Error('network timeout');});await h.run();h.advance(31000);await h.run();assert.equal(h.posts,2);assert.equal(h.creates,1);h.advance(31000);h.setBid(.15);await h.run();assert.equal(h.posts,2);
});
test('matched response alone is not closure; zero remaining confirmed twice',async()=>{
 const h=harness();h.setPost(async()=>({status:'matched',orderID:'sell-order'}));await h.run();h.advance(31000);await h.run();assert.equal(h.posts,1);h.setSize(0);await h.run();h.advance(31000);await h.run();const l=h.files.get('data/live-ledger.json');assert.equal(l.attempts[1].status,'CLOSED');assert.equal(l.attempts[0].position_closed,true);
});
