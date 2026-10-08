import fs from 'node:fs';
import path from 'node:path';

// Diagnostic only: decompose archived weather, market-consensus, and blended probabilities.
const root=process.cwd();
const src=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/bucket-probability-audit.json'),'utf8'));
const out=path.join(root,'data/backtest/probability-decomposition.json');
const hours=[0,6,9,12];
let seed=7419;const random=()=>((seed=(1664525*seed+1013904223)>>>0)/4294967296);
function normalize(rows,key){const sum=rows.reduce((s,r)=>s+r[key],0);return rows.map(r=>Math.max(1e-9,r[key]/sum));}
function eventScore(rows,key){const p=normalize(rows,key),winner=rows.findIndex(r=>r.y===1);return {brier:p.reduce((s,v,i)=>s+(v-(i===winner?1:0))**2,0),logloss:-Math.log(p[winner]),top_hit:p.indexOf(Math.max(...p))===winner?1:0,top_p:Math.max(...p)};}
function blendScore(rows,w){const weather=normalize(rows,'weather_p'),market=normalize(rows,'consensus_p'),winner=rows.findIndex(r=>r.y===1),p=weather.map((v,i)=>w*v+(1-w)*market[i]);return {brier:p.reduce((s,v,i)=>s+(v-(i===winner?1:0))**2,0),logloss:-Math.log(Math.max(1e-9,p[winner]))};}
function mean(a){return a.reduce((s,x)=>s+x,0)/a.length;}
function pairedCI(events,a,b,key){const d=events.map(e=>eventScore(e,a)[key]-eventScore(e,b)[key]),values=[];for(let n=0;n<5000;n++){let s=0;for(let i=0;i<d.length;i++)s+=d[Math.floor(random()*d.length)];values.push(s/d.length);}values.sort((x,y)=>x-y);return {difference:mean(d),ci95:[values[124],values[4874]]};}
const phases={};
for(const hour of hours){
  const all=Object.values(Object.groupBy(src.rows.filter(r=>r.local_hour===hour&&Number.isFinite(r.weather_p)&&r.weather_p>=0&&Number.isFinite(r.consensus_p)&&r.consensus_p>=0&&Number.isFinite(r.p)&&r.p>=0),r=>r.event_id));
  const events=all.filter(v=>v.length>=2&&v.reduce((s,r)=>s+r.y,0)===1&&v.reduce((s,r)=>s+r.weather_p,0)>.99&&v.reduce((s,r)=>s+r.consensus_p,0)>.5);
  const summary={};for(const key of ['weather_p','consensus_p','p']){const s=events.map(e=>eventScore(e,key));summary[key]={brier:mean(s.map(x=>x.brier)),logloss:mean(s.map(x=>x.logloss)),top_hit:mean(s.map(x=>x.top_hit)),top_p:mean(s.map(x=>x.top_p))};}
  const weight_curve=[];for(let i=0;i<=20;i++){const w=i/20,s=events.map(e=>blendScore(e,w));weight_curve.push({weather_weight:w,brier:mean(s.map(x=>x.brier)),logloss:mean(s.map(x=>x.logloss))});}
  phases[hour]={events:events.length,dates:[...new Set(events.flatMap(e=>e.map(r=>r.date)))].sort(),average_runtime_weather_weight:mean(events.map(e=>mean(e.filter(r=>Number.isFinite(r.weather_weight)).map(r=>r.weather_weight)))),summary,weather_vs_consensus_brier:pairedCI(events,'weather_p','consensus_p','brier'),blend_vs_consensus_brier:pairedCI(events,'p','consensus_p','brier'),weight_curve};
}
const result={generated_at:new Date().toISOString(),method:'Within each resolved event, normalize all mutually exclusive bucket probabilities to sum to one; compare raw weather probability, archived market consensus, and runtime blend. Paired uncertainty resamples whole events.',limitations:['Only two target dates; confidence intervals reflect event sampling, not date-to-date generalization.','Market consensus is an archived derived estimate, not guaranteed executable price.','Runtime model versions changed during collection.','Retrospective weather-weight curves are diagnostic and must not be deployed without new-date validation.'],phases};
fs.writeFileSync(out,JSON.stringify(result));
console.log(JSON.stringify({output:out,phases:Object.fromEntries(Object.entries(phases).map(([h,p])=>[h,{events:p.events,weather:p.summary.weather_p,consensus:p.summary.consensus_p,blend:p.summary.p,weatherVsMarket:p.weather_vs_consensus_brier,blendVsMarket:p.blend_vs_consensus_brier}]))},null,2));
