// Executed amounts come from account fills; fees remain formula estimates.
export function summarizeFills(attempts,trades){
 const out={};
 for(const a of attempts){
  if(!a.order_id)continue;
  const fills=[...new Map(trades.filter(t=>String(t.takerOrderId)===String(a.order_id)&&['CONFIRMED','MINED','MATCHED'].includes(String(t.status).toUpperCase().replace('TRADE_STATUS_',''))).map(t=>[t.id,t])).values()];
  if(!fills.length)continue;
  if(fills.some(t=>!Number.isFinite(+t.size)||+t.size<=0||!Number.isFinite(+t.price)||+t.price<=0||+t.price>=1))continue;
  const shares=fills.reduce((n,t)=>n+Number(t.size),0),gross=fills.reduce((n,t)=>n+Number(t.size)*Number(t.price),0);
  const fee=fills.every(t=>t.feeRateBps!==null&&t.feeRateBps!==undefined&&Number.isFinite(+t.feeRateBps))?fills.reduce((n,t)=>n+Number(t.size)*(Number(t.feeRateBps)/10000)*Number(t.price)*(1-Number(t.price)),0):null;
  out[a.intent_id]={shares,average_price:gross/shares,gross,estimated_fee:fee,net:fee===null?null:a.side==='BUY'?gross+fee:gross-fee,settled:fills.every(t=>String(t.status).endsWith('CONFIRMED')),source:'exchange_fills',fee_source:'exchange_rate_formula'};
 }
 // Each exit allocates the preceding entry's per-share cost, including partial exits.
 for(const a of attempts.filter(a=>a.side==='SELL')){
  const sell=out[a.intent_id];if(!sell)continue;
  const buy=[...attempts].reverse().find(b=>b.side==='BUY'&&b.asset_id===a.asset_id&&out[b.intent_id]&&Date.parse(b.created_at)<Date.parse(a.created_at));
  const entry=out[buy?.intent_id];
  if(entry?.net!==null&&entry&&sell.net!==null){sell.allocated_cost=entry.net/entry.shares*sell.shares;sell.estimated_realized_pnl=sell.net-sell.allocated_cost;}
 }
 return out;
}
