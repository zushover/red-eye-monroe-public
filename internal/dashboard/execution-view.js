/* One state resolver for overview, research, matrix and globe. */
(function(root){
 const up=x=>String(x||'').toUpperCase(),num=x=>x!==null&&x!==undefined&&Number.isFinite(Number(x));
 function resolveState(s,r,account,ledger,snapshot,now=Date.now()){
  const attempts=ledger?.attempts||[],asset=String(s.yes_token_id),assets=new Set((r.signals||[]).map(x=>String(x.yes_token_id)));
  const group=r.rule.station_icao+'|'+r.rule.local_date;
  const terminal=new Set(['REJECTED','CANCELED','CANCELLED','CLOSED','EXPIRED','FAILED','BLOCKED']);
  const active=a=>!terminal.has(up(a.status))&&!a.position_closed;
  const same=a=>a.group_key===group||assets.has(String(a.asset_id));
  const closed=a=>a.position_closed||attempts.some(x=>x.side==='SELL'&&x.asset_id===a.asset_id&&up(x.status)==='CLOSED'&&Date.parse(x.created_at)>Date.parse(a.created_at));
  const position=(account?.positions||[]).find(p=>String(p.asset_id)===asset&&p.current_size>0&&up(p.status)!=='REDEEMABLE');
  const sell=[...attempts].reverse().find(a=>a.side==='SELL'&&String(a.asset_id)===asset&&active(a));
  if(sell)return{label:'请求中',detail:'卖出请求待完成或成交结果待核对；仓位仍占用',position};
  if(position){const b=[...attempts].reverse().find(a=>a.side==='BUY'&&String(a.asset_id)===asset&&a.order_id&&active(a)&&!closed(a));return{label:'已持仓',detail:b?`系统管理 · ${b.exit_state||'待检查'} · ${b.exit_reason||'持有'}`:'手动持仓 · 未托管',position};}
  if((account?.positions||[]).some(p=>assets.has(String(p.asset_id))&&p.current_size>0&&up(p.status)!=='REDEEMABLE'))return{label:'已占用',detail:'同城同日另一温度已有仓位'};
  const order=(account?.orders||[]).find(o=>assets.has(String(o.asset_id))&&!terminal.has(up(o.status))&& !['MATCHED','FILLED'].includes(up(o.status)));
  if(order)return{label:String(order.asset_id)===asset?'请求中':'已占用',detail:'交易所订单待完成'};
  const pending=[...attempts].reverse().find(a=>same(a)&&active(a)&&!closed(a));
  if(pending)return{label:String(pending.asset_id)===asset?'请求中':'已占用',detail:'已提交订单尚未完成核对'};
  const exit=attempts.filter(a=>a.side==='SELL'&&up(a.status)==='CLOSED'&&(a.group_key===group||assets.has(String(a.asset_id)))).at(-1);
  if(exit){
   const b=[...attempts].reverse().find(a=>a.side==='BUY'&&a.asset_id===exit.asset_id&&up(a.status)!=='REJECTED'&&Date.parse(a.created_at)<Date.parse(exit.created_at));
   const at=Date.parse(b?.closed_at||exit.updated_at||exit.created_at),minutes=Math.max(0,Math.ceil((at+1800000-now)/60000));
   if(minutes)return{label:'已拦截',detail:`平仓后冷却，剩余${minutes}分钟`};
   if(/STOP|MODEL_DROP/.test(exit.reason||'')&&(asset!==String(b?.asset_id)||!num(b?.model_probability)||!num(s.model_probability)||s.model_probability<Number(b.model_probability)+.03||Date.parse(snapshot.generated_at)<=at))return{label:'已拦截',detail:'止损后等待新信号；原温度交易概率需较前次入场提高3pp'};
  }
  const control=account?.live_control;
  const last=account?.execution_by_market?.[s.market_id]||(account?.execution_last?.market_id===s.market_id?account.execution_last:null);
  if(last&&!last.accepted&&now-Date.parse(last.at)<180000){const rejected=last.stage==='REJECTED'||/couldn't be fully filled|fully filled or killed|Exchange explicitly rejected|invalid amount/i.test(last.message||'');return{label:rejected?'已拒绝':'已拦截',detail:last.message||'本地检查未通过'};}
  if(!account||!num(Date.parse(account.checked_at))||now-Date.parse(account.checked_at)>120000)return{label:'观测中',detail:'账户数据过期，暂停新开仓'};
  if(!control?.enabled||(control.persistent_authorization!==true&&(!Number.isFinite(Date.parse(control.expires_at))||Date.parse(control.expires_at)<=now)))return{label:'观测中',detail:'实盘授权未启用或已过期'};
  if(!Number.isFinite(Date.parse(snapshot.generated_at))||now-Date.parse(snapshot.generated_at)>120000)return{label:'观测中',detail:'行情快照过期'};
  // A past successful request is never evidence of a currently pending order.
  return{label:s.action==='BUY_LIVE'?'可开仓':'观测中',detail:s.reason||'当前未达到开仓条件'};
 }
 function portfolio(account,ledger,snapshot){
  const orders=account?.accounting?.orders||{},attempts=ledger?.attempts||[],signals=(snapshot?.reports||[]).flatMap(r=>r.signals||[]);
  const positions=(account?.positions||[]).filter(p=>p.current_size>0&&up(p.status)!=='REDEEMABLE').map(p=>{
   const b=[...attempts].reverse().find(a=>a.side==='BUY'&&String(a.asset_id)===String(p.asset_id)&&a.order_id&&a.status!=='REJECTED'&&!a.position_closed),fill=orders[b?.intent_id],s=signals.find(s=>String(s.yes_token_id)===String(p.asset_id));
   const principal=Number(p.current_size)*Number(p.avg_price),cost=fill&&num(fill.net)&&fill.shares>0?Number(p.current_size)*fill.net/fill.shares:principal;
   const fresh=s&&num(s.best_bid)&&Date.now()-Date.parse(s.generated_at)<120000;
   const bidValue=fresh?Number(p.current_size)*s.best_bid:null;
   const entryFee=fill&&num(fill.estimated_fee)?Number(p.current_size)*fill.estimated_fee/fill.shares:null;
   return{...p,principal,cost,bid_value:bidValue,mark_pnl:Number(p.current_value)-cost,bid_pnl:bidValue===null?null:bidValue-cost,entry_fee:entryFee,managed:!!b,exit_state:b?.exit_state||'未托管',exit_reason:b?.exit_reason||''};
  });
  const openBuys=(account?.orders||[]).filter(o=>up(o.side)==='BUY');
  const unresolved=attempts.filter(a=>a.side==='BUY'&&['PREPARING','SUBMITTING','SUBMISSION_UNCERTAIN','POSTED','LIVE','OPEN','DELAYED'].includes(up(a.status))&&!positions.some(p=>String(p.asset_id)===String(a.asset_id))&&!openBuys.some(o=>String(o.asset_id)===String(a.asset_id)));
  const reserved=openBuys.reduce((n,o)=>n+Math.max(0,Number(o.size)-Number(o.size_matched||0))*Number(o.price)*1.1,0)+unresolved.reduce((n,a)=>n+Number(a.max_spend_dollars||1.1),0);
  const cost=positions.reduce((n,p)=>n+p.cost,0),value=positions.reduce((n,p)=>n+Number(p.current_value),0),exposure=Number(account?.trading_position_exposure_dollars||0)+reserved;
  const balance=num(account?.balance?.balance)?Number(account.balance.balance)/1e6:null;
  const settledValue=(account?.positions||[]).filter(p=>up(p.status)==='REDEEMABLE').reduce((n,p)=>n+Number(p.current_value||0),0);
  return{positions,cost,value,pnl:value-cost,reserved,exposure,cap:Number(account?.live_control?.maximum_total_exposure_dollars||8),balance,equity:balance===null?null:balance+value+settledValue,settledValue,realized:Object.values(orders).filter(o=>num(o.estimated_realized_pnl)).reduce((n,o)=>n+o.estimated_realized_pnl,0)};
 }
 root.ExecutionView={resolveState,portfolio};
 if(typeof document==='undefined')return;
 const fmt=x=>num(x)?'$'+Number(x).toFixed(2):'—',signed=x=>num(x)?(Number(x)>=0?'+':'−')+'$'+Math.abs(Number(x)).toFixed(2):'—';
 const labels={MATCHED:'已成交',CLOSED:'已平仓',REJECTED:'已拒绝',CONFIRMING:'核对中',SUBMISSION_UNCERTAIN:'请求中',POSTED:'请求中',PREPARING:'准备中',PARTIAL:'部分成交',HOLD:'持有',STOP_25:'常规止损25%',EMERGENCY_STOP_40:'紧急止损40%',DYNAMIC_TRAIL:'动态止盈',BID_ABOVE_MODEL:'价格高于模型价值',MODEL_DROP_4PP:'模型下降4pp',MODEL_DROP_PERSISTENT:'模型持续恶化',LATE_CONVERGENCE:'晚段收敛',DAY_ENDED:'观测日结束'};
 const state=s=>{const r=(latest?.reports||[]).find(r=>(r.signals||[]).some(x=>x.market_id===s.market_id));return r?resolveState(s,r,liveStatus,liveLedger,latest):{label:'观察',detail:''};};
 marketExecutionState=state;
 const originalTable=marketTable;
 marketTable=function(r){let i=0;return originalTable(r).replace(/<tr class=/g,()=>`<tr data-market-id="${esc(r.signals[i++].market_id)}" class=`);};
 paintExecutionGates=function(){
  if(latest?.mode!=='live')return;
  const signals=(latest.reports||[]).flatMap(r=>r.signals||[]);
  for(const row of document.querySelectorAll('tr')){
   const jump=row.querySelector('.city-jump'),id=row.dataset.marketId;
   const s=id?signals.find(s=>String(s.market_id)===id):jump?signals.find(s=>(jump.getAttribute('onclick')||'').includes("'"+s.market_id+"'")):null;if(!s)continue;
   const x=state(s),headers=[...(row.closest('table')?.querySelectorAll('thead th')||[])],index=headers.findIndex(h=>h.textContent.trim()==='状态');
   if(index>=0&&row.cells[index]){row.cells[index].textContent=x.label;row.cells[index].title=x.detail;}
   else if(jump){let badge=row.querySelector('.execution-state');if(!badge){badge=document.createElement('span');badge.className='badge execution-state';jump.parentElement.append(badge);}badge.textContent=x.label;badge.title=x.detail;}
  }
 };
 const summary=p=>`<div class="live-readiness">${[['当前持仓成本',fmt(p.cost)],['动态价值',fmt(p.value)],['持仓浮盈（估值）',signed(p.pnl)],['额度占用 / 上限',fmt(p.exposure)+' / '+fmt(p.cap)],['挂单及未知请求预留',fmt(p.reserved)],['可用余额',fmt(p.balance)],['账户估值',fmt(p.equity)],['系统已实现盈亏',signed(p.realized)]].map(([k,v])=>`<div class="readiness-item"><small>${k}</small><b>${v}</b></div>`).join('')}</div>`;
 const rows=p=>p.positions.map(x=>`<tr><td>${esc(x.title)}<br><small>${x.managed?'系统管理':'手动 · 未托管'} · ${esc(x.exit_state)} ${esc(x.exit_reason)}</small></td><td>${Number(x.current_size).toFixed(4)}</td><td>${pct(x.avg_price)}</td><td>${fmt(x.cost)}</td><td>${fmt(x.current_value)}</td><td class="${x.mark_pnl>=0?'green':'red'}">${signed(x.mark_pnl)}</td><td>${fmt(x.bid_value)}<br><small>未扣卖出费 / 深度未保证</small></td></tr>`).join('');
 const oldLive=renderLiveState;
 renderLiveState=function(...args){oldLive(...args);const p=portfolio(liveStatus,liveLedger,latest),a=liveStatus?.accounting?.orders||{};
  document.querySelector('#liveReadiness').innerHTML=summary(p)+`<div class="muted">账户更新 ${esc(liveStatus?.checked_at||'—')} · 成交核对 ${esc(liveStatus?.accounting?.checked_at||'—')}</div>`;
  document.querySelector('#livePositions').innerHTML=`<h2>当前真实持仓 · ${p.positions.length}</h2><div class="scroll"><table><thead><tr><th>市场 / 管理</th><th>份额</th><th>均价</th><th>成本</th><th>估值</th><th>浮盈</th><th>Bid估值</th></tr></thead><tbody>${rows(p)}</tbody></table></div>`;
  document.querySelector('#liveAudit').innerHTML=`<h2>实盘执行审计</h2><div class="muted">成交金额来自交易所；费用按成交记录费率计算。缺失明细显示待核对。</div><div class="scroll"><table><thead><tr><th>时间 / 方向</th><th>市场</th><th>状态</th><th>成交份额 / 均价</th><th>买入成本 / 卖出收入</th><th>费用</th><th>卖出盈亏</th><th>原因</th></tr></thead><tbody>${[...(liveLedger?.attempts||[])].reverse().slice(0,60).map(x=>{const f=a[x.intent_id],r=(latest?.reports||[]).find(r=>(r.signals||[]).some(s=>s.market_id===x.market_id)),s=r?.signals.find(s=>s.market_id===x.market_id);return `<tr><td>${esc(new Date(x.created_at).toLocaleString())}<br>${esc(x.side)}</td><td>${esc(r?cityName(r)+' · '+r.rule.local_date+' · '+bucketLabel(s.bucket):x.market_id)}</td><td>${esc(x.position_closed?'已平仓':x.status)}</td><td>${f?f.shares.toFixed(4)+' / '+pct(f.average_price):'—'}</td><td>${f?fmt(f.net):x.status==='REJECTED'?'未成交': '待核对'}${x.side==='BUY'?'<br><small>预算上限 '+fmt(x.max_spend_dollars)+'</small>':''}</td><td>${f?fmt(f.estimated_fee):'—'}</td><td class="${f?.estimated_realized_pnl>=0?'green':'red'}">${x.side==='SELL'?signed(f?.estimated_realized_pnl):'—'}</td><td>${esc(x.reason||x.exit_reason||'—')}</td></tr>`}).join('')}</tbody></table></div>`;
  for(const row of document.querySelectorAll('#liveAudit tbody tr'))for(const index of [2,7]){const cell=row.cells[index],label=labels[up(cell?.textContent)];if(label){cell.title=cell.textContent;cell.textContent=label;}}
  document.querySelector('#liveRisk').innerHTML='<h2>当前执行检查</h2>'+`<p>每单本金 $1 · 含费预算 $1.10 · 最多同时持仓 ${Number(liveStatus?.live_control?.maximum_simultaneous_positions||6)} 单 · 持仓成本上限 ${fmt(p.cap)} · 持续授权</p>`+Object.values(liveStatus?.execution_by_market||{}).filter(x=>Date.now()-Date.parse(x.at)<300000&&!x.accepted).slice(-8).map(x=>`<div><b>${esc(x.market_id)} · ${x.stage==='REJECTED'?'已拒绝':'已拦截'}</b><small class="muted"> ${esc(x.message)}</small></div>`).join('');
  refreshSummary(p);paintExecutionGates();
 };
 function refreshSummary(p){
  const stale=!latest||Date.now()-Date.parse(latest.generated_at)>120000;
  if(document.querySelector('#health'))document.querySelector('#health').textContent=(stale?'行情超时 · 暂停新开仓':'行情更新中')+' · '+(latest?.mode==='live'?'真实执行':'模拟执行');
  const ops=document.querySelector('#opsStrip');if(ops&&stale){const first=ops.querySelector('b');if(first)first.textContent='行情超时 · 退出独立检查';}
  const quality=document.querySelector('#quality'),grid=quality?.querySelector('.position-grid');
  if(grid){grid.innerHTML=p.positions.map(x=>{const id=positionIdentity(x);return `<article class="position-card"><div><h3>${esc(id.name)} · ${esc(id.bucket)}</h3><div class="position-date muted">${esc(id.date)} · ${x.managed?'系统管理':'手动 · 未托管'}</div></div><div class="position-stats"><div><small>份额</small><b>${Number(x.current_size).toFixed(4)}</b></div><div><small>成本</small><b>${fmt(x.cost)}</b></div><div><small>估值</small><b>${fmt(x.current_value)}</b></div></div><div class="${x.mark_pnl>=0?'green':'red'}">浮盈 ${signed(x.mark_pnl)}</div></article>`}).join('')||'<div class="empty">当前没有未结算持仓</div>';const badges=quality.querySelectorAll('.badge');if(badges.length)badges[badges.length-1].textContent=p.positions.length;}
  let el=document.querySelector('#accountSummary');if(!el){el=document.createElement('div');el.id='accountSummary';el.className='card';document.querySelector('#kpis')?.insertAdjacentElement('afterend',el);}if(el)el.innerHTML='<h3>实盘资金与占用</h3>'+summary(p);
  let research=document.querySelector('#researchAccountSummary');if(!research&&document.querySelector('#method')){research=document.createElement('div');research.id='researchAccountSummary';research.className='card';document.querySelector('#method').prepend(research);}if(research)research.innerHTML='<h3>当前实盘 · 研究参照</h3>'+summary(p);
 }
 const oldMain=renderMain;renderMain=function(...args){oldMain(...args);refreshSummary(portfolio(liveStatus,liveLedger,latest));paintExecutionGates();};
 const oldGlobe=renderGlobeSelection;renderGlobeSelection=function(r){oldGlobe(r);if(!r)return;const held=(r.signals||[]).find(s=>state(s).label==='已持仓'),chosen=held||(r.signals||[]).find(s=>s.market_id===selectedMarketID)||(r.signals||[])[0];if(!chosen)return;const x=state(chosen),el=document.querySelector('#globeSelection .decision-box b');if(el){el.textContent=x.label;el.title=x.detail;}const p=portfolio(liveStatus,liveLedger,latest).positions.find(p=>String(p.asset_id)===String(chosen.yes_token_id));if(p)document.querySelector('#globeSelection').insertAdjacentHTML('beforeend',`<div class="muted">成本 ${fmt(p.cost)} · 估值 ${fmt(p.current_value)} · 浮盈 ${signed(p.mark_pnl)}<br>${p.managed?'系统管理':'手动 · 未托管'}</div>`);};
 setInterval(()=>{if(!latest)return;paintExecutionGates();const r=(latest.reports||[]).find(r=>r.rule.station_icao===selectedStation&&r.rule.local_date===selectedMarketDate);if(r)renderGlobeSelection(r);},5000);
})(globalThis);
