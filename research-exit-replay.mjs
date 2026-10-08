import fs from 'node:fs';
import readline from 'node:readline';
import path from 'node:path';

// Hypothetical displayed-quote replay. No credentials, orders, or production writes.
const root=process.cwd();
const audit=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/bucket-probability-audit.json'),'utf8'));
const out=path.join(root,'data/backtest/exit-replay-audit.json');
const horizons=[15,30,60];
const maxGrace=15;
const grouped=Object.groupBy(audit.rows,r=>`${r.local_hour}|${r.event_id}`);
const candidates=[];
const exclusion={};
for(const group of Object.values(grouped)) {
  const eligible=group.filter(r=>{
    if (!(Number.isFinite(r.p)&&r.p>=0&&r.p<=1&&Number.isFinite(r.bid)&&Number.isFinite(r.ask))) {exclusion.invalid_quote=(exclusion.invalid_quote||0)+1;return false;}
    if (!(r.ask>=.10&&r.ask<=.70)) {exclusion.ask_range=(exclusion.ask_range||0)+1;return false;}
    if (!(r.bid>0&&r.bid<=r.ask&&r.ask-r.bid<=.05)) {exclusion.spread=(exclusion.spread||0)+1;return false;}
    if (!(r.p-r.ask>=.05)) {exclusion.model_gap=(exclusion.model_gap||0)+1;return false;}
    return true;
  });
  eligible.sort((a,b)=>(b.p-b.ask)-(a.p-a.ask));
  if(eligible.length) candidates.push({...eligible[0],entry_fee:null,exit:{}});
}
const byMarket=new Map();
for(const c of candidates) {if(!byMarket.has(c.market_id))byMarket.set(c.market_id,[]);byMarket.get(c.market_id).push(c);}
let snapshotLines=0;
const input=readline.createInterface({input:fs.createReadStream(path.join(root,'data/snapshots.jsonl')),crlfDelay:Infinity});
for await(const line of input) {
  snapshotLines++;
  let snap;try{snap=JSON.parse(line);}catch{continue;}
  const now=Date.parse(snap.generated_at);
  if(!Number.isFinite(now))continue;
  for(const report of snap.reports||[])for(const signal of report.signals||[]) {
    const list=byMarket.get(String(signal.market_id));
    if(!list)continue;
    for(const c of list) {
      const age=(now-Date.parse(c.asof))/60000;
      if(age===0 && Number.isFinite(signal.fee_per_share))c.entry_fee=signal.fee_per_share;
      if(age<0 || age>75)continue;
      for(const h of horizons) {
        if(c.exit[h] || age<h || age>h+maxGrace)continue;
        if(!(Number.isFinite(signal.best_bid)&&signal.best_bid>0))continue;
        c.exit[h]={asof:snap.generated_at,minutes:age,bid:signal.best_bid,fee:Number.isFinite(signal.fee_per_share)?signal.fee_per_share:null};
      }
    }
  }
}
const byPhase={};
for(const hour of [0,6,9,12]) {
  const own=candidates.filter(c=>c.local_hour===hour);
  byPhase[hour]={candidates:own.length,horizons:{}};
  for(const h of horizons) {
    const filled=own.filter(c=>c.exit[h]&&c.entry_fee!==null&&c.exit[h].fee!==null);
    // 1c entry and exit impact is a stress assumption, not a measured spread or fill.
    const trades=filled.map(c=>({event_id:c.event_id,market_id:c.market_id,station:c.station,bucket:c.bucket,date:c.date,entry_asof:c.asof,exit_asof:c.exit[h].asof,p:c.p,ask:c.ask,bid:c.exit[h].bid,net_per_share:c.exit[h].bid-c.ask-c.entry_fee-c.exit[h].fee-.02}));
    byPhase[hour].horizons[h]={evaluated:trades.length,no_future_or_fee:own.length-trades.length,mean_net_per_share:trades.length?trades.reduce((s,t)=>s+t.net_per_share,0)/trades.length:null,positive_fraction:trades.length?trades.filter(t=>t.net_per_share>0).length/trades.length:null,trades};
  }
}
const result={generated_at:new Date().toISOString(),snapshot_lines:snapshotLines,entry_rule:'At first snapshot within 30 min after local 00/06/09/12, one bucket per event with max p-ask; ask 10-70c, spread <=5c, p-ask >=5pp',exit_rule:'First displayed Bid 15/30/60 minutes later, up to 15-minute grace; subtract recorded entry and exit fee per share plus assumed 1c impact each side',candidate_count:candidates.length,exclusion,limitations:['This is not an order fill simulation; displayed best Ask/Bid may lack $1 depth and may move before execution.','The entry rule is a deliberately simple research screen, not the production strategy.','The same event can appear in multiple phase analyses; phase groups are not independent.','Two target dates and model-version changes are insufficient for generalization.','Unmatched future quotes are excluded, which may bias results upward.'],by_phase:byPhase};
fs.writeFileSync(out,JSON.stringify(result));
console.log(JSON.stringify({output:out,candidates:candidates.length,byPhase:Object.fromEntries(Object.entries(byPhase).map(([h,x])=>[h,{candidates:x.candidates,horizons:Object.fromEntries(Object.entries(x.horizons).map(([k,v])=>[k,{evaluated:v.evaluated,mean_net_per_share:v.mean_net_per_share,positive_fraction:v.positive_fraction}]))}]))},null,2));
