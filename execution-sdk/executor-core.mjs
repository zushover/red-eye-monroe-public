export function blocksSellRetry(attempt,asset,after=0) {
 if(after&&Date.parse(attempt.created_at)<after) return false;
 return attempt.side==='SELL'&&attempt.asset_id===asset&&!!(attempt.order_id||['SUBMITTING','SUBMISSION_UNCERTAIN'].includes(attempt.status));
}
export function groupOccupied(group,tokenGroups,positions,orders,attempts) {
 const key=a=>a.group_key||tokenGroups.get(String(a.asset_id||''));
 if(positions.some(p=>tokenGroups.get(String(p.assetId))===group&&Number(p.currentSize)>0&&String(p.status).toUpperCase()!=='REDEEMABLE'))return true;
 if(orders.some(o=>tokenGroups.get(String(o.assetId||o.tokenId))===group))return true;
 return attempts.some(a=>a.side==='BUY'&&key(a)===group&&a.status!=='REJECTED'&&!a.position_closed&&!attempts.some(x=>x.side==='SELL'&&x.asset_id===a.asset_id&&String(x.status).toUpperCase()==='CLOSED'&&Date.parse(x.created_at)>Date.parse(a.created_at)));
}
export function positionExposure(positions) {
 let total=0;
 for(const p of positions){
  // Only an explicit settlement status releases trading risk. A zero mark
  // alone does not prove that a live position has ended.
  if(String(p.status||'').toUpperCase()==='REDEEMABLE') continue;
  const size=Number(p.currentSize),price=Number(p.avgPrice);
  if(!Number.isFinite(size)||!Number.isFinite(price)||size<0||price<0||price>1) throw new Error('invalid position exposure data');
  total+=size*price*1.10; // Conservatively reserve up to 10% for paid entry fees.
 }
 return total;
}

export function validateIntent(value,now=new Date()) {
 if (!value||value.version!==1) throw new Error('intent version must be 1');
 if (!/^[A-Za-z0-9._-]{8,120}$/.test(value.intent_id||'')) throw new Error('invalid intent_id');
 if (!/^(0x[0-9a-fA-F]{40,64}|[0-9]{10,})$/.test(value.asset_id||'')) throw new Error('invalid asset_id');
 if (!/^[0-9A-Za-z_-]{1,120}$/.test(value.market_id||'')) throw new Error('invalid market_id');
 if (value.side!=='BUY') throw new Error('only BUY is supported');
 for (const [name,n] of [['amount_dollars',value.amount_dollars],['max_spend_dollars',value.max_spend_dollars],['max_price',value.max_price],['net_edge',value.net_edge]]) {
  if (!Number.isFinite(n)) throw new Error(`${name} must be finite`);
 }
 if (value.amount_dollars!==1||value.max_spend_dollars!==1.10) throw new Error('order must be $1 notional with a $1.10 fee-inclusive cap');
 if (value.max_price<=0||value.max_price>=1) throw new Error('invalid max_price');
 if (value.net_edge<=0) throw new Error('net edge must be positive after all buffers');
 if (!Number.isFinite(value.market_consensus_probability)||value.market_consensus_probability<0.10) throw new Error('market consensus is below 10% hard floor');
 if (value.model_calibrated!==true||value.probability_status!=='CALIBRATED') throw new Error('model is not calibrated for live trading');
 if (!Number.isFinite(value.data_quality)||value.data_quality<0.65) throw new Error('weather data quality is below 65%');
 const created=new Date(value.generated_at);
 if (!Number.isFinite(created.getTime())||Math.abs(now-created)>60000) throw new Error('intent is stale');
 if (value.book_verified!==true) throw new Error('fresh verified book is required');
 return Object.freeze({...value});
}

export function validateControl(value,now=new Date()) {
 if (!value||value.version!==1||value.enabled!==true) throw new Error('live execution is disabled');
 if(value.persistent_authorization!==true){const expiry=new Date(value.expires_at);if(!Number.isFinite(expiry.getTime())||expiry<=now)throw new Error('live authorization expired');if(expiry-now>12*60*60*1000+60000)throw new Error('live authorization exceeds 12 hours');}
 if (value.maximum_order_dollars!==1.10||value.maximum_open_orders>2||value.maximum_simultaneous_positions!==6||value.maximum_total_exposure_dollars!==8) throw new Error('unsafe live limits');
 if (value.daily_submission_limit_dollars!==null&&value.daily_submission_limit_dollars!==undefined) throw new Error('daily submission limit must be disabled');
 return Object.freeze({...value});
}
