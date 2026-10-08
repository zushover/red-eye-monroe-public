import fs from 'node:fs';
import readline from 'node:readline';
import path from 'node:path';

// Read-only research join: archived snapshot probabilities vs public settled Gamma outcomes.
// Does not read credentials, change live settings, or submit orders.
const root = process.cwd();
const snapshots = path.join(root, 'data/snapshots.jsonl');
const cachePath = path.join(root, 'data/backtest/settled-weather-events.json');
const outPath = path.join(root, 'data/backtest/bucket-probability-audit.json');
const cache = fs.existsSync(cachePath) ? JSON.parse(fs.readFileSync(cachePath, 'utf8')) : {};
const slots = [0, 6, 9, 12];
const chosen = new Map();
const fmtCache = new Map();
function localHour(iso, timezone) {
  if (!fmtCache.has(timezone)) fmtCache.set(timezone, new Intl.DateTimeFormat('en-CA', {timeZone:timezone, year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', hourCycle:'h23'}));
  const parts=Object.fromEntries(fmtCache.get(timezone).formatToParts(new Date(iso)).map(p=>[p.type,p.value]));
  return {date:`${parts.year}-${parts.month}-${parts.day}`, minute:Number(parts.hour)*60+Number(parts.minute)};
}
let lines=0, parsed=0;
const input=readline.createInterface({input:fs.createReadStream(snapshots),crlfDelay:Infinity});
for await (const line of input) {
  lines++;
  let snap;
  try {snap=JSON.parse(line);parsed++;} catch {continue;}
  for (const r of snap.reports||[]) {
    const eventId=String(r.event?.id||'');
    const targetDate=r.rule?.local_date;
    const zone=r.station?.timezone;
    if (!eventId || !targetDate || !zone || !Array.isArray(r.signals)) continue;
    let time;
    try {time=localHour(snap.generated_at,zone);} catch {continue;}
    if (time.date!==targetDate) continue;
    for (const hour of slots) {
      const delay=time.minute-hour*60;
      if (delay<0 || delay>30) continue;
      const key=`${eventId}|${hour}`;
      const old=chosen.get(key);
      if (old && old.delay<=delay) continue;
      const markets=r.signals.filter(s=>s.market_id && Number.isFinite(s.model_probability)).map(s=>({market_id:String(s.market_id),bucket:s.bucket,p:Number(s.model_probability),weather_p:Number.isFinite(s.weather_probability)?s.weather_probability:null,consensus_p:Number.isFinite(s.market_consensus_probability)?s.market_consensus_probability:null,weather_weight:Number.isFinite(s.weather_weight)?s.weather_weight:null,book_verified:s.book_verified===true,bid:s.best_bid,ask:s.best_ask}));
      if (markets.length) chosen.set(key,{event_id:eventId,title:r.event.title,station:r.station.icao,date:targetDate,local_hour:hour,asof:snap.generated_at,delay,markets});
    }
  }
}
const eventIds=[...new Set([...chosen.values()].map(x=>x.event_id))];
for (const id of eventIds) {
  if (cache[id]) continue;
  try {
    const response=await fetch(`https://gamma-api.polymarket.com/events/${encodeURIComponent(id)}`,{signal:AbortSignal.timeout(15000)});
    if (!response.ok) throw Error(`HTTP ${response.status}`);
    const event=await response.json();
    const markets={};
    for (const m of event.markets||[]) {
      let prices;
      try {prices=typeof m.outcomePrices==='string'?JSON.parse(m.outcomePrices):m.outcomePrices;} catch {continue;}
      if (m.closed && Array.isArray(prices) && prices.length>=2 && (Number(prices[0])===0 || Number(prices[0])===1) && Number(prices[0])+Number(prices[1])===1) markets[String(m.id)]=Number(prices[0]);
    }
    cache[id]={closed:!!event.closed,markets,checked_at:new Date().toISOString()};
  } catch(e) {cache[id]={error:String(e),checked_at:new Date().toISOString()};}
  await new Promise(resolve=>setTimeout(resolve,180));
}
fs.writeFileSync(cachePath,JSON.stringify(cache,null,2));
const rows=[];
for(const c of chosen.values()) for(const m of c.markets) {
  const y=cache[c.event_id]?.markets?.[m.market_id];
  if (y!==0 && y!==1) continue;
  rows.push({...c,markets:undefined,delay:undefined,...m,y});
}
const byPhase={};
for(const hour of slots) {
  const a=rows.filter(r=>r.local_hour===hour),events=new Set(a.map(r=>r.event_id));
  const brier=a.length?a.reduce((sum,r)=>sum+(r.p-r.y)**2,0)/a.length:null;
  const logloss=a.length?a.reduce((sum,r)=>sum-(r.y*Math.log(Math.max(1e-6,r.p))+(1-r.y)*Math.log(Math.max(1e-6,1-r.p))),0)/a.length:null;
  const bins=Array.from({length:10},(_,i)=>{const v=a.filter(r=>r.p>=i/10 && (i===9?r.p<=1:r.p<(i+1)/10));return {from:i/10,to:(i+1)/10,n:v.length,p:v.length?v.reduce((s,r)=>s+r.p,0)/v.length:null,observed:v.length?v.reduce((s,r)=>s+r.y,0)/v.length:null};});
  byPhase[hour]={markets:a.length,events:events.size,brier,logloss,bins};
}
const output={generated_at:new Date().toISOString(),snapshot_lines:lines,parsed_lines:parsed,matched_slots:chosen.size,event_ids:eventIds.length,settled_rows:rows.length,method:'first snapshot within 30 minutes after local 00, 06, 09, or 12; same event and exact market id joined to public Gamma resolved Yes outcome',limitations:['Small and temporally clustered sample, not independent forward validation.','Gamma terminal prices are used only to identify settled binary outcomes, never as historical tradable prices.','Model probabilities are archived runtime values; model versions changed during this window.','Some events may remain unresolved; those are excluded.'],by_phase:byPhase,rows};
fs.writeFileSync(outPath,JSON.stringify(output));
console.log(JSON.stringify({output:outPath,lines,parsed,slots:chosen.size,eventIds:eventIds.length,settledRows:rows.length,byPhase:Object.fromEntries(Object.entries(byPhase).map(([k,v])=>[k,{markets:v.markets,events:v.events,brier:v.brier,logloss:v.logloss}]))},null,2));
