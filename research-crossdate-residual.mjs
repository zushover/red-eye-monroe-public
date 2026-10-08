import fs from 'node:fs';
import path from 'node:path';

// Cross-date market-residual test. Fits on one target date, evaluates only on the other.
const root=process.cwd();
const source=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/bucket-probability-audit.json'),'utf8'));
const output=path.join(root,'data/backtest/crossdate-residual-audit.json');
const hours=[0,6,9,12];
const dates=[...new Set(source.rows.map(r=>r.date))].sort();
const clip=x=>Math.max(1e-9,x);
const mean=a=>a.reduce((s,x)=>s+x,0)/a.length;
function normalized(rows,key){const s=rows.reduce((q,r)=>q+r[key],0);return rows.map(r=>clip(r[key]/s));}
function eventsFor(hour,date){const groups=Object.values(Object.groupBy(source.rows.filter(r=>r.local_hour===hour&&r.date===date&&Number.isFinite(r.weather_p)&&r.weather_p>=0&&Number.isFinite(r.consensus_p)&&r.consensus_p>=0),r=>r.event_id));return groups.filter(v=>v.length>=2&&v.reduce((s,r)=>s+r.y,0)===1&&v.reduce((s,r)=>s+r.weather_p,0)>.99&&v.reduce((s,r)=>s+r.consensus_p,0)>.5);}
function score(events,alpha){const per=[];for(const rows of events){const w=normalized(rows,'weather_p'),m=normalized(rows,'consensus_p'),raw=w.map((x,i)=>Math.pow(clip(m[i]),1-alpha)*Math.pow(clip(x),alpha)),z=raw.reduce((s,x)=>s+x,0),p=raw.map(x=>x/z),winner=rows.findIndex(r=>r.y===1);per.push({event_id:rows[0].event_id,station:rows[0].station,logloss:-Math.log(clip(p[winner])),brier:p.reduce((s,x,i)=>s+(x-(i===winner?1:0))**2,0),top_hit:p.indexOf(Math.max(...p))===winner?1:0});}return {n:per.length,logloss:mean(per.map(x=>x.logloss)),brier:mean(per.map(x=>x.brier)),top_hit:mean(per.map(x=>x.top_hit)),per_event:per};}
const folds=[];
if(dates.length===2)for(const hour of hours)for(const [train,test] of [[dates[0],dates[1]],[dates[1],dates[0]]]){
  const trainEvents=eventsFor(hour,train),testEvents=eventsFor(hour,test);if(!trainEvents.length||!testEvents.length)continue;
  const curve=[];for(let i=0;i<=40;i++){const alpha=i/40,s=score(trainEvents,alpha);curve.push({alpha,brier:s.brier,logloss:s.logloss});}
  const selected=[...curve].sort((a,b)=>(a.logloss+.25*a.brier)-(b.logloss+.25*b.brier))[0];
  const market=score(testEvents,0),weather=score(testEvents,1),model=score(testEvents,selected.alpha);
  folds.push({hour,train_date:train,test_date:test,train_events:trainEvents.length,test_events:testEvents.length,selected_alpha:selected.alpha,training_curve:curve,test:{market:{n:market.n,brier:market.brier,logloss:market.logloss,top_hit:market.top_hit},weather:{n:weather.n,brier:weather.brier,logloss:weather.logloss,top_hit:weather.top_hit},crossfit:{n:model.n,brier:model.brier,logloss:model.logloss,top_hit:model.top_hit},brier_delta_vs_market:model.brier-market.brier,logloss_delta_vs_market:model.logloss-market.logloss},per_event:model.per_event.map((x,i)=>({...x,market_brier:market.per_event[i].brier,market_logloss:market.per_event[i].logloss,brier_delta:x.brier-market.per_event[i].brier,logloss_delta:x.logloss-market.per_event[i].logloss}))});
}
const aggregate={};for(const hour of hours){const f=folds.filter(x=>x.hour===hour),n=f.reduce((s,x)=>s+x.test_events,0);if(!n)continue;aggregate[hour]={folds:f.length,events:n,selected_alpha_by_fold:f.map(x=>({train:x.train_date,alpha:x.selected_alpha})),market_brier:f.reduce((s,x)=>s+x.test.market.brier*x.test_events,0)/n,crossfit_brier:f.reduce((s,x)=>s+x.test.crossfit.brier*x.test_events,0)/n,market_logloss:f.reduce((s,x)=>s+x.test.market.logloss*x.test_events,0)/n,crossfit_logloss:f.reduce((s,x)=>s+x.test.crossfit.logloss*x.test_events,0)/n};aggregate[hour].brier_delta=aggregate[hour].crossfit_brier-aggregate[hour].market_brier;aggregate[hour].logloss_delta=aggregate[hour].crossfit_logloss-aggregate[hour].market_logloss;}
const result={generated_at:new Date().toISOString(),dates,method:'Two-fold leave-one-target-date-out log-opinion pool: p proportional to market^(1-alpha)*weather^alpha. Alpha in [0,1] selected on training date by logloss + 0.25*Brier, evaluated only on other date.',auto_apply:false,limitations:['Only two target dates; this tests transport between these two dates, not long-run generalization.','Market consensus is a derived archived estimate, not necessarily executable.','Model versions changed during collection.','Alpha is constrained nonnegative; the test asks whether weather helps, not whether it can be used contrarian.'],aggregate,folds};
fs.writeFileSync(output,JSON.stringify(result));
console.log(JSON.stringify({output,dates,aggregate},null,2));
