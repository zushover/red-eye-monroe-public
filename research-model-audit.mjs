import fs from 'node:fs';
import path from 'node:path';

// Offline evidence report only. Never writes production settings or submits orders.
const root = process.cwd();
const read = (name) => JSON.parse(fs.readFileSync(path.join(root, name), 'utf8'));
const research = read('data/backtest/city-model-research/report.json');
const history = read('data/backtest/city-model-research/expanded-history-v2.json');
const pricePath = read('data/backtest/stage-price-path.json');
const stations = research.stations.filter(s => s.holdout?.n >= 30 && Number.isFinite(s.baseline?.mae_c));
const byId = new Map(history.stations.map(s => [s.icao, s]));
const escape = s => String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const n = (x, digits=2) => Number.isFinite(x) ? x.toFixed(digits) : '—';
const totalDays = stations.reduce((a,s) => a + s.holdout.n, 0);
const weighted = key => stations.reduce((a,s) => a + s.holdout.n*s[key].mae_c, 0)/totalDays;
const better = stations.filter(s => s.holdout.mae_c < s.baseline.mae_c).length;
const worse = stations.length-better;
const selected = ['KMIA','EDDM','LTAC','ZBAA','ZHHH'].map(id => byId.get(id)).filter(Boolean);

function lineChart(station) {
  const samples = station.samples.filter(s => s.date >= '2025-03-12').slice(0, 36);
  if (!samples.length) return '';
  const names = Object.keys(samples[0].forecasts_c);
  const values = samples.flatMap(s => [s.observed_c,...names.map(k => s.forecasts_c[k])].filter(Number.isFinite));
  const low = Math.floor(Math.min(...values)-1), high = Math.ceil(Math.max(...values)+1);
  const W=850,H=210,L=45,R=12,T=12,B=30;
  const xx = i => L+i*(W-L-R)/Math.max(1,samples.length-1);
  const yy = v => T+(high-v)*(H-T-B)/(high-low);
  const colors=['#ff667b','#67baff','#e5ba61','#8bdbaf'];
  const series=[['METAR daily max',s=>s.observed_c],...names.map(k=>[k,s=>s.forecasts_c[k]])];
  const lines=series.map(([name,get],j)=>{
    const points=samples.map((s,i)=>`${xx(i).toFixed(1)},${yy(get(s)).toFixed(1)}`).join(' ');
    return `<polyline fill="none" stroke="${colors[j]}" stroke-width="${j===0?2.7:1.5}" points="${points}"><title>${escape(name)}</title></polyline>`;
  }).join('');
  const grid=[0,.25,.5,.75,1].map(t=>{let v=low+(high-low)*t,y=yy(v);return `<line x1="${L}" y1="${y}" x2="${W-R}" y2="${y}" stroke="#354154"/><text x="${L-7}" y="${y+4}" text-anchor="end" fill="#aab7ca" font-size="11">${n(v,0)}°</text>`}).join('');
  const legend=series.map(([name],j)=>`<span><i style="background:${colors[j]}"></i>${escape(name)}</span>`).join('');
  return `<article><h3>${escape(station.name)} · ${escape(station.icao)}</h3><p class="meta">留出期连续 ${samples.length} 天 · 观测为 METAR 日最高温代理，不是官方结算值</p><div class="legend">${legend}</div><svg viewBox="0 0 ${W} ${H}" role="img" aria-label="${escape(station.name)} daily maximum forecasts versus METAR observations">${grid}${lines}<text x="${L}" y="${H-6}" fill="#aab7ca" font-size="11">${samples[0].date}</text><text x="${W-R}" y="${H-6}" text-anchor="end" fill="#aab7ca" font-size="11">${samples.at(-1).date}</text></svg></article>`;
}

const W=850,H=270,L=56,R=25,T=20,B=40,max=4;
const x=v=>L+v*(W-L-R)/max,y=v=>H-B-v*(H-T-B)/max;
const scatter=stations.map(s=>`<circle cx="${x(s.baseline.mae_c).toFixed(1)}" cy="${y(s.holdout.mae_c).toFixed(1)}" r="4" fill="${s.holdout.mae_c<s.baseline.mae_c?'#6fd5ad':'#ff7b8b'}"><title>${escape(s.name)}: baseline ${n(s.baseline.mae_c)}°C, candidate ${n(s.holdout.mae_c)}°C</title></circle>`).join('');
const scatterChart=`<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="City holdout MAE comparison"><line x1="${L}" y1="${H-B}" x2="${W-R}" y2="${T}" stroke="#77899f" stroke-dasharray="5 5"/><line x1="${L}" y1="${H-B}" x2="${W-R}" y2="${H-B}" stroke="#607088"/><line x1="${L}" y1="${H-B}" x2="${L}" y2="${T}" stroke="#607088"/>${scatter}<text x="${W/2}" y="${H-5}" text-anchor="middle" fill="#dfe8f5" font-size="12">基准 MAE (°C)</text><text x="12" y="${H/2}" transform="rotate(-90 12 ${H/2})" text-anchor="middle" fill="#dfe8f5" font-size="12">候选模型 MAE (°C)</text></svg>`;

const phases=['OVERNIGHT/ALL_TOP_BUCKETS','MORNING_06_09/ALL_TOP_BUCKETS','WARMING_09_12/ALL_TOP_BUCKETS','PRE_PEAK_CONVERGENCE/ALL_TOP_BUCKETS'];
const tradingRows=phases.flatMap(k=>['15m0s','30m0s','1h0m0s'].map(h=>({phase:k,h,...pricePath.metrics_by_phase_and_horizon[k]?.[h]}))).filter(v=>Number.isFinite(v.mean_net_exit_edge));
const phaseLabel={OVERNIGHT:'前夜',MORNING_06_09:'早晨',WARMING_09_12:'升温',PRE_PEAK_CONVERGENCE:'峰前'};
const tradeTable=tradingRows.map(r=>`<tr><td>${phaseLabel[r.phase.split('/')[0]]}</td><td>${r.h.replace('15m0s','15分').replace('30m0s','30分').replace('1h0m0s','1小时')}</td><td>${r.samples}</td><td>${r.independent_city_days}</td><td class="${r.mean_net_exit_edge>=0?'good':'bad'}">${n(r.mean_net_exit_edge*100,1)}pp</td><td>${n(r.net_positive_rate*100,1)}%</td></tr>`).join('');
const stationRows=stations.sort((a,b)=>a.holdout.mae_c-a.baseline.mae_c-(b.holdout.mae_c-b.baseline.mae_c)).map(s=>`<tr><td>${escape(s.name)}</td><td>${s.holdout.n}</td><td>${n(s.baseline.mae_c)}</td><td>${n(s.holdout.mae_c)}</td><td class="${s.holdout.mae_c<s.baseline.mae_c?'good':'bad'}">${n(s.holdout.mae_c-s.baseline.mae_c)}</td><td>${n(s.holdout.integer_c_log_loss)}</td></tr>`).join('');
const html=`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RED EYE MONROE · 概率模型证据板</title><style>body{margin:0;background:#0b111b;color:#ecf2fb;font:15px/1.55 system-ui,sans-serif}main{max-width:1120px;margin:auto;padding:34px 22px 70px}h1{font-size:28px;margin:0}h2{font-size:20px;margin:42px 0 12px}h3{font-size:16px;margin:0}p{margin:8px 0}.meta,small{color:#aab7ca}.notice{border-left:3px solid #e5ba61;padding:10px 16px;background:#171a22;margin:24px 0}.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:10px;margin-top:24px}.stat,article{background:#121b28;border:1px solid #29374b;border-radius:12px;padding:16px}.stat strong{display:block;font-size:23px}.stat span{color:#aab7ca}svg{width:100%;height:auto;display:block}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(420px,1fr));gap:12px}.legend{display:flex;flex-wrap:wrap;gap:14px;font-size:12px;margin:8px 0}.legend i{display:inline-block;width:12px;height:3px;vertical-align:middle;margin-right:5px}.table-wrap{overflow:auto}table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{text-align:left;padding:8px 11px;border-bottom:1px solid #29374b;white-space:nowrap}th{color:#aab7ca}.good{color:#6fd5ad}.bad{color:#ff7b8b}@media(max-width:520px){.grid{grid-template-columns:1fr}main{padding:20px 12px}}</style><main><p class="meta">RED EYE MONROE / OFFLINE MODEL AUDIT · ${escape(research.generated_at)}</p><h1>天气概率模型证据板</h1><p>研究版本：仅做历史诊断，不替换线上模型、不触发交易。</p><div class="notice"><b>结论：尚未达到实盘升级门槛。</b> 以下天气目标是 METAR 日最高温代理；市场温度档、官方结算、固定时点预测与完整可成交回放尚未闭环。历史留出集已反复被研究查看，也不能再当全新独立测试集。</div><div class="stats"><div class="stat"><strong>${stations.length}</strong><span>有留出指标的城市</span></div><div class="stat"><strong>${totalDays}</strong><span>城市-日留出样本（非独立交易）</span></div><div class="stat"><strong>${better} / ${worse}</strong><span>候选 MAE 改善 / 变差城市</span></div><div class="stat"><strong>${n(weighted('baseline'))}° → ${n(weighted('holdout'))}°</strong><span>样本加权日最高温 MAE</span></div></div><h2>一、城市模型是否真的改善天气预测</h2><p class="meta">每点一城。虚线下方表示候选 MAE 更低；这不是每个市场档的胜率证明。</p>${scatterChart}<div class="table-wrap"><table><thead><tr><th>城市</th><th>留出天</th><th>基准 MAE °C</th><th>候选 MAE °C</th><th>变化 °C</th><th>整数 °C 档 Log loss</th></tr></thead><tbody>${stationRows}</tbody></table></div><h2>二、预测与真实日最高温轨迹</h2><p class="meta">下图是三路原始天气源和 METAR 观测连续日轨迹；并非盘中逐小时轨迹，也不能用它验证当地 06:00 或 09:00 阶段模型。</p><div class="grid">${selected.map(lineChart).join('')}</div><h2>三、能否从交易中赚钱</h2><p class="meta">存档快照的“进场 Ask → 后续可显示 Bid”粗略回放。下表使用所有热门温度档，非当前策略成交；净退出差值已扣存档进场费用和每侧 1¢ 滑点，但未完整模拟卖出费、深度、排队、拒单或真实成交。</p><div class="table-wrap"><table><thead><tr><th>当地阶段</th><th>持有时间</th><th>重叠快照</th><th>独立城市-日</th><th>平均净退出差</th><th>正收益占比</th></tr></thead><tbody>${tradeTable}</tbody></table></div><h2>四、下一次上线前必须补齐</h2><ol><li>归档固定当地时点预报版本、逐小时 METAR、官方结算最高温，并逐市场原始 °F/°C 档映射。</li><li>按城市-日期整组时间前推，锁定训练与模型选择；新日期做真正未触碰的前向检验，报告分档 Log loss、Brier、可靠性曲线、覆盖率与置信区间。</li><li>记录当时完整买卖盘口、深度、费用、订单回执；回放实际候选、排队与失败重试，比较真实可执行净收益、回撤及流动性分层。</li><li>达到预先写定的样本量和收益/校准门槛后才提议影子盘，再经用户确认才可能切换实盘。</li></ol><p class="meta">来源：本地 city-model-research/report.json、expanded-history-v2.json、stage-price-path.json。报告由 research-model-audit.mjs 生成。</p></main></html>`;
const out=path.join(root,'docs','model-audit.html');
fs.mkdirSync(path.dirname(out),{recursive:true});
const htmlFinal=html.replace('<h2>三、能否从交易中赚钱</h2>', '<p class="meta">数据相关性提醒：迈阿密历史样本中，Open-Meteo best match 与 GFS 数值完全相同，不是两份独立证据。</p><h2>三、能否从交易中赚钱</h2>');
fs.writeFileSync(out,htmlFinal);
console.log(JSON.stringify({output:out,stations:stations.length,holdout_days:totalDays,improved:better,worsened:worse,baseline_mae:weighted('baseline'),candidate_mae:weighted('holdout'),trade_rows:tradingRows.length},null,2));
