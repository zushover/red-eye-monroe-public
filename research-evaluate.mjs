// Independent temperature-forecast verification. Input contains no market data.
import {readFile,writeFile} from 'node:fs/promises';
import {resolve} from 'node:path';
const [input,output]=process.argv.slice(2);
if(!input||!output)throw Error('Usage: node research-evaluate.mjs input.json output.json');
const rows=JSON.parse(await readFile(resolve(input),'utf8'));
if(!Array.isArray(rows)||rows.length<30)throw Error('At least 30 independently dated samples required');
const groups=new Map(),seen=new Set();
for(const r of rows){
 if(!r.station||!r.model||!/^\d{4}-\d{2}-\d{2}$/.test(r.local_date)||!Number.isInteger(r.lead_hours)||r.lead_hours<0||!Number.isFinite(r.forecast_c)||!Number.isFinite(r.observed_c))throw Error('Invalid sample');
 const issue=Date.parse(r.issued_at),available=Date.parse(r.available_at),cutoff=Date.parse(r.cutoff_at),start=Date.parse(r.target_start),end=Date.parse(r.target_end);
 if(![issue,available,cutoff,start,end].every(Number.isFinite)||issue>available||available>cutoff||cutoff>start||end<=start||Math.abs((start-cutoff)/3600000-r.lead_hours)>1e-6)throw Error('Invalid availability, target window or lead time; historical leakage possible');
 if(r.observation_coverage<.9||r.observation_coverage>1||r.observation_source!=='station_observation')throw Error('Verified station observations and >=90% coverage required');
 const key=[r.station,r.model,r.lead_hours].join('/'),id=key+'/'+r.local_date;
 if(seen.has(id))throw Error('Duplicate date within station/model/lead group');seen.add(id);
 if(!groups.has(key))groups.set(key,[]);groups.get(key).push(r);
}
const results=[];
for(const [key,rs]of groups){rs.sort((a,b)=>a.local_date.localeCompare(b.local_date));let n=Math.floor(rs.length*.7);if(n<20||rs.length-n<10){results.push({group:key,status:'insufficient_independent_dates',samples:rs.length});continue}
 const train=rs.slice(0,n),test=rs.slice(n);if(Math.max(...train.map(r=>Date.parse(r.target_end)))>Math.min(...test.map(r=>Date.parse(r.cutoff_at))))throw Error('Training observations unavailable at first validation cutoff');
 const bias=train.reduce((s,r)=>s+r.observed_c-r.forecast_c,0)/n;
 const residual=train.map(r=>Math.abs(r.observed_c-r.forecast_c-bias)).sort((a,b)=>a-b),halfWidth=residual[Math.min(residual.length-1,Math.ceil(.9*(n+1))-1)];
 const mae=shift=>test.reduce((s,r)=>s+Math.abs(r.forecast_c+shift-r.observed_c),0)/test.length;
 results.push({group:key,status:'held_out_evaluation',training_dates:n,validation_dates:test.length,training_last_date:train.at(-1).local_date,validation_first_date:test[0].local_date,bias_c:bias,baseline_mae_c:mae(0),corrected_mae_c:mae(bias),empirical_90_width_c:halfWidth,validation_coverage:test.filter(r=>Math.abs(r.forecast_c+bias-r.observed_c)<=halfWidth).length/test.length,note:'Chronological split; empirical interval coverage is measured, not guaranteed. Parameters are not deployed.'});
}
await writeFile(resolve(output),JSON.stringify({created_at:new Date().toISOString(),results},null,2));console.log('Temperature verification report saved; no parameters deployed.');
