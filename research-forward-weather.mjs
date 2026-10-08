import fs from 'node:fs';
import readline from 'node:readline';
import {Readable} from 'node:stream';
import path from 'node:path';

// Strictly post-training weather check. Public read-only APIs, frozen 2025-08-20 parameters.
const root=process.cwd();
const old=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/city-model-research/report.json'),'utf8'));
const out=path.join(root,'data/backtest/city-model-research/forward-weather.json');
const start='2025-08-21',end='2026-07-14';
const stations=[
  {icao:'KMIA',ghcnh:'USW00012839',lat:25.78805,lon:-80.31694,timezone:'America/New_York'},
  {icao:'KAUS',ghcnh:'USW00013904',lat:30.183,lon:-97.68,timezone:'America/Chicago'},
  {icao:'EDDM',ghcnh:'GMI0000EDDM',lat:48.354,lon:11.786,timezone:'Europe/Berlin'},
  {icao:'KATL',ghcnh:'USW00013874',lat:33.64,lon:-84.427,timezone:'America/New_York'},
  {icao:'KDAL',ghcnh:'USW00013960',lat:32.847,lon:-96.852,timezone:'America/Chicago'},
  {icao:'EHAM',ghcnh:'NLMU0029295',lat:52.315,lon:4.79,timezone:'Europe/Amsterdam'},
  {icao:'KLGA',ghcnh:'USW00014732',lat:40.779,lon:-73.88,timezone:'America/New_York'},
  {icao:'KLAX',ghcnh:'USW00023174',lat:33.938,lon:-118.389,timezone:'America/Los_Angeles'},
  {icao:'WSSS',ghcnh:'SNI0000WSSS',lat:1.364,lon:103.991,timezone:'Asia/Singapore'},
  {icao:'ZSPD',ghcnh:'CHI0000ZSPD',lat:31.145,lon:121.793,timezone:'Asia/Shanghai'},
  {icao:'LFPG',ghcnh:'FRI0000LFPG',lat:49.01,lon:2.548,timezone:'Europe/Paris'},
];
const names=['ECMWF IFS 0.25°','NCEP GFS seamless','Open-Meteo best match'];
const formatters=new Map();
function localDate(iso,tz) {
  if(!formatters.has(tz))formatters.set(tz,new Intl.DateTimeFormat('en-CA',{timeZone:tz,year:'numeric',month:'2-digit',day:'2-digit'}));
  const p=Object.fromEntries(formatters.get(tz).formatToParts(new Date(iso)).map(x=>[x.type,x.value]));
  return `${p.year}-${p.month}-${p.day}`;
}
async function observations(station) {
  const exact=new Map(),all=new Map();const coverage=[];
  const add=(daily,date,value)=>{const old=daily.get(date);daily.set(date,{max:old?Math.max(old.max,value):value,count:(old?.count||0)+1});};
  for(const year of [2025,2026]) {
    const url=`https://www.ncei.noaa.gov/oa/global-historical-climatology-network/hourly/access/by-year/${year}/psv/GHCNh_${station.ghcnh}_${year}.psv`;
    const response=await fetch(url,{signal:AbortSignal.timeout(90000)});
    if(!response.ok)throw Error(`${station.icao} GHCNh ${year}: HTTP ${response.status}`);
    const lines=readline.createInterface({input:Readable.fromWeb(response.body),crlfDelay:Infinity});
    let idx,accepted=0,exactAccepted=0,latest='';
    for await(const line of lines) {
      if(!idx){idx=Object.fromEntries(line.split('|').map((x,i)=>[x,i]));continue;}
      const p=line.split('|');const utc=p[idx.DATE],value=Number(p[idx.temperature]);
      if(!utc)continue;latest=utc;
      if(p[idx.temperature_Report_Type]!=='FM15'||p[idx.temperature_Quality_Code]!=='1'||!Number.isFinite(value))continue;
      const date=localDate(`${utc}Z`,station.timezone);
      if(date<start||date>end)continue;
      add(all,date,value);accepted++;if(p[idx.temperature_Source_Station_ID]===`ICAO-${station.icao}`){add(exact,date,value);exactAccepted++;}
    }
    coverage.push({year,latest_utc:latest,accepted,exact_icao_accepted:exactAccepted});
  }
  const exactComplete=[...exact.values()].filter(x=>x.count>=12).length,allComplete=[...all.values()].filter(x=>x.count>=12).length,useExact=exactComplete>=Math.max(30,allComplete*.5);
  return {daily:useExact?exact:all,coverage,observation_selection:useExact?'FM15 QC=1 exact ICAO source':'FM15 QC=1 within exact GHCNh station file',exact_complete_days:exactComplete,all_complete_days:allComplete};
}
async function forecast(station,model) {
  const u=new URL('https://previous-runs-api.open-meteo.com/v1/forecast');
  u.searchParams.set('latitude',station.lat);u.searchParams.set('longitude',station.lon);u.searchParams.set('timezone',station.timezone);u.searchParams.set('start_date',start);u.searchParams.set('end_date',end);u.searchParams.set('hourly','temperature_2m_previous_day1');if(model)u.searchParams.set('models',model);
  const response=await fetch(u,{signal:AbortSignal.timeout(90000)});if(!response.ok)throw Error(`${station.icao} forecast ${model||'best'}: HTTP ${response.status}`);
  const x=await response.json();const out=new Map();const t=x.hourly?.time||[],v=x.hourly?.temperature_2m_previous_day1||[];
  for(let i=0;i<t.length;i++){const value=v[i],date=t[i].slice(0,10);if(!Number.isFinite(value))continue;out.set(date,Math.max(out.get(date)??-Infinity,value));}
  return out;
}
function predict(row,fit){const v=names.map(n=>row.forecasts_c[n]),m=v.reduce((s,x)=>s+x,0)/3;const p=fit.parameters;
  if(fit.selected.type==='ridge'){const month=Number(row.date.slice(5,7)),x=[1,...v.map(x=>x-m),m,...(fit.selected.seasonal?[Math.sin(month*Math.PI/6),Math.cos(month*Math.PI/6),Math.max(...v)-Math.min(...v)]:[])];return x.reduce((s,value,j)=>s+p.coef[j]*(value-p.avg[j])/p.sd[j],0);}
  return fit.selected.sources.reduce((s,i)=>s+v[i],0)/fit.selected.sources.length+p.bias;
}
const results=[];
for(const station of stations) {
  console.log(`Fetching ${station.icao}...`);
  try{
    const obs=await observations(station);
    const [ec,gfs,best]=await Promise.all([forecast(station,'ecmwf_ifs025'),forecast(station,'gfs_seamless'),forecast(station,'')]);
    const fit=old.stations.find(x=>x.icao===station.icao);
    const rows=[];
    for(const [date,o] of obs.daily){if(o.count<12||![ec,gfs,best].every(m=>m.has(date)))continue;const forecasts_c=Object.fromEntries(names.map((n,i)=>[n,[ec,gfs,best][i].get(date)]));const r={date,observed_c:o.max,metar_count:o.count,forecasts_c};r.baseline_c=(forecasts_c[names[0]]+forecasts_c[names[1]])/2;r.frozen_candidate_c=predict(r,fit);rows.push(r);}
    rows.sort((a,b)=>a.date.localeCompare(b.date));
    const score=key=>{const e=rows.map(r=>r[key]-r.observed_c);return {n:e.length,mae_c:e.reduce((s,x)=>s+Math.abs(x),0)/e.length,rmse_c:Math.sqrt(e.reduce((s,x)=>s+x*x,0)/e.length),bias_c:e.reduce((s,x)=>s+x,0)/e.length};};
    const result={icao:station.icao,ghcnh:station.ghcnh,frozen_model:fit.selected.name,training_end:fit.dates.train_end,prior_research_end:fit.dates.end,coverage:obs.coverage,observation_selection:obs.observation_selection,exact_complete_days:obs.exact_complete_days,all_complete_days:obs.all_complete_days,first:rows[0]?.date,last:rows.at(-1)?.date,baseline:score('baseline_c'),candidate:score('frozen_candidate_c'),rows};
    results.push(result);fs.writeFileSync(out,JSON.stringify({generated_at:new Date().toISOString(),start,end,auto_apply:false,method:'Candidate trained no later than 2025-03-14 and previously studied through 2025-08-20; NOAA GHCNh FM15 ICAO source QC=1 local daily maximum vs Open-Meteo rolling previous_day1',stations:results}));
    console.log(JSON.stringify({icao:station.icao,n:rows.length,baseline:result.baseline.mae_c,candidate:result.candidate.mae_c,first:result.first,last:result.last}));
  }catch(e){console.error(`${station.icao}: ${e}`);results.push({icao:station.icao,error:String(e)});}
}
fs.writeFileSync(out,JSON.stringify({generated_at:new Date().toISOString(),start,end,auto_apply:false,method:'Candidate trained no later than 2025-03-14 and previously studied through 2025-08-20; NOAA GHCNh FM15 ICAO source QC=1 local daily maximum vs Open-Meteo rolling previous_day1',limitations:['GHCNh FM15 hourly temperature maximum is not guaranteed to match official Polymarket settlement maximum.','Open-Meteo previous_day1 is a rolling 24-hour lead, not a fixed local time forecast.','Only three stations with exact GHCNh ICAO mappings are assessed.'],stations:results}));
