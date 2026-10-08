import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {runInNewContext} from 'node:vm';
test('globe shows actual held temperature, not highest-ranked alternative',()=>{
 const code=readFileSync('internal/dashboard/index.html','utf8').split('\n').filter(x=>x.startsWith('function renderGlobeSelection(')).at(-1);
 const target={innerHTML:''},context={$:()=>target,esc:String,cityName:()=> 'Munich',temp:String,pct:String,bucketLabel:String,marketState:()=>({color:'red',label:'真实持仓',conv:.4}),liveStatus:{positions:[{asset_id:'held27',current_size:6.25,status:'OPEN'}]}};
 runInNewContext(code,context);
 context.renderGlobeSelection({rule:{station_icao:'EDDM',local_date:'2026-09-16'},distribution:{},signals:[{yes_token_id:'other28',bucket:'28°C',net_edge:.2},{yes_token_id:'held27',bucket:'27°C',net_edge:.01}]});
 assert.match(target.innerHTML,/已持仓/);assert.match(target.innerHTML,/27°C/);assert.doesNotMatch(target.innerHTML,/28°C/);
});
