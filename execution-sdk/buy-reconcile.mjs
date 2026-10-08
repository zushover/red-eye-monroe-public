import {entryLiquidity} from './entry-guards.mjs';
// Runs under the shared execution lock. Never manufactures a replacement order.
export async function reconcileBuys({client,ledger,load,save,resolve,secretsRoot,rows,weatherFresh,now,stopped,fetchBook}) {
 for(const a of ledger.attempts.filter(a=>a.side==='BUY'&&['SUBMITTING','SUBMISSION_UNCERTAIN','POSTED','LIVE','OPEN','DELAYED','PREPARING'].includes(String(a.status).toUpperCase()))) {
  try {
   if(a.status==='PREPARING'){a.status='REJECTED';a.reason='Interrupted before submission';continue;}
   if(now()-Date.parse(a.buy_next_check_at||0)<0)continue;
   a.buy_next_check_at=new Date(now()+30000).toISOString();
   if(a.order_id){
    const order=await client.fetchOrder({orderId:a.order_id});
    const state=String(order?.status||'').toUpperCase();
    if(['MATCHED','FILLED'].includes(state)){a.status='matched';a.reason=null;}
    else if(['CANCELED','CANCELLED','REJECTED','EXPIRED','UNMATCHED'].includes(state)){
     // A cancelled order may have filled partially: retain its slot if any shares matched.
     a.status=Number(order.sizeMatched||order.size_matched||0)>0?'matched':'REJECTED';a.reason='Exchange order status confirmed';
    }
    else if(['LIVE','OPEN','DELAYED'].includes(state)){
     const r=rows.find(r=>String(r.signal.yes_token_id)===String(a.asset_id));
     if(!weatherFresh||r?.signal?.action!=='BUY_LIVE'||!r.signal.new_entry_allowed||await stopped()){
      await client.cancelOrder({orderId:a.order_id});a.reason='Entry withdrawn; cancellation requested, awaiting confirmation';
     }
    }
    continue;
   }
   if(!a.signed_order_file){a.reason='Unknown legacy submission: missing signed checkpoint; manual exchange verification required';continue;}
   const row=rows.find(r=>String(r.signal.yes_token_id)===String(a.asset_id));
   const s=row?.signal,d=row?.distribution;
   if(!weatherFresh||s?.action!=='BUY_LIVE'||!s.new_entry_allowed||!d?.calibration_ready||d.probability_status!=='CALIBRATED'||!(s.net_edge>0)||s.best_ask>a.max_price||await stopped()){
    a.reason='Uncertain submission retained; current entry no longer eligible; no repost';continue;
   }
   const signed=await load(resolve(secretsRoot,a.signed_order_file),null);
   if(!signed){a.reason='Signed checkpoint unavailable; no repost';continue;}
   if(fetchBook){
    const book=await fetchBook(a.asset_id),asks=(book.asks||[]).filter(x=>Number(x.size)>0&&Number(x.price)>0);
    if(!Number.isFinite(Number(book.timestamp))||Math.abs(now()-Number(book.timestamp))>60000)throw new Error('stale retry book');
    const ask=Math.min(...asks.map(x=>Number(x.price))),rate=Number(s.fee_per_share)/(Number(s.best_ask)*(1-Number(s.best_ask)));
    const edge=Number(s.net_edge)+Number(s.best_ask)-a.max_price+Number(s.fee_per_share)-rate*a.max_price*(1-a.max_price);
    if(!Number.isFinite(ask)||ask>a.max_price||!Number.isFinite(edge)||edge<=0){a.reason='Original buy retry withheld: fresh price removes eligibility';continue;}
    entryLiquidity({bids:book.bids||[],entry:a.max_price,shares:1/ask,rate});
   }
   // Same signature/salt, not a new buy. A duplicate response is correlated to its order ID.
   try {
    const response=await client.postOrder(signed);
    a.order_id=response.orderId||response.orderID||null;
    a.status=a.order_id?(response.status||'POSTED'):'SUBMISSION_UNCERTAIN';
    if(response.success===false||['REJECTED','UNMATCHED'].includes(String(response.status).toUpperCase()))a.status='REJECTED';
    a.reason=a.order_id?null:'Submission still unconfirmed';
   }catch(e){
    const id=String(e?.message||'').match(/(0x[0-9a-fA-F]{64}).{0,30}Duplicated/i)?.[1];
    if(id){a.order_id=id;a.reason='Duplicate original order identified; awaiting exchange query';}
    else if(/couldn't be fully filled|fully filled or killed|no orders found to match|invalid amount|invalid order payload/i.test(String(e?.message||''))){a.status='REJECTED';a.reason='Original order explicitly rejected by exchange';}
    else a.reason='Submission still unconfirmed; original signed order retained';
   }
   a.updated_at=new Date(now()).toISOString();
  }catch{a.reason='Buy confirmation temporarily unavailable; next check scheduled';}
 }
}
