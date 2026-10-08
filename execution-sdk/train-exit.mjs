// Research only: chronological, purged future-Bid regression. No order imports.
import fs from 'node:fs';
import readline from 'node:readline';
const paths=new Map();let snapshots=0;
for await(const line of readline.createInterface({input:fs.createReadStream('data/snapshots.jsonl'),crlfDelay:Infinity})){
 let snap;try{snap=JSON.parse(line)}catch{continue} snapshots++;
 const t=Date.parse(snap.generated_at);if(!Number.isFinite(t))continue;
 for(const r of snap.reports||[])for(const s of r.signals||[]){
  if(!s.market_id||!(s.best_bid>0&&s.best_ask>s.best_bid&&s.best_ask<1))continue;
  const a=paths.get(s.market_id)||[];
  a.push({t,b:s.best_bid,a:s.best_ask,p:s.model_probability||0,q:r.distribution?.data_quality||0,h:r.distribution?.remaining_heating_hours||0,c:s.market_convergence||0,fee:s.fee_per_share||0,verified:s.book_verified===true,city:r.rule?.station_icao,date:r.rule?.local_date});paths.set(s.market_id,a);
 }
}
const features=o=>[1,o.a-o.b,o.p-o.a,o.q,Math.min(24,o.h)/24,o.c];
function fit(rows){const n=6,A=Array.from({length:n},()=>Array(n+1).fill(0));for(const r of rows){for(let i=0;i<n;i++){for(let j=0;j<n;j++)A[i][j]+=r.x[i]*r.x[j];A[i][n]+=r.x[i]*r.y}}for(let i=1;i<n;i++)A[i][i]+=1;for(let i=0;i<n;i++){let k=i;for(let j=i+1;j<n;j++)if(Math.abs(A[j][i])>Math.abs(A[k][i]))k=j;[A[i],A[k]]=[A[k],A[i]];const d=A[i][i];if(Math.abs(d)<1e-10)return null;for(let j=i;j<=n;j++)A[i][j]/=d;for(let k=0;k<n;k++)if(k!==i){const v=A[k][i];for(let j=i;j<=n;j++)A[k][j]-=v*A[i][j]}}return A.map(r=>r[n])}
const result={generated_at:new Date().toISOString(),snapshots,markets:paths.size,research_only:true,horizons:{},limitations:['Quotes are historical displayed Bid, not guaranteed execution; depth and queue position are not modeled.','Overlapping rows are not independent trades; sums are not portfolio returns.','Chronological split with horizon purge; city/day generalization remains unproven.','Live pricing and trading remain unchanged.']};
for(const minutes of [15,30,60]){
 const rows=[];for(const a of paths.values()){a.sort((x,y)=>x.t-y.t);let j=0;for(let i=0;i<a.length;i++){j=Math.max(j,i+1);while(j<a.length&&a[j].t<a[i].t+minutes*60000)j++;if(j>=a.length||a[j].t>a[i].t+(minutes+10)*60000)continue;rows.push({x:features(a[i]),y:a[j].b-a[i].b,o:a[i],end:a[j].t,future:a[j].b})}}
 rows.sort((a,b)=>a.o.t-b.o.t);if(rows.length<100){result.horizons[minutes]={samples:rows.length,status:'INSUFFICIENT'};continue}
 const split=rows[Math.floor(rows.length*.7)].o.t,train=rows.filter(r=>r.end<split),test=rows.filter(r=>r.o.t>=split),w=fit(train);if(!w){result.horizons[minutes]={status:'SINGULAR'};continue}
 let mae=0,zero=0,heuristic=0,selected=0,positive=0,netSum=0,verified=0;const days=new Set();
 for(const r of test){const pred=Math.max(-r.o.b,Math.min(1-r.o.b,w.reduce((s,v,i)=>s+v*r.x[i],0)));mae+=Math.abs(pred-r.y);zero+=Math.abs(r.y);const value=r.o.p-r.o.fee-.01-.025-.015,capture=.25+.5/(1+Math.max(0,r.o.h));const old=r.o.b+Math.min(.65,capture)*Math.max(0,value-r.o.b)-.25*(r.o.a-r.o.b);heuristic+=Math.abs(old-r.future);const rate=r.o.fee/(r.o.a*(1-r.o.a)),predBid=r.o.b+pred;const predictedNet=predBid-r.o.a-r.o.fee-rate*predBid*(1-predBid)-.02;if(predictedNet>0){selected++;const net=r.future-r.o.a-r.o.fee-rate*r.future*(1-r.future)-.02;positive+=net>0?1:0;netSum+=net;verified+=r.o.verified?1:0;days.add(r.o.city+'/'+r.o.date)}}
 result.horizons[minutes]={samples:rows.length,train:train.length,test:test.length,split:new Date(split).toISOString(),coefficients:w,feature_names:['intercept','spread','model_minus_ask','data_quality','remaining_hours_div24','concentration'],bid_move_mae:mae/test.length,no_change_mae:zero/test.length,heuristic_bid_mae:heuristic/test.length,selected_rows:selected,selected_verified_entry_rows:verified,selected_city_days:days.size,selected_positive_net_rate:selected?positive/selected:null,selected_mean_net_per_share:selected?netSum/selected:null,status:mae<zero?'BEATS_NO_CHANGE_RESEARCH_ONLY':'DOES_NOT_BEAT_NO_CHANGE'};
}
fs.mkdirSync('data/backtest',{recursive:true});fs.writeFileSync('data/backtest/exit-price-training.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result,null,2));
