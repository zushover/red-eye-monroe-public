import test from 'node:test';import assert from 'node:assert/strict';import {summarizeFills} from './accounting.mjs';
test('exchange enum prefix, duplicates, partial sell and failed fills',()=>{
 const attempts=[{side:'BUY',intent_id:'b',order_id:'B',asset_id:'x',created_at:'2026-09-15T01:00:00Z'},{side:'SELL',intent_id:'s',order_id:'S',asset_id:'x',created_at:'2026-09-15T02:00:00Z'}];
 const b={id:'1',takerOrderId:'B',size:'6.25',price:'.16',status:'TRADE_STATUS_CONFIRMED',feeRateBps:'0'},s={id:'2',takerOrderId:'S',size:'5',price:'.11',status:'TRADE_STATUS_CONFIRMED',feeRateBps:'0'};
 const a=summarizeFills(attempts,[b,b,s,{...s,id:'3',status:'TRADE_STATUS_FAILED'}]);
 assert.equal(a.b.gross,1);assert.equal(a.b.net,1);assert.equal(a.s.shares,5);assert.ok(Math.abs(a.s.estimated_realized_pnl+.25)<1e-8);
});
