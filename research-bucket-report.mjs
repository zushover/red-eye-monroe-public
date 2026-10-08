import fs from 'node:fs';
import path from 'node:path';

const root=process.cwd();
const data=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/bucket-probability-audit.json'),'utf8'));
const replay=JSON.parse(fs.readFileSync(path.join(root,'data/backtest/exit-replay-audit.json'),'utf8'));
const hours=[0,6,9,12];
const phase={0:'00:00',6:'06:00',9:'09:00',12:'12:00'};
const fmt=(x,d=3)=>Number.isFinite(x)?x.toFixed(d):'—';
const valid=r=>Number.isFinite(r.bid)&&Number.isFinite(r.ask)&&r.bid>0&&r.ask>0&&r.ask>=r.bid&&r.ask<=1;
const rows=hours.map(h=>{
  const a=data.rows.filter(r=>r.local_hour===h);
  const q=a.filter(valid);
  const groups=Object.values(Object.groupBy(a,r=>r.event_id)).filter(v=>v.reduce((s,r)=>s+r.y,0)===1);
  const top=groups.map(v=>[...v].sort((x,y)=>y.p-x.p)[0]);
  return {hour:h,events:groups.length,buckets:a.length,quoted:q.length,topHit:top.length?top.reduce((s,r)=>s+r.y,0)/top.length:null,topP:top.length?top.reduce((s,r)=>s+r.p,0)/top.length:null,modelBrier:q.length?q.reduce((s,r)=>s+(r.p-r.y)**2,0)/q.length:null,marketBrier:q.length?q.reduce((s,r)=>s+((r.bid+r.ask)/2-r.y)**2,0)/q.length:null};
});
const binRows=hours.flatMap(h=>data.by_phase[h].bins.filter(b=>b.n).map(b=>({...b,hour:h})));
function reliability(h) {
  const bins=data.by_phase[h].bins.filter(b=>b.n>=10);
  const W=520,H=280,L=48,R=18,T=17,B=42,X=v=>L+v*(W-L-R),Y=v=>H-B-v*(H-T-B);
  const grid=[0,.2,.4,.6,.8,1].map(v=>`<line x1="${X(v)}" y1="${Y(0)}" x2="${X(v)}" y2="${Y(1)}" stroke="#28384a"/><line x1="${X(0)}" y1="${Y(v)}" x2="${X(1)}" y2="${Y(v)}" stroke="#28384a"/><text x="${X(v)}" y="${H-24}" text-anchor="middle" fill="#adbbca" font-size="11">${v.toFixed(1)}</text><text x="${L-8}" y="${Y(v)+4}" text-anchor="end" fill="#adbbca" font-size="11">${v.toFixed(1)}</text>`).join('');
  const dots=bins.map(b=>`<circle cx="${X(b.p).toFixed(1)}" cy="${Y(b.observed).toFixed(1)}" r="${Math.max(5,Math.min(13,Math.sqrt(b.n)))}" fill="#ff6b82" fill-opacity=".82"><title>预测 ${fmt(b.p,2)}，实际 ${fmt(b.observed,2)}，${b.n} 档</title></circle>`).join('');
  return `<article><h3>当地 ${phase[h]} · ${data.by_phase[h].events} 个事件</h3><svg viewBox="0 0 ${W} ${H}" role="img" aria-label="${phase[h]} reliability curve"><rect x="${L}" y="${T}" width="${W-L-R}" height="${H-T-B}" fill="none" stroke="#607085"/>${grid}<line x1="${X(0)}" y1="${Y(0)}" x2="${X(1)}" y2="${Y(1)}" stroke="#9aacc1" stroke-dasharray="5 5"/>${dots}<text x="${W/2}" y="${H-4}" text-anchor="middle" fill="#dce7f4" font-size="12">预测概率</text><text x="12" y="${H/2}" transform="rotate(-90 12 ${H/2})" text-anchor="middle" fill="#dce7f4" font-size="12">实际命中率</text></svg><p class="small">圆点大小 ∝ 档位样本量；少于 10 条的概率分组未画出。</p></article>`;
}
const dates=[...new Set(data.rows.map(r=>r.date))].sort();
const table=rows.map(r=>`<tr><td>${phase[r.hour]}</td><td>${r.events}</td><td>${r.buckets}</td><td>${r.quoted}</td><td>${fmt(r.topP,1)}</td><td>${fmt(r.topHit,1)}</td><td>${fmt(r.modelBrier)}</td><td>${fmt(r.marketBrier)}</td><td class="${r.modelBrier<r.marketBrier?'good':'bad'}">${fmt(r.modelBrier-r.marketBrier)}</td></tr>`).join('');
const html=`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RED EYE MONROE · 市场温度档校准</title><style>body{margin:0;background:#0b111b;color:#ecf2fb;font:15px/1.55 system-ui,sans-serif}main{max-width:1120px;margin:auto;padding:34px 22px 70px}h1{font-size:28px;margin:0}h2{font-size:20px;margin:35px 0 10px}h3{font-size:16px;margin:0}.meta,.small{color:#aebacc}.small{font-size:12px}.notice{border-left:3px solid #e5ba61;padding:11px 16px;background:#171a22;margin:22px 0}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(360px,1fr));gap:12px}article{background:#121b28;border:1px solid #29374b;border-radius:12px;padding:16px}svg{width:100%;height:auto}.table-wrap{overflow:auto}table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{text-align:left;padding:9px;border-bottom:1px solid #29374b;white-space:nowrap}th{color:#aebacc}.bad{color:#ff7d8c}.good{color:#6fd5ad}@media(max-width:500px){.grid{grid-template-columns:1fr}main{padding:18px 12px}}</style><main><p class="meta">RED EYE MONROE / RESOLVED BUCKET AUDIT · ${data.generated_at}</p><h1>市场温度档：模型概率是否可信？</h1><p>目标日期：${dates.join('、')}。从本地历史快照中取当地整点后 30 分钟内最早一次概率，按事件 ID 和市场 ID 对接公开结算结果。</p><div class="notice"><b>目前不能据此升级实盘。</b> 样本只覆盖 ${dates.length} 个目标日期。即使有 ${data.settled_rows} 条温度档记录，也不是 ${data.settled_rows} 次独立实验；同一事件的温度档互斥，同一地区天气相关，模型版本在采集期间也发生过变化。</div><h2>同一盘口、同一结果：模型 vs 市场</h2><p class="meta">Brier 越低越好。市场参考值为当时可见 Bid/Ask 中点，只在两侧报价有效时比较；它不是可成交利润。最热门档为模型概率最高的温度档。</p><div class="table-wrap"><table><thead><tr><th>当地时间</th><th>事件</th><th>已结算档</th><th>有效双边报价</th><th>热门档平均预测</th><th>热门档命中率</th><th>模型 Brier</th><th>市场 Brier</th><th>模型−市场</th></tr></thead><tbody>${table}</tbody></table></div><h2>可靠性：说 30%，实际中多少？</h2><p class="meta">虚线为完全校准；点在虚线上方代表低估、下方代表高估。当前分组样本少，尤其高概率区域，图只作诊断。</p><div class="grid">${hours.map(reliability).join('')}</div><h2>交易检验仍缺什么</h2><p>校准和赚钱是两件事。下一步必须用同一时点的可成交 Ask、足够买入深度、后续可成交 Bid、手续费、拒单记录做按事件分组的前向回放，并与“直接跟随市场”比较。当前这份结果不能估计真实净利润。</p><p class="meta">数据：本地 <code>snapshots.jsonl</code>；结算：<a href="https://docs.polymarket.com/api-reference/events/get-event-by-id">Polymarket Gamma 事件 API</a>。只有终态 Yes=1/No=0 的市场被纳入，未结算与无法确认结果的市场排除。</p></main></html>`;
const replayRows=hours.map(h=>{const v=replay.by_phase[h].horizons[60];return `<tr><td>${phase[h]}</td><td>${replay.by_phase[h].candidates}</td><td>${v.evaluated}</td><td class="${v.mean_net_per_share>0?'good':'bad'}">${fmt(v.mean_net_per_share*100,1)}¢/份</td><td>${fmt(v.positive_fraction*100,1)}%</td></tr>`;}).join('');
const replayBlock=`<h2>简化退出回放：60 分钟后卖得出去吗？</h2><p class="meta">每个事件、每个时点只选一个档：Ask 10–70¢、点差≤5¢、模型概率−Ask≥5pp，择优势最大者。使用 60 分钟后首次可见 Bid，减去记录的双边手续费和假设每侧 1¢ 执行冲击。该规则不是当前实盘策略，也未模拟 $1 深度或真实成交；不同时间段可能重复同一事件。</p><div class="table-wrap"><table><thead><tr><th>当地时间</th><th>候选事件</th><th>有后续报价</th><th>平均净退出差</th><th>正差比例</th></tr></thead><tbody>${replayRows}</tbody></table></div><p class="meta">作为敏感性检查，不扣任何费用和冲击时，各阶段 60 分钟平均 Bid−入场 Ask 也均为负。样本仅两个目标日期，不能据此估计长期收益率。</p>`;
const out=path.join(root,'docs/bucket-audit.html');
fs.writeFileSync(out,html.replace('<h2>交易检验仍缺什么</h2>',`${replayBlock}<h2>交易检验仍缺什么</h2>`));
console.log(JSON.stringify({output:out,dates,rows},null,2));
