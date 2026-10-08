export function entryLiquidity({bids,entry,shares,rate}) {
 if(!Number.isFinite(entry)||entry<=0||entry>=1||!Number.isFinite(shares)||shares<=0||!Number.isFinite(rate)||rate<0||rate>1)throw new Error('invalid entry risk inputs');
 const levels=bids.filter(x=>Number.isFinite(Number(x.price))&&Number(x.price)>0&&Number(x.price)<1&&Number.isFinite(Number(x.size))&&Number(x.size)>0).map(x=>({price:Number(x.price),size:Number(x.size)})).sort((a,b)=>b.price-a.price);
 let left=shares,value=0;
 for(const x of levels){const n=Math.min(left,x.size);value+=n*x.price*(1-rate*(1-x.price));left-=n;if(left<=1e-8)break;}
 if(left>1e-8)throw new Error('entry blocked: insufficient full-position exit depth');
 const cost=shares*entry*(1+rate*(1-entry));
 if(!Number.isFinite(cost)||cost<=0||!Number.isFinite(value)||value/cost<.80-1e-10)throw new Error('entry blocked: immediate fee-inclusive liquidation loss exceeds 20%');
 return {bid:levels[0].price,liquidation_loss:1-value/cost};
}
export function reentryGuard({attempts,group,tokenGroups,signal,snapshotAt,now=Date.now()}) {
 const exits=attempts.filter(a=>a.side==='SELL'&&a.status==='CLOSED'&&(a.group_key===group||tokenGroups.get(String(a.asset_id))===group));
 const exit=exits.at(-1);if(!exit)return;
 const previous=[...attempts].reverse().find(a=>a.side==='BUY'&&a.status!=='REJECTED'&&a.asset_id===exit.asset_id&&Date.parse(a.created_at)<Date.parse(exit.created_at));
 const closed=Date.parse(previous?.closed_at||exit.updated_at||exit.created_at);
 if(now-closed<30*60000)throw new Error('exit reentry cooldown: 30 minutes');
 if(!/STOP|MODEL_DROP/.test(exit.reason||'')){if(!Number.isFinite(closed)||!Number.isFinite(Date.parse(snapshotAt))||Date.parse(snapshotAt)<=closed)throw new Error('exit reentry requires newer snapshot');return;}
 if(String(signal.yes_token_id||previous?.asset_id)!==String(previous?.asset_id))throw new Error('stop-loss reentry to another temperature requires review');
 if(!Number.isFinite(closed)||!Number.isFinite(Date.parse(snapshotAt))||Date.parse(snapshotAt)<=closed||!Number.isFinite(Number(previous?.model_probability))||!Number.isFinite(Number(signal.model_probability))||Number(signal.model_probability)<Number(previous.model_probability)+.03)throw new Error('stop-loss reentry requires a newer snapshot and model improvement of 3pp');
}
