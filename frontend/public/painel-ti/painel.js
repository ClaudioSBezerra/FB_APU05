
(function(){
'use strict';
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const MES = ['jan','fev','mar','abr','mai','jun','jul','ago','set','out','nov','dez'];
const ymLabel = ym => ym ? MES[+ym.slice(5,7)-1] + '/' + ym.slice(2,4) : '—';
const nf0 = new Intl.NumberFormat('pt-BR',{maximumFractionDigits:0});
const nf2 = new Intl.NumberFormat('pt-BR',{minimumFractionDigits:2,maximumFractionDigits:2});
const brl = v => 'R$ ' + nf2.format(v||0);
const brl0 = v => 'R$ ' + nf0.format(Math.round(v||0));
function short(v){
  const a = Math.abs(v||0), s = v<0?'−':'';
  if (a>=1e6) return s+'R$ '+(a/1e6).toLocaleString('pt-BR',{maximumFractionDigits:a>=1e7?1:2})+' mi';
  if (a>=1e3) return s+'R$ '+(a/1e3).toLocaleString('pt-BR',{maximumFractionDigits:a>=1e5?0:1})+' mil';
  return s+'R$ '+nf0.format(a);
}
const pct = (v,d=1) => (v==null||!isFinite(v)) ? '—' : (v*100).toLocaleString('pt-BR',{maximumFractionDigits:d,minimumFractionDigits:d})+'%';
const esc = s => String(s==null?'':s).replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const sum = a => a.reduce((s,x)=>s+(x||0),0);
const dBR = iso => iso ? iso.split('-').reverse().join('/') : '';
const store = {
  get(k){ try{ return JSON.parse(localStorage.getItem(k)); }catch(e){ return null; } },
  set(k,v){ try{ localStorage.setItem(k, JSON.stringify(v)); return true; }catch(e){ return false; } }
};

/* ---------- De-para oficial de descrições (prevalece sobre a planilha) ---------- */
const DIV_DESC = {'1002':'IMBIRIBEIRA','4016':'RETAILX LABS','N/D':'IMOB','1001':'GARANHUNS','1007':'NATAL','1008':'CARUARU','1004':'TAMARINEIRA','1006':'JOÃO PESSOA','1005':'ARACAJU','91':'CD Cabo','9207':'IMPORTADORA [ ROYAL ]','92':'CD Cabo','1003':'SALVADOR','1009':'BARRIS','9090':'IMPORTADORA [ FCM ]','9290':'IMPORTADORA [ AMERICAN ]','9190':'IMPORTADORA [ ROYAL ]','9390':'IMPORTADORA [ GREEN ]'};
const CC_DESC = {'6770':'VENDAS B2B - CORP','6780':'CENTRAL ATEND - CORP','6151':'SERVICOS DESENVOLVIMENTO TECNOLOGIA','6000':'ADMINISTRATIVO','6152':'SERVICOS TI SFC','6153':'SERVICOS SUPORTE','6154':'SERVICOS TI INFRA SEG','6155':'SERVICOS TI INT DADOS','6030':'DP','6720':'TI - Infraestrutura','6570':'MANUTENCAO CORP','6710':'TI - Sistemas Corporativos','IMOB':'Imobilizado (CAPEX)','6620':'FCX Lab – Corp','6730':'TI INT DADOS - CORP','6810':'CYBER - CORP','6550':'PREV PERDAS - CORP','6470':'Marketing - Corp','6150':'TI - Filial','6440':'RH - CORP','6070':'Gerência Loja','6660':'COMERC FIMP - CORP','6460':'COMERCIAL - CORP','6040':'RH'};

/* ---------- Grupos ---------- */
const GROUPS = [
  {k:'FC', l:'Ferreira Costa', c:'var(--real)'},
  {k:'RX', l:'RetailX', c:'var(--g-rx)'},
  {k:'IMP', l:'Importadoras', c:'var(--g-imp)'},
  {k:'OUT', l:'Sem divisão', c:'var(--ink-3)'}
];
const GLABEL = Object.fromEntries(GROUPS.map(g=>[g.k,g.l]));
function grpOf(div){
  const d = String(div||'').trim();
  const desc = (DIV_DESC[d] || IDX.divDesc0[d] || '').toUpperCase();
  if (desc.startsWith('IMPORTADORA')) return 'IMP';
  if (d === '4016') return 'RX';
  const n = +d;
  if (isFinite(n) && d !== '' && n >= 91 && n <= 1009) return 'FC';
  if (d === 'N/D') return 'FC';   // IMOB (imobilizado) entra no grupo FC
  return 'OUT';
}

let M = null;
const S = { view:'geral', year:null, grps:[], back:null, cc:'', div:'', tipo:'', conta:'', ex:true, q:'', status:'', lancSort:{k:'comp',d:-1}, lancLimit:200, pag:'', lv:'lanc', oq:'', oLimit:150, pq:'', pst:'', pLimit:200, fo:{q:'', seg:'all', sort:{k:'U',d:-1}, limit:100}, lf:{mes:'', fk:'', fname:''}, cq:'', cst:'', cLimit:500, gest:'', mes:'' };
let IDX = { divDesc0:{} };

/* ---------- carga ---------- */
/* Adaptador: modelo do motor (Despesas TI + Orçamento TI + KSB1/KSB2/FBL1N) -> modelo deste painel.
   RAW é o que é gravado no HTML publicado. Linhas da planilha substituídas pela KSB1 (kind X) ficam fora das contas,
   mas continuam na aba Lançamentos. Os anos seguintes da projeção vêm de RAW.plur (FC FY27 Forecast). */
/* Perfil do usuário (rota GET /eu do servidor). Sem servidor = ADM. O filtro aqui é de tela;
   quem garante o acesso é o servidor, que só devolve os CCs do gestor em /modelo. */
const ACL = { perfil:'adm', ok:null, eu:null };
const GRP_EMP = { 'retailx':'RX', 'ferreira costa':'FC', 'importadoras':'IMP' };
function setAcl(eu){
  ACL.eu = eu || null;
  if (!eu || /^(adm|admin|editor)$/i.test(eu.perfil||'')){ ACL.perfil='adm'; ACL.ok=null; }
  else { const ccs = new Set((eu.ccs||[]).map(String)), gs = new Set((eu.empresas||[]).map(e=>GRP_EMP[String(e).toLowerCase()]||e)), dv = new Set((eu.divisoes||[]).map(String));
    ACL.perfil='gestor'; ACL.ok = r => ccs.has(String(r.cc)) || gs.has(r.grp) || dv.has(String(r.div)); }
  const g = ACL.perfil==='gestor';
  ['#btnMain','#btnSrv','#closeBanner','#prevBanner','#btnMail'].forEach(q=>{ const e=$(q); if (e && g) e.style.display='none'; });
  const ct = $('#tabs button[data-v="conf"]'); if (ct) ct.style.display = g ? 'none' : '';
  if (g && S.view==='conf') S.view='geral';
}
const maskDoc = d => (ACL.perfil==='gestor' && String(d||'').replace(/\D/g,'').length===11) ? '***.***.***-**' : d;
let RAW = null;
/* Ano fechado guardado dentro do painel (relatório histórico). */
const trimYear = m => ({ meta: { year:m.meta.year, months:m.meta.months, lastClosed:m.meta.lastClosed, sapMonths:m.meta.sapMonths||[], monthSrc:m.meta.monthSrc||{},
  sources:m.meta.sources||{}, parsedAt:m.meta.parsedAt, k1modo:m.meta.k1modo, orcFonte:m.meta.orcFonte, ignorado:m.meta.ignorado||{} },
  budget:m.budget||[], cf:m.cf||[], lanc:m.lanc||[], conf:m.conf||[], pag:m.pag||null });
function withHistory(m, prev){
  m.hist = Object.assign({}, (prev && prev.hist) || {}, m.hist || {});
  if (prev && prev.meta && prev.meta.year && m.meta.year && prev.meta.year < m.meta.year && !m.hist[prev.meta.year]) m.hist[prev.meta.year] = trimYear(prev);
  Object.keys(m.hist).forEach(y=>{ if (!m.meta.year || y >= m.meta.year) delete m.hist[y]; });
  if (!m.plur && prev && prev.plur) m.plur = prev.plur;
  return m;
}
function loadModel(m){
  if (!m || !m.meta){ if (API.url){ showOk('Carregando a versão publicada do servidor…'); return; } showErr('Painel sem dados. Clique em Importar planilha e escolha a pasta 1 - DESPESAS - ADM.'); return; }
  withHistory(m, RAW); RAW = m;
  const parts = Object.keys(m.hist||{}).sort().map(y=>m.hist[y]).concat([m]);
  let months = [...new Set(parts.flatMap(p=>p.meta.months||[]))].sort();
  const P = m.plur, last = months[months.length-1];
  const fut = (P && P.months) ? P.months.filter(ym=>ym>last) : [];
  const nBase = months.length; months = months.concat(fut);
  const pos = {}; months.forEach((ym,i)=>{ pos[ym]=i; });
  const widen = (r, ms) => { const a = months.map(()=>0); ms.forEach((ym,i)=>{ if (pos[ym]!=null) a[pos[ym]] += (r.m&&r.m[i])||0; }); return Object.assign({}, r, { m:a }); };
  let budget=[], cf=[], lanc=[], conf=[], orphans=[];
  parts.forEach(p=>{ const pre = p===m ? '' : p.meta.year+'-';
    budget = budget.concat((p.budget||[]).map(r=>widen(r, p.meta.months)));
    cf = cf.concat((p.cf||[]).map(r=>widen(r, p.meta.months)));
    lanc = lanc.concat((p.lanc||[]).map(l=>Object.assign({}, l, { id: pre + l.id })));
    conf = conf.concat((p.conf||[]).map(c=>Object.assign({}, c)));
    if (p.pag) orphans = orphans.concat((p.pag.orphans||[]).filter(x=>(x.dtLanc||'').startsWith(p.meta.year||'')));
  });
  if (fut.length){ const ix = fut.map(ym=>P.months.indexOf(ym));
    const add = r => Object.assign({}, r, { m: months.map((ym,i)=> i < nBase ? 0 : ((r.m||[])[ix[i-nBase]]||0)) });
    (P.budget||[]).forEach(r=>budget.push(Object.assign(add(r), { origem:'Forecast' })));
    (P.cf||[]).forEach(r=>cf.push(add(r))); }
  const plan = lanc.filter(l=>l.src!=='SAP');
  const pm = parts.map(p=>p.pag).filter(Boolean).pop();
  window.FCX_PAG = pm ? { meta: pm.meta, orphans, byId:{} } : null;
  if (window.FCX_PAG) plan.forEach(l=>{ if (l.pag) window.FCX_PAG.byId[l.id] = l.pag; });
  const sapMonths = [...new Set(parts.flatMap(p=>p.meta.sapMonths||[]))].sort();
  const monthSrc = Object.assign({}, ...parts.map(p=>p.meta.monthSrc||{}));
  const lc = m.meta.lastClosed, ed = lc ? new Date(+lc.slice(0,4), +lc.slice(5,7), 0) : null;
  const fs = m.meta.sources || {};
  loadFY({ meta: Object.assign({}, m.meta, { months, sapMonths, monthSrc, anos: parts.map(p=>p.meta.year), fileName: fs.main ? fs.main.name : (m.meta.fileName||'Despesas TI'),
      refDate: ed ? ed.getFullYear()+'-'+String(ed.getMonth()+1).padStart(2,'0')+'-'+String(ed.getDate()).padStart(2,'0') : null }),
    budget, cf, lanc: lanc.filter(l=>l.kind!=='X'), lancPlan: plan, conf, pag: m.pag||null });
}
function loadFY(model){
  M = model;
  // CC 6600 (Lei do Bem) fica fora do painel, inclusive quando a planilha é recarregada
  ['budget','cf','lanc','lancPlan'].forEach(k=>{ M[k] = (M[k]||[]).filter(r=>r.cc!=='6600'); });
  const all = [...M.budget, ...M.cf, ...M.lanc, ...M.lancPlan.filter(l=>l.kind==='X')];
  IDX.divDesc0 = {}; all.forEach(r=>{ if(r.div && r.divDesc && !IDX.divDesc0[r.div]) IDX.divDesc0[r.div]=r.divDesc; });
  const bad = v => !v || v==='Verificar' || /verificar/i.test(v);
  all.forEach(r=>{ if (bad(r.ccDesc) && CC_DESC[r.cc]) r.ccDesc = CC_DESC[r.cc]; if (bad(r.divDesc) && DIV_DESC[r.div]) r.divDesc = DIV_DESC[r.div]; if (r.tipo==='Imobilizado (CAPEX)') r.tipo='Imobilizado'; r.grp = r.cc==='IMOB' ? 'FC' : grpOf(r.div); });
  (M.conf||[]).forEach(r=>{ r.grp = r.cc==='IMOB' ? 'FC' : grpOf(r.div); });
  (M.lancPlan||[]).forEach(r=>{ if (!r.grp) r.grp = r.cc==='IMOB' ? 'FC' : grpOf(r.div); });
  if (ACL.ok) ['budget','cf','lanc','lancPlan','conf'].forEach(k=>{ M[k] = (M[k]||[]).filter(ACL.ok); });
  IDX.years = [...new Set(M.meta.months.map(m=>m.slice(0,4)))];
  IDX.closedYear = M.meta.lastClosed.slice(0,4);
  { const d = new Date(); IDX.curYM = d.getFullYear()+'-'+String(d.getMonth()+1).padStart(2,'0'); if (IDX.curYM <= M.meta.lastClosed) IDX.curYM = monthAdd(M.meta.lastClosed, 1); }
  IDX.curYear = IDX.years.includes(IDX.closedYear) ? IDX.closedYear : IDX.years[0];
  if (!S.year || !IDX.years.includes(S.year) || S.view!=='fcst') S.year = IDX.curYear;
  const ccDesc = {}, divDesc = {};
  all.forEach(r=>{ if(r.cc && r.ccDesc && !ccDesc[r.cc]) ccDesc[r.cc]=r.ccDesc; if(r.div && !divDesc[r.div]) divDesc[r.div]=r.divDesc||DIV_DESC[r.div]||''; });
  IDX.ccDesc = ccDesc; IDX.divDesc = divDesc;
  IDX.tipos = [...new Set(all.map(r=>r.tipo).filter(Boolean))].sort();
  IDX.contas = [...new Set(all.map(r=>r.conta).filter(Boolean))].sort();
  IDX.contaDesc = {}; all.forEach(r=>{ if (r.conta && r.contaDesc && r.contaDesc!=='Verificar' && !IDX.contaDesc[r.conta]) IDX.contaDesc[r.conta]=r.contaDesc; });
  IDX.ccs = [...new Set(all.map(r=>r.cc).filter(Boolean))].sort();
  IDX.divs = [...new Set(all.map(r=>r.div).filter(Boolean))].sort((a,b)=>(+a||1e9)-(+b||1e9));
  IDX.statuses = [...new Set(M.lancPlan.map(r=>r.status).filter(Boolean))].sort();
  IDX.grps = GROUPS.filter(g=>all.some(r=>r.grp===g.k));
  buildFilters();
  render();
}

/* ---------- filtros ---------- */
function availFor(field){
  const Y = S.year, ix = M.meta.months.map((m,i)=>m.startsWith(Y)?i:-1).filter(i=>i>=0);
  const ok = r => (!S.ex || r.cc!=='6600') && gestOk(r) && (!S.grps.length || S.grps.includes(r.grp))
    && (field==='div' || !S.div || r.div===S.div) && (field==='cc' || !S.cc || r.cc===S.cc) && (field==='tipo' || !S.tipo || r.tipo===S.tipo) && (field==='conta' || !S.conta || r.conta===S.conta);
  const set = new Set();
  M.budget.forEach(r=>{ if (ok(r) && ix.some(i=>Math.abs(r.m[i])>0.004)) set.add(r[field]); });
  M.cf.forEach(r=>{ if (ok(r) && ix.some(i=>Math.abs(r.m[i])>0.004)) set.add(r[field]); });
  M.lanc.forEach(r=>{ if (ok(r) && (!r.comp || r.comp.startsWith(Y))) set.add(r[field]); });
  return set;
}
function buildFilters(){
  $('#years').innerHTML = IDX.years.map(y=>`<button data-y="${y}" class="${y===S.year?'on':''}">${y}</button>`).join('');
  $$('#years button').forEach(b=>b.onclick=()=>{ S.year=b.dataset.y; buildFilters(); render(); });
  $('#groups').innerHTML = `<button data-g="" class="${!S.grps.length?'on':''}" title="Todos os grupos">Geral</button>` + IDX.grps.map(g=>`<button data-g="${g.k}" class="${S.grps.includes(g.k)?'on':''}" title="Clique para incluir ou tirar da seleção">${S.grps.includes(g.k)?'✓ ':''}${g.k==='OUT'?'Sem divisão':g.l}</button>`).join('');
  $$('#groups button').forEach(b=>b.onclick=()=>{ toggleGrp(b.dataset.g); });
  const opt = (arr, cur, lab, all) => `<option value="">${all}</option>` + arr.map(v=>`<option value="${esc(v)}" ${v===cur?'selected':''}>${esc(lab(v))}</option>`).join('');
  // filtros em cascata: cada lista mostra só o que existe com os demais filtros (grupo, filial, CC, tipo, ano)
  const av = { div: availFor('div'), cc: availFor('cc'), tipo: availFor('tipo'), conta: availFor('conta') };
  let reset = false;
  if (S.div && !av.div.has(S.div)) { S.div = ''; reset = true; }
  if (S.cc && !av.cc.has(S.cc)) { S.cc = ''; reset = true; }
  if (S.tipo && !av.tipo.has(S.tipo)) { S.tipo = ''; reset = true; }
  if (S.conta && !av.conta.has(S.conta)) { S.conta = ''; reset = true; }
  if (reset) { av.div = availFor('div'); av.cc = availFor('cc'); av.tipo = availFor('tipo'); av.conta = availFor('conta'); }
  $('#fDiv').innerHTML = opt(IDX.divs.filter(v=>av.div.has(v)), S.div, v=>v+' · '+(IDX.divDesc[v]||''), 'Todas');
  $('#fCC').innerHTML = opt(IDX.ccs.filter(v=>av.cc.has(v)), S.cc, v=>v+' · '+(IDX.ccDesc[v]||''), 'Todos');
  $('#gCC').innerHTML = opt(IDX.ccs.filter(v=>av.cc.has(v)), S.cc, v=>v+' · '+(IDX.ccDesc[v]||''), S.gest?'Todos do gestor':'Todos');
  $('#gGest').innerHTML = `<option value="">Todos</option>` + GESTORES.map(g=>`<option value="${g.k}" ${S.gest===g.k?'selected':''}>${esc(g.nome)} · ${esc(g.desc)}</option>`).join('');
  { const g = !!ACL.ok; $('#gGest').style.display = g?'none':''; $('#gGestL').style.display = g?'none':''; }
  $('#fTipo').innerHTML = opt(IDX.tipos.filter(v=>av.tipo.has(v)), S.tipo, v=>v, 'Todos');
  $('#fConta').innerHTML = opt(IDX.contas.filter(v=>av.conta.has(v)), S.conta, v=>v+' · '+(IDX.contaDesc[v]||''), 'Todas');
  $('#fStatus').innerHTML = opt(IDX.statuses, S.status, v=>v, 'Todos os status');
  $('#p-st').innerHTML = opt(IDX.statuses.filter(s=>s!=='LANÇADO'), S.pst, v=>v, 'Todas as etapas');
  $$('#tabs button').forEach(b=>b.classList.toggle('on', b.dataset.v===S.view));
  renderChips();
  $$('.view').forEach(v=>v.classList.toggle('on', v.dataset.view===S.view));
}
function toggleGrp(k){
  if (!k) S.grps = [];
  else { S.grps = S.grps.includes(k) ? S.grps.filter(x=>x!==k) : [...S.grps, k]; if (S.grps.length === IDX.grps.length) S.grps = []; }
  if (S.div && S.grps.length && !S.grps.includes(grpOf(S.div))) S.div = '';
  buildFilters(); render();
}
/* Gestores para o relatório (o acesso de cada um vem do servidor, rota /eu). 6810 é compartilhado; no relatório fica com o Carvalho. */
const GESTORES = [
  { k:'carvalho', nome:'Carvalho', ccs:['6720','6150','6810'], desc:'6720 · 6150 (filiais) · 6810' },
  { k:'marlos',   nome:'Marlos',   ccs:['6710'], desc:'6710' },
  { k:'salvio',   nome:'Salvio',   ccs:['6730'], desc:'6730' },
  { k:'klayton',  nome:'Klayton',  ccs:['6620'], grps:['RX'], desc:'RetailX · 6620' }
];
const gestOk = r => { if (!S.gest) return true; const g = GESTORES.find(x=>x.k===S.gest); return !g || g.ccs.includes(String(r.cc)) || (g.grps||[]).includes(r.grp); };
function pass(r, noGrp){
  if (S.ex && r.cc==='6600') return false;
  if (!gestOk(r)) return false;
  if (!noGrp && S.grps.length && !S.grps.includes(r.grp)) return false;
  if (S.cc && r.cc!==S.cc) return false;
  if (S.div && r.div!==S.div) return false;
  if (S.tipo && r.tipo!==S.tipo) return false;
  if (S.conta && r.conta!==S.conta) return false;
  return true;
}

/* ---------- agregações ---------- */
function compute(noGrp){
  const months = M.meta.months, lc = M.meta.lastClosed, Y = S.year;
  const yIx = months.map((m,i)=>m.startsWith(Y)?i:-1).filter(i=>i>=0);
  const ymY = yIx.map(i=>months[i]);
  const closedN = ymY.filter(m=>m<=lc).length;
  const B = M.budget.filter(r=>pass(r,noGrp)), CF = M.cf.filter(r=>pass(r,noGrp)), L = M.lanc.filter(r=>pass(r,noGrp));
  const mB = yIx.map(i=>sum(B.map(b=>b.m[i])));
  const mCF = yIx.map(i=>sum(CF.map(c=>c.m[i])));
  const mR = ymY.map(()=>0), mC = ymY.map(()=>0);
  const LY = L.filter(l=>l.comp && l.comp.startsWith(Y));
  LY.forEach(l=>{ const k = ymY.indexOf(l.comp); if(k<0) return; if(l.classif==='Realizado') mR[k]+=l.valor; else mC[k]+=l.valor; });
  const t = { B: sum(mB), CF: sum(mCF), R: sum(mR), C: sum(mC), Bytd: sum(mB.slice(0,closedN)), Rytd: sum(mR.slice(0,closedN)), CFytd: sum(mCF.slice(0,closedN)), Copen: sum(mC.slice(0,closedN)), closedN };
  const nextY = String(+Y+1), nIx = months.map((m,i)=>m.startsWith(nextY)?i:-1).filter(i=>i>=0);
  function by(keyFn, labelFn){
    const g = {};
    const get = k => g[k] || (g[k] = { k, label: labelFn(k), B:0, CF:0, R:0, C:0, Bytd:0, Rytd:0, Bn:0 });
    B.forEach(b=>{ const o=get(keyFn(b)); yIx.forEach((i,j)=>{ o.B+=b.m[i]; if(j<closedN) o.Bytd+=b.m[i]; }); nIx.forEach(i=>o.Bn+=b.m[i]); });
    CF.forEach(c=>{ const o=get(keyFn(c)); yIx.forEach(i=>{ o.CF+=c.m[i]; }); });
    LY.forEach(l=>{ const o=get(keyFn(l)); if(l.classif==='Realizado'){ o.R+=l.valor; if(l.comp<=lc) o.Rytd+=l.valor; } else o.C+=l.valor; });
    return Object.values(g).map(o=>(o.V=o.B-o.CF,o)).filter(o=>Math.abs(o.B)+Math.abs(o.CF)+Math.abs(o.R)+Math.abs(o.C)+Math.abs(o.Bn)>0.5);
  }
  return { Y, nextY, yIx, ymY, mB, mCF, mR, mC, t, B, CF, L, LY, by,
    byCC: by(r=>r.cc||'N/D', k=>IDX.ccDesc[k]||''),
    byTipo: by(r=>r.tipo||'Não classificado', k=>k),
    byKey: by(r=>[r.div,r.cc,r.conta].join('|'), k=>k),
    byGrp: by(r=>r.grp, k=>GLABEL[k]) };
}

/* ---------- resumo do período (mês) e relatório para o gestor ---------- */
function selLabel(){
  const g = GESTORES.find(x=>x.k===S.gest), p = [];
  if (g) p.push('Gestor '+g.nome+' ('+g.desc+')');
  if (S.cc) p.push('CC '+S.cc+' '+(IDX.ccDesc[S.cc]||''));
  if (S.div) p.push('Filial '+(IDX.divDesc[S.div]||S.div));
  if (S.grps.length) p.push(S.grps.map(k=>GLABEL[k]).join(' + '));
  if (S.tipo) p.push(S.tipo); if (S.conta) p.push('Conta '+S.conta);
  return p.length ? p.join(' · ') : 'Todo o TI';
}
function mesData(D, mes){
  const i = D.ymY.indexOf(mes), lc = M.meta.lastClosed, ms = M.meta.sapMonths||[];
  const fech = mes <= lc, sap = ms.includes(mes);
  const acB = sum(D.mB.slice(0,i+1)), acR = sum(D.mR.slice(0,i+1)), acReal = sum(D.ymY.slice(0,i+1).map((m,k)=>m<=lc?D.mR[k]:D.mR[k]+D.mC[k]));
  const PG = (window.FCX_PAG && window.FCX_PAG.byId) || {};
  const plan = (M.lancPlan||[]).filter(l=>pass(l) && l.comp && l.comp.startsWith(D.Y));
  const notas = plan.filter(l=>l.comp===mes && l.classif==='Realizado').map(l=>Object.assign({}, l, { pg: PG[l.id]||l.pag||null })).sort((a,b)=>b.valor-a.valor);
  const aLancVenc = plan.filter(l=>l.classif!=='Realizado' && l.comp<=mes && l.comp<=lc).sort((a,b)=>a.comp.localeCompare(b.comp)||b.valor-a.valor);
  const pagProb = plan.filter(l=>l.classif==='Realizado' && l.comp<=mes).map(l=>Object.assign({}, l, { pg: PG[l.id]||l.pag||null })).filter(l=>l.pg && (l.pg.st==='Vencida' || l.pg.bloq)).sort((a,b)=>b.valor-a.valor);
  const fg = {}; D.LY.filter(l=>l.comp===mes).forEach(l=>{ const k = fornKey(l.forn), o = fg[k] || (fg[k] = { n:{}, R:0, C:0 }); o.n[l.forn||'(sem fornecedor)'] = (o.n[l.forn]||0)+Math.abs(l.valor); if (l.classif==='Realizado') o.R += l.valor; else o.C += l.valor; });
  const forn = Object.values(fg).map(o=>({ nome:Object.entries(o.n).sort((a,b)=>b[1]-a[1])[0][0], R:o.R, C:o.C, T:o.R+o.C })).filter(o=>Math.abs(o.T)>0.5).sort((a,b)=>b.T-a.T);
  const ccs = D.byCC.map(o=>{ const k = o.k; const B = D.B.filter(b=>(b.cc||'N/D')===k), CF = D.CF.filter(c=>(c.cc||'N/D')===k), L = D.LY.filter(l=>(l.cc||'N/D')===k);
    const bm = sum(B.map(b=>b.m[D.yIx[i]])), rm = sum(L.filter(l=>l.comp===mes&&l.classif==='Realizado').map(l=>l.valor)), ab = sum(B.map(b=>sum(D.yIx.slice(0,i+1).map(j=>b.m[j])))), ar = sum(L.filter(l=>l.comp<=mes&&l.classif==='Realizado').map(l=>l.valor));
    return { k, label:o.label, bm, rm, ab, ar, B:o.B, CF:o.CF, V:o.V }; }).filter(o=>Math.abs(o.B)+Math.abs(o.CF)+Math.abs(o.ar)>0.5).sort((a,b)=>b.B-a.B);
  const P = priorData(D), prior = sum(P.R.filter(x=>x.l.comp<=mes).map(x=>x.l.valor)), priorMes = sum(P.R.filter(x=>x.l.comp===mes).map(x=>x.l.valor));
  return { prior, priorMes, i, mes, fech, sap, b:D.mB[i]||0, r:D.mR[i]||0, c:D.mC[i]||0, cf:D.mCF[i]||0, acB, acR, B:D.t.B, CF:D.t.CF, notas, aLancVenc, pagProb, forn, ccs };
}
const seloHtml = x => x.fech ? `<span class="selo f">Mês fechado${x.sap?' · SAP':''}</span>` : `<span class="selo p">Provisório · mês em aberto</span>`;
function renderMes(D){
  const sel = $('#gMes'); if (S.mes && !D.ymY.includes(S.mes)) S.mes = '';
  sel.innerHTML = `<option value="">Ano até hoje</option>` + D.ymY.map(m=>`<option value="${m}" ${S.mes===m?'selected':''}>${ymLabel(m)}${m>M.meta.lastClosed?' (em aberto)':''}</option>`).join('');
  const box = $('#g-mes'); if (!S.mes){ box.style.display='none'; box.innerHTML=''; return; }
  const x = mesData(D, S.mes), L = ymLabel(S.mes), y0 = ymLabel(D.Y+'-01');
  const dif = x.b - (x.fech ? x.r : x.cf);
  box.style.display = '';
  const acD = x.acB - x.acR, prD = x.B - x.CF;
  box.innerHTML = `<div class="card"><div class="mh"><h2>Resumo de ${L}</h2>${seloHtml(x)}<span class="hint">${esc(selLabel())}</span></div>
    <div class="mgrp">No mês · ${L}</div>
    <div class="grid kpis k4">${[
      kpi('Orçado no mês', short(x.b), 'quanto estava previsto gastar', '', brl(x.b)),
      kpi('Realizado no mês', short(x.r), x.b?deltaSpan(x.r-x.b, x.b)+' vs orçado':'sem orçado no mês', '', brl(x.r)),
      kpi(dif>=0?'Sobrou no mês':'Passou no mês', `<span style="color:${colorV(dif)}">${short(Math.abs(dif))}</span>`, x.fech?'orçado − realizado':'orçado − projeção do mês', '', brl(dif)),
      kpi('A lançar do mês', short(x.c), x.c>0?(x.fech?'<span class="delta up">nota ainda não lançada</span>':'previsto, ainda sem nota'):'nada pendente', '', brl(x.c))
    ].join('')}</div>
    <div class="mgrp">No ano · ${y0} a ${L}</div>
    <div class="grid kpis k3">${[
      kpi(`Gasto acumulado`, short(x.acR), `orçado do período ${short(x.acB)} · ${acD>=0?'<span class="delta down">sobra '+short(acD)+'</span>':'<span class="delta up">passou '+short(-acD)+'</span>'}`, meter(x.acB?x.acR/x.acB:0), brl(x.acR)),
      kpi(`Despesas de anos anteriores`, short(x.prior), x.prior>0?`já incluídas no acumulado (${pct(x.acR?x.prior/x.acR:0,0)}) · notas de ${+D.Y-1} ou antes lançadas em ${D.Y}`:'nenhuma no período', '', brl(x.prior)),
      kpi(`Previsão para ${D.Y}`, short(x.CF), `orçado do ano ${short(x.B)} · ${prD>=0?'<span class="delta down">deve sobrar '+short(prD)+'</span>':'<span class="delta up">deve passar '+short(-prD)+'</span>'}`, '', brl(x.CF))
    ].join('')}</div></div>
    <div class="grid two-eq sec">
      <div class="card"><div class="card-h"><h2>Maiores fornecedores em ${L}</h2><span class="hint">realizado + a lançar</span></div><div class="scroll" id="gm-forn" style="max-height:330px"></div></div>
      <div class="card"><div class="card-h"><h2>Pendências até ${L}</h2><span class="hint">a lançar em mês fechado · notas vencidas ou bloqueadas</span></div><div class="scroll" id="gm-pend" style="max-height:330px"></div></div>
    </div>
    <div class="card sec"><div class="card-h"><h2>Notas lançadas em ${L}</h2><span class="hint">${x.notas.length} nota(s) · ${brl(sum(x.notas.map(l=>l.valor)))} · clique em Lançamentos para o detalhe</span></div><div class="scroll" id="gm-notas" style="max-height:360px"></div></div>`;
  table('#gm-forn', [{l:'Fornecedor', f:o=>esc(o.nome)}, {l:'Realizado', n:1, f:o=>brl(o.R)}, {l:'A lançar', n:1, f:o=>o.C?brl(o.C):'—'}], x.forn.slice(0,15));
  const pend = x.aLancVenc.map(l=>({ t:'A lançar', l })).concat(x.pagProb.map(l=>({ t:l.pg.bloq?'Bloqueada':'Vencida', l })));
  if (!pend.length) $('#gm-pend').innerHTML = '<div class="empty">Nada pendente.</div>';
  else table('#gm-pend', [{l:'Situação', f:p=>`<span class="pill ${p.t==='A lançar'?'pend':'late'}">${p.t}</span>`}, {l:'Mês', f:p=>ymLabel(p.l.comp)}, {l:'Fornecedor', f:p=>esc(p.l.forn)}, {l:'Valor', n:1, f:p=>brl(p.l.valor)}], pend.slice(0,60));
  if (!x.notas.length) $('#gm-notas').innerHTML = '<div class="empty">Nenhuma nota lançada no mês.</div>';
  else table('#gm-notas', [{l:'Fornecedor', f:l=>esc(l.forn)}, {l:'Descrição', f:l=>`<span class="muted">${esc((l.desc||'').slice(0,70))}</span>`}, {l:'CC', f:l=>esc(l.cc)}, {l:'NF', f:l=>esc(l.nf)}, {l:'Valor', n:1, f:l=>brl(l.valor)}, {l:'Pagamento', f:l=>l.pg?`<span class="pill ${PAGCLS[l.pg.st]||''}">${esc(l.pg.st)}${l.pg.bloq?' ⚠':''}</span>`:'<span class="muted small">—</span>'}], x.notas);
}
/* relatório em A4 (layout próprio, só o relevante para o gestor) */
function relChart(D){
  const W = 680, H = 190, pl = 46, pr = 8, pt = 10, pb = 22, n = D.ymY.length, lc = M.meta.lastClosed;
  const v = D.ymY.map((m,i)=>m<=lc ? D.mR[i] : D.mCF[i]); const mx = niceMax(Math.max(1, ...v, ...D.mB));
  const bw = (W-pl-pr)/n, y = val => pt + (H-pt-pb)*(1 - val/mx);
  let g = '';
  for (let k=0;k<=4;k++){ const val = mx*k/4, yy = y(val); g += `<line x1="${pl}" x2="${W-pr}" y1="${yy}" y2="${yy}" stroke="#e3e6ec"/><text x="${pl-6}" y="${yy+3}" font-size="8" text-anchor="end" fill="#666">${axisShort(val)}</text>`; }
  D.ymY.forEach((m,i)=>{ const x = pl + i*bw + bw*0.18, w = bw*0.64, h = Math.max(0, (H-pt-pb)*(Math.max(0,v[i])/mx)), sel = m===S.mes;
    g += `<rect x="${x}" y="${H-pb-h}" width="${w}" height="${h}" fill="${m<=lc?'#2b2f36':'#c4c9d2'}" ${sel?'stroke="#d7262c" stroke-width="2"':''}/>`;
    g += `<text x="${x+w/2}" y="${H-6}" font-size="8.5" text-anchor="middle" fill="${sel?'#d7262c':'#444'}" ${sel?'font-weight="700"':''}>${MES[+m.slice(5)-1]}</text>`; });
  g += `<polyline fill="none" stroke="#d7262c" stroke-width="1.6" stroke-dasharray="4 3" points="${D.mB.map((b,i)=>(pl+i*bw+bw/2)+','+y(b)).join(' ')}"/>`;
  return `<svg viewBox="0 0 ${W} ${H}" width="100%" style="max-height:62mm">${g}</svg>`;
}
function exportResumo(soMontar){
  if (!M) return;
  const D = IDX.D || compute(), mes = S.mes || (D.ymY.filter(m=>m<=M.meta.lastClosed).pop() || D.ymY[0]);
  const x = mesData(D, mes), L = ymLabel(mes), y0 = ymLabel(D.Y+'-01'), sl = selLabel(), prevY = +D.Y - 1;
  const logo = (document.querySelector('.brandbar .logo.lt')||{}).src || '';
  const n = v => brl(v), nn = v => short(v), cls = v => v<0?'neg':'pos';
  const dif = x.b - (x.fech ? x.r : x.cf), acD = x.acB - x.acR, prD = x.B - x.CF;
  const pctTxt = (a,b) => b ? pct(Math.abs(a)/b,0) : '';
  const quem = S.gest ? (GESTORES.find(g=>g.k===S.gest)||{}).desc : (S.cc ? 'CC '+S.cc : 'TI');
  // em resumo (frases simples)
  const f1 = x.b ? `Em <b>${L}</b> foram gastos <b>${nn(x.r)}</b>, ${dif>=0?`<span class="pos">${nn(dif)} abaixo</span>`:`<span class="neg">${nn(-dif)} acima</span>`} do orçado para o mês (${nn(x.b)}).`
                 : `Em <b>${L}</b> foram gastos <b>${nn(x.r)}</b> (não havia orçado para o mês).`;
  const f2 = `De ${y0} a ${L} o gasto acumulado é <b>${nn(x.acR)}</b> para um orçado de ${nn(x.acB)}: ${acD>=0?`<span class="pos">sobram ${nn(acD)}</span>`:`<span class="neg">passou ${nn(-acD)} (${pctTxt(acD,x.acB)})</span>`}.`
           + (x.prior>0.5 ? ` Desse total, <b>${nn(x.prior)}</b> são despesas de ${prevY} ou antes que só foram lançadas em ${D.Y}.` : '');
  const f3 = `A previsão para o ano de ${D.Y} é <b>${nn(x.CF)}</b> para um orçado de ${nn(x.B)}: ${prD>=0?`<span class="pos">deve sobrar ${nn(prD)}</span>`:`<span class="neg">deve passar ${nn(-prD)}</span>`}.`;
  const f4 = x.fech ? '' : `<b>Atenção:</b> ${L} ainda está aberto; os números do mês podem mudar até o fechamento.`;
  let h = `<div class="rh">${logo?`<img src="${logo}" alt="FCx Labs">`:''}<div><h1>Resumo de despesas de TI · ${L}</h1><div class="s">${esc(sl)} · <span class="sl ${x.fech?'f':'p'}">${x.fech?'Mês fechado':'Provisório (mês em aberto)'}</span></div></div></div>`;
  h += `<h2>Em resumo</h2><div class="resumo"><p>${f1}</p><p>${f2}</p><p>${f3}</p>${f4?`<p>${f4}</p>`:''}</div>`;
  let k = 1;
  h += `<h2><span class="num">${k++}</span>No mês · ${L}</h2><div class="sub">O que estava previsto para o mês e o que foi gasto.</div>
    <div class="blk b4">
      <div class="c"><div class="l">Orçado no mês</div><div class="v">${n(x.b)}</div><div class="f">quanto estava previsto</div></div>
      <div class="c dest"><div class="l">Realizado no mês</div><div class="v">${n(x.r)}</div><div class="f">${x.b?pct(x.r/x.b,0)+' do orçado':''}</div></div>
      <div class="c"><div class="l">${dif>=0?'Sobrou no mês':'Passou no mês'}</div><div class="v ${cls(dif)}">${n(Math.abs(dif))}</div><div class="f">${x.fech?'orçado − realizado':'orçado − projeção do mês'}</div></div>
      <div class="c"><div class="l">A lançar do mês</div><div class="v">${n(x.c)}</div><div class="f">${x.c>0?'previsto, nota ainda não lançada':'nada pendente'}</div></div>
    </div>`;
  h += `<h2><span class="num">${k++}</span>No ano · ${y0} a ${L}</h2><div class="sub">Tudo o que foi gasto desde janeiro, comparado com o orçado do mesmo período.</div>
    <div class="blk b3">
      <div class="c dest"><div class="l">Gasto acumulado</div><div class="v">${n(x.acR)}</div><div class="f">orçado do período ${n(x.acB)} (${pct(x.acB?x.acR/x.acB:null,0)} usado)</div></div>
      <div class="c"><div class="l">${acD>=0?'Sobra no período':'Passou no período'}</div><div class="v ${cls(acD)}">${n(Math.abs(acD))}</div><div class="f">orçado − gasto acumulado</div></div>
      <div class="c"><div class="l">Despesas de anos anteriores</div><div class="v">${n(x.prior)}</div><div class="f">${x.prior>0.5?`notas de ${prevY} ou antes lançadas em ${D.Y}; já estão no gasto acumulado`:'nenhuma no período'}</div></div>
    </div>`;
  h += `<h2><span class="num">${k++}</span>Previsão para ${D.Y}</h2><div class="sub">Meses fechados pelo realizado; meses que faltam pelo orçado ou pelo que já está previsto, o que for maior.</div>
    <div class="blk b3">
      <div class="c"><div class="l">Orçado do ano</div><div class="v">${n(x.B)}</div></div>
      <div class="c dest"><div class="l">Previsão do ano</div><div class="v">${n(x.CF)}</div></div>
      <div class="c"><div class="l">${prD>=0?'Deve sobrar':'Deve passar'}</div><div class="v ${cls(prD)}">${n(Math.abs(prD))}</div><div class="f">${pctTxt(prD,x.B)} do orçado do ano</div></div>
    </div>`;
  h += `<h2><span class="num">${k++}</span>Mês a mês em ${D.Y}</h2><div class="sub">Barra escura = gasto do mês fechado · barra cinza = previsão dos meses que faltam · linha vermelha = orçado · mês do relatório com contorno vermelho.</div>` + relChart(D);
  if (x.ccs.length > 1) h += `<div class="keep"><h2><span class="num">${k++}</span>Por centro de custo</h2><div class="sub">Mesmos números, separados por CC.</div><table><thead><tr><th>CC</th><th class="n">Orçado ${L}</th><th class="n">Gasto ${L}</th><th class="n">Orçado ${y0}–${L}</th><th class="n">Gasto ${y0}–${L}</th><th class="n">Previsão ${D.Y}</th><th class="n">Sobra/passa no ano</th></tr></thead><tbody>${
    x.ccs.map(o=>`<tr><td>${esc(o.k)} ${esc(o.label||'')}</td><td class="n">${n(o.bm)}</td><td class="n">${n(o.rm)}</td><td class="n">${n(o.ab)}</td><td class="n">${n(o.ar)}</td><td class="n">${n(o.CF)}</td><td class="n ${cls(o.V)}">${n(o.V)}</td></tr>`).join('')}</tbody></table></div>`;
  h += `<div class="keep"><h2><span class="num">${k++}</span>Onde foi o gasto de ${L}</h2><div class="sub">Maiores fornecedores do mês.</div>` + (x.forn.length ? `<table><thead><tr><th>Fornecedor</th><th class="n">Gasto no mês</th><th class="n">Ainda a lançar</th></tr></thead><tbody>${x.forn.slice(0,10).map(o=>`<tr><td>${esc(o.nome)}</td><td class="n">${n(o.R)}</td><td class="n">${o.C?n(o.C):'—'}</td></tr>`).join('')}</tbody></table></div>` : '<div class="nota">Sem lançamentos no mês.</div></div>');
  const NMAX = 15, resto = x.notas.slice(NMAX);
  h += `<h2><span class="num">${k++}</span>Principais notas de ${L}</h2><div class="sub">${x.notas.length} nota(s) lançada(s) no mês, total ${n(sum(x.notas.map(l=>l.valor)))}. Abaixo, as ${Math.min(NMAX,x.notas.length)} maiores e a situação do pagamento.${Math.abs(sum(x.notas.map(l=>l.valor))-x.r)>1?` A lista vem da planilha; o realizado do mês (${n(x.r)}) é o oficial do SAP, por isso os totais podem diferir.`:''}</div>` + (x.notas.length ? `<table><thead><tr><th>Fornecedor</th><th>Descrição</th><th>CC</th><th class="n">Valor</th><th>Pagamento</th></tr></thead><tbody>${
    x.notas.slice(0,NMAX).map(l=>`<tr><td>${esc(l.forn)}</td><td>${esc((l.desc||'').slice(0,60))}</td><td>${esc(l.cc)}</td><td class="n">${n(l.valor)}</td><td>${l.pg?esc(l.pg.st)+(l.pg.bloq?' (bloqueio)':'')+(l.pg.dtComp?' em '+dBR(l.pg.dtComp):''):'—'}</td></tr>`).join('')}</tbody></table>${resto.length?`<div class="nota">Outras ${resto.length} nota(s) menores somam ${n(sum(resto.map(l=>l.valor)))}.</div>`:''}` : '<div class="nota">Nenhuma nota lançada no mês.</div>');
  const pend = x.aLancVenc.map(l=>['Ainda não lançada', l]).concat(x.pagProb.map(l=>[l.pg.bloq?'Pagamento bloqueado':'Pagamento vencido', l]));
  h += `<h2><span class="num">${k++}</span>O que está pendente</h2><div class="sub">Itens de meses já fechados que ainda não foram lançados e notas com pagamento vencido ou bloqueado.</div>` + (pend.length ? `<table><thead><tr><th>Situação</th><th>Mês</th><th>Fornecedor</th><th>Descrição</th><th class="n">Valor</th></tr></thead><tbody>${pend.slice(0,15).map(([t,l])=>`<tr><td>${t}</td><td>${ymLabel(l.comp)}</td><td>${esc(l.forn)}</td><td>${esc((l.desc||'').slice(0,55))}</td><td class="n">${n(l.valor)}</td></tr>`).join('')}</tbody></table>${pend.length>15?`<div class="nota">+ ${pend.length-15} item(ns), total ${n(sum(pend.slice(15).map(p=>p[1].valor)))}.</div>`:''}` : '<div class="nota">Nada pendente.</div>');
  h += `<div class="glos"><b>Como ler este relatório</b>
    <div><b>Orçado:</b> valor aprovado para gastar. <b>Realizado / gasto:</b> notas lançadas no SAP no mês (mês fechado = número oficial).</div>
    <div><b>A lançar:</b> despesa prevista cuja nota ainda não foi lançada. <b>Previsão do ano:</b> o que já foi gasto + o que se espera gastar até dezembro.</div>
    <div><b>Despesas de anos anteriores:</b> serviço de ${prevY} ou antes cuja nota só foi lançada em ${D.Y}; conta no ano em que foi lançada.</div>
    <div><b>Fechamento:</b> o mês fecha no dia 6 do mês seguinte. Lançamentos retroativos feitos até o dia 5 entram no mês que fecha; depois disso, contam no mês atual.</div></div>
    <div class="ft">Uso interno · ${esc(sl)} · gerado em ${new Date().toLocaleString('pt-BR')} · Painel de Despesas - TI (FCx Labs · Administrativo)</div>`;
  const rel = $('#rel'); rel.innerHTML = h;
  if (soMontar === true) return { quem, L };  // só monta o relatório (usado para gerar o PDF por trás)
  const t0 = document.title; document.title = `Resumo TI - ${quem.replace(/[\\/:*?"<>|·]/g,' ').replace(/\s+/g,' ').trim()} - ${L.replace('/','-')}`;
  document.body.classList.add('rel-print');
  const done = ()=>{ document.body.classList.remove('rel-print'); document.title = t0; window.removeEventListener('afterprint', done); };
  window.addEventListener('afterprint', done);
  setTimeout(()=>{ window.print(); setTimeout(()=>{ if (document.body.classList.contains('rel-print') && !matchMedia('print').matches) done(); }, 1500); }, 50);
}
/* ---------- render geral ---------- */
function render(){
  if (!M) return;
  try{ renderCloseBanner(); }catch(e){}
  const D = compute(); IDX.D = D;
  const lc = M.meta.lastClosed;
  { const ms = M.meta.sapMonths||[], up = new Date(M.meta.parsedAt), opn = M.meta.months.filter(m=>m>lc);
    const src = ms.length ? `realizado oficial <b>SAP (KSB1) até ${ymLabel(ms[ms.length-1])}</b>` : 'realizado <b>pela planilha</b> (sem KSB1)';
    const prov = M.meta.months.filter(m=>m<=lc && m.startsWith(M.meta.year||'') && !ms.includes(m));
    $('#subline').innerHTML = (M.meta.server?`<b>${M.meta.server.estado==='previa'?'Prévia do SAP (não publicada)':'Publicado pelo servidor'}</b> · `:'') + `Atualizado em <b>${up.toLocaleDateString('pt-BR')} ${up.toLocaleTimeString('pt-BR',{hour:'2-digit',minute:'2-digit'})}</b> · ${src}${prov.length?` · ${prov.map(ymLabel).join(', ')} pela planilha (provisório)`:''} · fechado até <b>${ymLabel(lc)}</b> (fecha no dia 6)` + ((M.meta.tardios&&M.meta.tardios.n)?` · <span title="Lançamentos feitos no SAP do dia 6 em diante com data do mês fechado">${M.meta.tardios.n} lançamento(s) do SAP após o fechamento (${short(M.meta.tardios.valor)}) contados em <b>${ymLabel(M.meta.tardios.mes)}</b></span>`:'') + ((M.meta.warn||[]).length?` · <span style="color:var(--crit-ink)">${esc(M.meta.warn.join(' '))}</span>`:''); }
  IDX.mov = computeMov();
  $$('.movTitle').forEach(e=>e.textContent = `Movimentação de ${ymLabel(lc)}`);
  const V = S.view;
  if (V==='geral'){ renderMes(D); renderGeralKPIs(D); renderPriorStrip('#g-prior', D); monthlyMain('#g-cMonthly', D); renderAccGeral(D); $('#g-tMonthly').textContent=`Mês a mês — ${D.Y}`; renderAlerts(D); renderGrp(D); ccChart('#g-cCC', D); renderMov('g'); wireMov('g'); wireKpis('#g-kpis', ['orc','fcst','fcst','desp','prov','orc']);
    fornBars('#g-forn', fornData(D).filter(o=>o.U>0.5).sort((a,b)=>b.U-a.U).slice(0,8)); }
  if (V==='orc') renderOrc(D);
  if (V==='desp'){ renderDesp(D); wireMov('d'); renderPriorStrip('#d-prior', D); renderAccDesp(D); }
  if (V==='prov') renderProv(D);
  if (V==='forn') renderForn(D);
  if (V==='fcst') renderFcst(D);
  if (V==='lanc'){ renderBack(); renderLanc(); }
  if (V==='conf') renderConf(D);
  const f = [];
  if (S.grps.length) f.push('Grupo '+S.grps.map(k=>GLABEL[k]).join(' + ')); if (S.div) f.push('Filial '+(IDX.divDesc[S.div]||S.div)); if (S.cc) f.push('CC '+S.cc+' '+(IDX.ccDesc[S.cc]||'')); if (S.tipo) f.push(S.tipo); if (S.conta) f.push('Conta '+S.conta+' '+(IDX.contaDesc[S.conta]||''));
  const fs = M.meta.sources||{}, fn = o=>o?esc(o.name):'—';
  $('#footnote').innerHTML = `Filtros ativos: ${f.length?esc(f.join(' · ')):'nenhum'}. CC 6600 (Lei do Bem) fica fora do painel.<br>
  Arquivos lidos: ${fn(fs.main)} · ${fn(fs.orc)} · KSB1: ${(fs.ksb1||[]).map(fn).join(', ')||'—'} · KSB2: ${fn(fs.ksb2)} · FBL1N: ${fn(fs.fbl1n)}${RAW&&RAW.plur?` · anos seguintes da projeção: ${esc(RAW.plur.fonte||'FC FY27 Forecast')}`:''}.<br>
  Grupos: Ferreira Costa = divisões 91 a 1009 + imobilizado (IMOB) · RetailX = 4016 · Importadoras = divisões cuja descrição começa com “IMPORTADORA”. Descrições de CC, filial e conta vêm da aba Parâmetros.<br>
  Orçado = planilha Orçamento TI${M.meta.orcFonte==='forecast'?` (${esc(M.meta.year)}: FC FY27 Forecast até o orçamento oficial ser divulgado)`:''}. Anos no painel: ${esc((M.meta.anos||[]).join(', '))}. Realizado: mês fechado com KSB1 = SAP (pedidos que estão na planilha) + imobilizado e itens sem pedido da planilha; mês sem KSB1 = planilha (status LANÇADO), provisório. A lançar = demais status da planilha.
  Projeção: mês fechado = realizado; mês em aberto = o maior valor entre orçado e (lançado + a lançar), por filial + CC + conta. Sobra = Orçado − Projeção.<br>
  Fechamento: todo dia 6 o mês anterior fecha automaticamente. Fica no mês fechado o lançamento com data do mês anterior que entrou no SAP até o dia 5 (retroativo); lançamento com data do mês atual é sempre do mês atual, mesmo entrando do dia 1 ao 5; o que entrar do dia 6 em diante conta no mês atual. Para atualizar, clique em “Importar planilha”.`;
}

function kpi(label, val, foot, extra='', title=''){
  return `<div class="card kpi" ${title?`title="${esc(title)}"`:''}><div class="lbl"><span>${label}</span></div><div class="val">${val}</div><div class="foot">${foot}</div>${extra}</div>`;
}
const meter = (p, p2) => `<div class="meter"><i style="width:${Math.max(0,Math.min(100,(p||0)*100))}%"></i>${p2!=null?`<i class="c" style="left:${Math.min(100,(p||0)*100)}%;width:${Math.max(0,Math.min(100-(p||0)*100,(p2||0)*100))}%"></i>`:''}</div>`;
function deltaSpan(v, base){
  if (!base) return `<span class="delta flat">sem orçado</span>`;
  const p = v/base, cls = Math.abs(p)<0.005?'flat':(v>0?'up':'down');
  return `<span class="delta ${cls}">${cls==='flat'?'●':(v>0?'▲':'▼')} ${pct(Math.abs(p))} ${v>0?'acima':'abaixo'}</span>`;
}
const colorV = v => v>=0?'var(--good-ink)':'var(--crit-ink)';
function renderGeralKPIs(D){
  const t = D.t, Y = D.Y, lc = M.meta.lastClosed, exec = t.Bytd? t.Rytd/t.Bytd : null, livre = t.B - t.R - t.C;
  $('#g-kpis').innerHTML = [
    kpi(`Orçado ${Y}`, short(t.B), `orçado no ano · ${brl0(t.B/12)}/mês médio`, '', brl(t.B)),
    kpi(`Projeção ${Y}`, short(t.CF), deltaSpan(t.CF-t.B, t.B)+' vs orçado', '', brl(t.CF)),
    kpi('Deve sobrar do orçado', `<span style="color:${colorV(t.B-t.CF)}">${short(t.B-t.CF)}</span>`, t.B-t.CF>=0?'Orçado − Projeção: abaixo do orçado':'Projeção acima do orçado', '', brl(t.B-t.CF)),
    t.closedN===0 ? kpi(`Realizado ${Y}`, '—', 'ano ainda sem meses fechados') :
      kpi(`Realizado até ${ymLabel(t.closedN===12?Y+'-12':lc)}`, short(t.Rytd), `${pct(exec)} do orçado do período (${short(t.Bytd)})`, meter(exec), brl(t.Rytd)),
    kpi('A lançar no ano', short(t.C), t.Copen>0?`<span class="delta up">▲ ${short(t.Copen)}</span> em meses já fechados`:'nada pendente em mês fechado', '', brl(t.C)),
    kpi('Saldo disponível', `<span style="color:${livre>=0?'inherit':'var(--crit-ink)'}">${short(livre)}</span>`, 'orçado − realizado − a lançar', t.B?meter(t.R/t.B, t.C/t.B):'', brl(livre))
  ].join('');
}

/* ---------- gráficos ---------- */
const tt = $('#tt');
function showTT(ev, html, wide){
  tt.innerHTML = html; tt.className = 'tt' + (wide?' wide':''); tt.style.display='block';
  const pad = 14, w = tt.offsetWidth, h = tt.offsetHeight;
  const px = ev.pageX!=null ? ev.pageX : ev.x, py = ev.pageY!=null ? ev.pageY : ev.y;
  let x = px + pad, y = py + pad;
  if (x + w > window.scrollX + document.documentElement.clientWidth - 8) x = px - w - pad;
  if (y + h > window.scrollY + window.innerHeight - 8) y = py - h - pad;
  tt.style.left = Math.max(window.scrollX+8,x)+'px'; tt.style.top = y+'px';
}
function hideTT(){ tt.style.display='none'; }
function niceMax(v){ if(v<=0) return 1; const p = Math.pow(10, Math.floor(Math.log10(v))); const n = v/p; return (n<=1?1:n<=2?2:n<=2.5?2.5:n<=5?5:10)*p; }
function axisShort(v){ const a=Math.abs(v); if(a>=1e6) return (v/1e6).toLocaleString('pt-BR',{maximumFractionDigits:1})+' mi'; if(a>=1e3) return (v/1e3).toLocaleString('pt-BR',{maximumFractionDigits:0})+' mil'; return nf0.format(v); }
function roundTopBar(x,y,w,h,r){ if(h<=0) return ''; r=Math.min(r,h,w/2); return `M${x},${y+h}V${y+r}Q${x},${y} ${x+r},${y}H${x+w-r}Q${x+w},${y} ${x+w},${y+r}V${y+h}Z`; }
const row = (sw, k, v) => `<div class="r"><span>${sw}${k}</span><span>${v}</span></div>`;
const swc = c => `<i class="sw" style="background:${c}"></i>`;
const swl = c => `<i class="sw line" style="background:${c}"></i>`;

/* barras + linha opcional, eixo único */
function barLine(sel, o){
  const el = $(sel); if (!el) return;
  const W = el.clientWidth || 600, H = o.h || 270, L = 56, R = 10, T = 14, Bm = 26;
  const n = o.labels.length;
  const tops = o.bars.map((v,i)=>v+(o.bars2?Math.max(0,o.bars2[i]||0):0));
  const ymax = niceMax(Math.max(...tops, ...(o.line||[]), 1)*1.05);
  const iw = W-L-R, ih = H-T-Bm, band = iw/n, bw = Math.max(6, Math.min(o.maxBar||44, band*0.56));
  const y = v => T + ih - (Math.max(0,v)/ymax)*ih;
  let g = '';
  for (let k=0;k<=4;k++){ const v = ymax*k/4; g += `<line x1="${L}" x2="${W-R}" y1="${y(v)}" y2="${y(v)}" stroke="var(--line)"/><text x="${L-8}" y="${y(v)+4}" text-anchor="end" font-size="11" fill="var(--ink-3)">${axisShort(v)}</text>`; }
  if (o.split>0 && o.split<n){ const xx = L + band*o.split; g += `<line x1="${xx}" x2="${xx}" y1="${T}" y2="${T+ih}" stroke="var(--ink-3)"/><text x="${xx+6}" y="${T+10}" font-size="11" fill="var(--ink-2)">${o.splitR||'meses em aberto →'}</text><text x="${xx-6}" y="${T+10}" font-size="11" text-anchor="end" fill="var(--ink-2)">${o.splitL||'← fechado'}</text>`; }
  o.bars.forEach((v,i)=>{
    const x = L + band*i + (band-bw)/2;
    const fc0 = typeof o.color==='function'?o.color(i):o.color, b2 = o.bars2?Math.max(0,o.bars2[i]||0):0;
    g += `<path d="${roundTopBar(x, y(v), bw, Math.max(0,T+ih-y(v)), b2>0?0:4)}" fill="${fc0==='var(--fcst)'?'url(#fcx-hatch)':fc0}"/>`;
    if (b2>0) g += `<path d="${roundTopBar(x, y(v+b2), bw, Math.max(0,y(v)-y(v+b2)), 4)}" fill="url(#fcx-hatch)"/>`;
    g += `<text x="${L+band*i+band/2}" y="${H-8}" text-anchor="middle" font-size="11" fill="var(--ink-2)">${esc(o.labels[i])}</text>`;
    if (o.valueLabels) g += `<text x="${L+band*i+band/2}" y="${y(v)-6}" text-anchor="middle" font-size="11" fill="var(--ink)" font-weight="600">${axisShort(v)}</text>`;
  });
  if (o.line){
    const pts = o.line.map((v,i)=>[L+band*i+band/2, y(v)]);
    g += `<polyline points="${pts.map(p=>p.join(',')).join(' ')}" fill="none" stroke="var(--base)" stroke-width="2" stroke-dasharray="6 4" stroke-linejoin="round"/>`;
    pts.forEach(p=> g += `<circle cx="${p[0]}" cy="${p[1]}" r="4" fill="var(--surface)" stroke="var(--base)" stroke-width="2"/>`);
  }
  o.labels.forEach((_,i)=>{ g += `<rect class="hit" data-i="${i}" x="${L+band*i}" y="${T}" width="${band}" height="${ih}" fill="transparent"/>`; });
  el.innerHTML = `<svg viewBox="0 0 ${W} ${H}" height="${H}" role="img" aria-label="${esc(o.aria||'gráfico')}">${g}</svg>`;
  if (o.tip) el.querySelectorAll('.hit').forEach(r=>{ r.addEventListener('mousemove', ev=>showTT(ev, o.tip(+r.dataset.i)+(o.click?'<div class="small muted" style="margin-top:6px">Clique para ver as notas do mês</div>':''))); r.addEventListener('mouseleave', hideTT); });
  if (o.click) el.querySelectorAll('.hit').forEach(r=>{ r.style.cursor='pointer'; r.addEventListener('click', ()=>{ hideTT(); o.click(+r.dataset.i); }); });
}

function priorTip(D,i){
  const P = IDX.prior || priorData(D); const v = P.byM[i] || 0;
  return v>0 ? `<div class="small" style="margin-top:4px;color:var(--ink-2)">↳ ${brl0(v)} são despesas de anos anteriores lançadas neste mês</div>` : '';
}
function accTip(D,i){
  const aB = sum(D.mB.slice(0,i+1)), aP = sum(D.mCF.slice(0,i+1)), s = aB-aP;
  return `<div class="h" style="margin-top:6px">Acumulado até ${ymLabel(D.ymY[i])}</div>`+row('','Orçado',brl0(aB))+row('','Realizado / Projeção',brl0(aP))+row('','Saldo acumulado',`<b style="color:${colorV(s)}">${brl0(s)}</b>`);
}
function monthlyMain(sel, D){
  const lc = M.meta.lastClosed;
  barLine(sel, { labels: D.ymY.map(m=>MES[+m.slice(5)-1]+(m===IDX.curYM?'*':'')), bars: D.ymY.map((ym,i)=> ym<=lc ? D.mR[i] : D.mCF[i]), color: i=> D.ymY[i]<=lc?'var(--real)':'var(--fcst)', line: D.mB, split: D.t.closedN, aria:'Despesas mensais', click: i=>goLanc({mes:D.ymY[i]}),
    tip: i=>{ const ym=D.ymY[i], v=D.mB[i]-D.mCF[i]; return `<div class="t">${ymLabel(ym)} · ${ym<=lc?'fechado':(ym===IDX.curYM?'em andamento':'em aberto')}</div>`+(ym===IDX.curYM?`<div class="small muted" style="margin-bottom:4px">Já lançado até hoje: ${brl0(D.mR[i])}</div>`:'')+row(swl('var(--base)'),'Orçado',brl0(D.mB[i]))+row(swc('var(--real)'),'Realizado',brl0(D.mR[i]))+row('','A lançar',brl0(D.mC[i]))+row(swc('var(--fcst)'),'Projeção',brl0(D.mCF[i]))+row('','Saldo do mês',`<b style="color:${colorV(v)}">${brl0(v)}</b>`)+priorTip(D,i)+accTip(D,i); } });
  const mx = D.ymY.map((ym,i)=>({ym, v:D.mCF[i]-D.mB[i]})).sort((a,b)=>b.v-a.v)[0];
  $('#g-hMonthly').textContent = mx && mx.v>0 ? `mês mais acima do orçado: ${ymLabel(mx.ym)} (+${short(mx.v)})` : 'nenhum mês acima do orçado';
}
function monthlyTable(sel, D){
  let tb = `<table><thead><tr><th>Mês</th><th class="n">Orçado</th><th class="n">Realizado</th><th class="n">A lançar</th><th class="n">Projeção</th><th class="n">Sobra</th></tr></thead><tbody>`;
  D.ymY.forEach((ym,i)=>{ tb += `<tr><td>${ymLabel(ym)}</td><td class="n">${brl0(D.mB[i])}</td><td class="n">${brl0(D.mR[i])}</td><td class="n">${brl0(D.mC[i])}</td><td class="n">${brl0(D.mCF[i])}</td><td class="n">${brl0(D.mB[i]-D.mCF[i])}</td></tr>`; });
  tb += `</tbody><tfoot><tr><td>Total</td><td class="n">${brl0(D.t.B)}</td><td class="n">${brl0(D.t.R)}</td><td class="n">${brl0(D.t.C)}</td><td class="n">${brl0(D.t.CF)}</td><td class="n">${brl0(D.t.B-D.t.CF)}</td></tr></tfoot></table>`;
  $(sel).innerHTML = tb;
}

function cumChart(sel, D){
  const el = $(sel); const W = el.clientWidth || 600, H = 250, L = 56, R = 86, T = 14, Bm = 26;
  let cb=0, cc=0; const aB = D.mB.map(v=>cb+=v), aC = D.mCF.map(v=>cc+=v);
  const n = aB.length, ymax = niceMax(Math.max(...aB, ...aC, 1)*1.05);
  const iw = W-L-R, ih = H-T-Bm, step = n>1? iw/(n-1) : iw;
  const x = i => L + step*i, y = v => T + ih - (v/ymax)*ih;
  let g = '';
  for (let k=0;k<=4;k++){ const v = ymax*k/4; g += `<line x1="${L}" x2="${W-R}" y1="${y(v)}" y2="${y(v)}" stroke="var(--line)"/><text x="${L-8}" y="${y(v)+4}" text-anchor="end" font-size="11" fill="var(--ink-3)">${axisShort(v)}</text>`; }
  D.ymY.forEach((ym,i)=>{ g += `<text x="${x(i)}" y="${H-8}" text-anchor="middle" font-size="11" fill="var(--ink-2)">${MES[+ym.slice(5)-1]}</text>`; });
  const poly = aB.map((v,i)=>`${x(i)},${y(v)}`).concat(aC.map((v,i)=>`${x(i)},${y(v)}`).reverse()).join(' ');
  g += `<polygon points="${poly}" fill="${aB[n-1]>=aC[n-1]?'var(--pos)':'var(--neg)'}" opacity=".08"/>`;
  g += `<polyline points="${aB.map((v,i)=>x(i)+','+y(v)).join(' ')}" fill="none" stroke="var(--base)" stroke-width="2" stroke-dasharray="6 4"/>`;
  g += `<polyline points="${aC.map((v,i)=>x(i)+','+y(v)).join(' ')}" fill="none" stroke="var(--real)" stroke-width="2"/>`;
  let yb = y(aB[n-1]), yc = y(aC[n-1]); if (Math.abs(yb-yc)<14){ if (yb<yc){ yb-=7; yc+=7; } else { yb+=7; yc-=7; } }
  g += `<circle cx="${x(n-1)}" cy="${y(aB[n-1])}" r="4" fill="var(--surface)" stroke="var(--base)" stroke-width="2"/><circle cx="${x(n-1)}" cy="${y(aC[n-1])}" r="4" fill="var(--real)" stroke="var(--surface)" stroke-width="2"/>`;
  g += `<text x="${x(n-1)+8}" y="${yb+4}" font-size="11" fill="var(--ink-2)">Orçado ${axisShort(aB[n-1])}</text><text x="${x(n-1)+8}" y="${yc+4}" font-size="11" fill="var(--ink)" font-weight="600">Projeção ${axisShort(aC[n-1])}</text>`;
  g += `<line class="cx" x1="0" x2="0" y1="${T}" y2="${T+ih}" stroke="var(--ink-3)" opacity="0"/><rect class="hitc" x="${L}" y="${T}" width="${iw}" height="${ih}" fill="transparent"/>`;
  el.innerHTML = `<svg viewBox="0 0 ${W} ${H}" height="${H}" role="img" aria-label="Acumulado">${g}</svg>`;
  const svg = el.querySelector('svg'), hit = el.querySelector('.hitc'), cx = el.querySelector('.cx');
  hit.addEventListener('mousemove', ev=>{
    const r = svg.getBoundingClientRect(); const px = (ev.clientX - r.left) * (W / r.width);
    const i = Math.max(0, Math.min(n-1, Math.round((px-L)/step)));
    cx.setAttribute('x1', x(i)); cx.setAttribute('x2', x(i)); cx.setAttribute('opacity', 1);
    const gap = aB[i]-aC[i];
    showTT(ev, `<div class="t">Acumulado até ${ymLabel(D.ymY[i])}</div>`+row(swl('var(--base)'),'Orçado',brl0(aB[i]))+row(swl('var(--real)'),'Projeção',brl0(aC[i]))+row('','Sobra acumulada',`<b style="color:${colorV(gap)}">${brl0(gap)}</b>`));
  });
  hit.addEventListener('mouseleave', ()=>{ hideTT(); cx.setAttribute('opacity',0); });
}

function ccChart(sel, D){
  const el = $(sel); const W = el.clientWidth || 600;
  const rows = D.byCC.filter(o=>Math.abs(o.V)>=1).sort((a,b)=>Math.abs(b.V)-Math.abs(a.V)).slice(0,14).sort((a,b)=>b.V-a.V);
  if (!rows.length){ el.innerHTML = '<div class="empty">Sem variação para os filtros atuais.</div>'; return; }
  const rh = 24, T = 6, H = T + rows.length*rh + 6, LBL = Math.min(240, W*0.42), R = 8, VAL = 74;
  const max = Math.max(...rows.map(o=>Math.abs(o.V)));
  const iw = W - LBL - R - VAL*2, zero = LBL + VAL + iw/2, sc = v => (v/max)*(iw/2);
  let g = `<line x1="${zero}" x2="${zero}" y1="${T-4}" y2="${H-2}" stroke="var(--ink-3)"/>`;
  rows.forEach((o,i)=>{
    const yy = T + i*rh, bh = 14, w = Math.abs(sc(o.V)), pos = o.V>=0, x0 = pos? zero : zero - w;
    let lab = o.k+' · '+(o.label||''); const mxc = Math.floor(LBL/7.2); if (lab.length>mxc) lab = lab.slice(0,mxc-1)+'…';
    g += `<g class="ccrow" data-cc="${esc(o.k)}" style="cursor:pointer"><rect x="0" y="${yy}" width="${W}" height="${rh}" fill="transparent"/>`;
    g += `<text x="${LBL-8}" y="${yy+bh/2+5}" text-anchor="end" font-size="11.5" fill="var(--ink)">${esc(lab)}</text>`;
    g += `<rect x="${x0}" y="${yy+3}" width="${Math.max(1.5,w)}" height="${bh}" rx="3" fill="${pos?'var(--pos)':'var(--neg)'}"/>`;
    g += `<text x="${pos? zero+w+6 : zero-w-6}" y="${yy+bh/2+5}" text-anchor="${pos?'start':'end'}" font-size="11" fill="var(--ink-2)">${pos?'':'−'}${axisShort(Math.abs(o.V))}</text></g>`;
  });
  el.innerHTML = `<svg viewBox="0 0 ${W} ${H}" height="${H}" role="img" aria-label="Variação por CC">${g}</svg>`;
  el.querySelectorAll('.ccrow').forEach(r=>{
    const o = rows.find(x=>x.k===r.dataset.cc);
    r.addEventListener('mousemove', ev=>showTT(ev, `<div class="t">CC ${esc(o.k)} · ${esc(o.label)}</div>`+row('','Orçado',brl0(o.B))+row('','Projeção',brl0(o.CF))+row('','Realizado',brl0(o.R))+row('','A lançar',brl0(o.C))+row('', o.V>=0?'Sobra':'Acima do orçado', `<b style="color:${colorV(o.V)}">${brl0(Math.abs(o.V))}${o.B?' ('+pct(Math.abs(o.V)/o.B)+')':''}</b>`)));
    r.addEventListener('mouseleave', hideTT);
    r.addEventListener('click', ()=>{ S.cc = S.cc===o.k?'':o.k; hideTT(); buildFilters(); render(); });
  });
}

/* ---------- despesas de anos anteriores lançadas no ano ---------- */
// Regra: lançamento com competência no ano selecionado cuja descrição/origem cita só anos anteriores
// (ex.: "12.2025_...", "Trimestre 2025", "PENDÊNCIAS 2025"). Entra no realizado do ano (bate com o SAP), mas fica marcado.
function priorRef(l, Y){
  if (!l.comp || !l.comp.startsWith(Y)) return null;
  const d = l.desc || '', y = +Y;
  if (new RegExp('(^|[^0-9])'+Y+'([^0-9]|$)').test(d)) return null;   // cita o próprio ano: não é pendência
  if (/PEND/i.test(l.origem||'')) { const g = (l.origem.match(/20\d\d/)||[])[0]; if (g && +g < y) return { ref: g, motivo: 'Origem "'+l.origem+'"' }; }
  let mt = d.match(/(^|[^0-9])(0?[1-9]|1[0-2])[.\/](20\d\d)([^0-9]|$)/);
  if (mt && +mt[3] < y && +mt[3] >= y-3) return { ref: mt[2].padStart(2,'0')+'/'+mt[3], motivo: 'Descrição cita '+mt[2].padStart(2,'0')+'/'+mt[3] };
  mt = d.match(/(^|[^0-9])(20\d\d)([^0-9]|$)/);
  if (mt && +mt[2] < y && +mt[2] >= y-3) return { ref: mt[2], motivo: 'Descrição cita '+mt[2] };
  return null;
}
function priorData(D){
  const items = D.LY.map(l=>({l, p:priorRef(l, D.Y)})).filter(x=>x.p);
  const R = items.filter(x=>x.l.classif==='Realizado'), C = items.filter(x=>x.l.classif!=='Realizado');
  const byM = D.ymY.map(ym=> sum(R.filter(x=>x.l.comp===ym).map(x=>x.l.valor)));
  return { items, R, C, byM, totR: sum(R.map(x=>x.l.valor)), totC: sum(C.map(x=>x.l.valor)) };
}
function renderPriorStrip(sel, D){
  const P = IDX.prior = priorData(D), el = $(sel), prevY = +D.Y - 1;
  if (!P.items.length){ el.innerHTML = `<div class="kw"><div><div class="lbl">Despesas de ${prevY} (ou antes) lançadas em ${D.Y}</div><div class="muted small">Nenhuma encontrada para os filtros atuais.</div></div></div>`; el.classList.add('off'); return; }
  el.classList.remove('off');
  const top = P.R.slice().sort((a,b)=>b.l.valor-a.l.valor)[0];
  const mx = P.byM.indexOf(Math.max(...P.byM));
  el.innerHTML = `<div class="kw">
    <div class="kwmain"><div class="lbl">Despesas de ${prevY} (ou antes) lançadas em ${D.Y}</div>
      <div class="val">${short(P.totR)}</div>
      <div class="foot">${P.R.length} lançamento(s) · ${pct(D.t.R? P.totR/D.t.R : 0,1)} do realizado do ano${P.totC>=100?` · + ${short(P.totC)} ainda a lançar`:''}</div></div>
    <div class="kwside">
      <div><span class="muted small">Mês com mais pendências</span><br><b>${ymLabel(D.ymY[mx])}</b> · ${short(P.byM[mx])}</div>
      ${top?`<div><span class="muted small">Maior item</span><br><b>${esc(top.l.forn.split(' ').slice(0,3).join(' '))}</b> · ${short(top.l.valor)}</div>`:''}
      <div class="kwcta">Ver lista →</div>
    </div></div>
    <div class="small muted" style="margin-top:6px">Contam no realizado de ${D.Y} (é o mês em que foram lançadas no SAP), mas referem-se a serviços de anos anteriores. Por isso podem fazer um mês parecer acima do orçado.</div>`;
}
function openPriorModal(){
  const D = IDX.D, P = IDX.prior || priorData(D); if (!P.items.length) return;
  $('#mtitle').textContent = `Despesas de ${+D.Y-1} (ou antes) lançadas em ${D.Y}`;
  $('#msub').textContent = `${P.items.length} lançamento(s) · realizado ${brl(P.totR)}${P.C.length?' · a lançar '+brl(P.totC):''} · filtros do painel aplicados`;
  const byMes = {}, byCC = {};
  P.items.forEach(({l})=>{ byMes[l.comp]=(byMes[l.comp]||0)+l.valor; const k=l.cc+' · '+(IDX.ccDesc[l.cc]||''); byCC[k]=(byCC[k]||0)+l.valor; });
  const mini = (obj, lab) => `<table><thead><tr><th>${lab}</th><th class="n">Valor</th></tr></thead><tbody>${Object.entries(obj).sort((a,b)=>lab==='Mês lançado'?a[0].localeCompare(b[0]):b[1]-a[1]).map(([k,v])=>`<tr><td>${lab==='Mês lançado'?ymLabel(k):esc(k)}</td><td class="n">${brl0(v)}</td></tr>`).join('')}</tbody></table>`;
  let rows = P.items.slice().sort((a,b)=>b.l.valor-a.l.valor);
  const draw = q => {
    const qq = (q||'').toLowerCase();
    const r = rows.filter(({l})=>!qq || (l.forn+' '+l.desc+' '+l.pedido+' '+l.nf+' '+l.miro).toLowerCase().includes(qq));
    $('#mlist').innerHTML = `<table><thead><tr><th>Lançado em</th><th>Refere-se a</th><th>Status</th><th>Fornecedor</th><th>Descrição</th><th>CC</th><th>Filial</th><th>Pedido</th><th>MIRO</th><th>NF</th><th class="n">Valor</th></tr></thead><tbody>${
      r.map(({l,p})=>`<tr><td>${ymLabel(l.comp)}</td><td title="${esc(p.motivo)}"><span class="pill pend">${esc(p.ref)}</span></td><td><span class="pill ${l.classif==='Realizado'?'ok':'pend'}">${esc(l.status)}</span></td><td>${esc(l.forn)}</td><td class="muted">${esc(l.desc)}</td><td>${esc(l.cc)}</td><td>${esc(l.divDesc)}</td><td class="muted">${esc(l.pedido)}</td><td class="muted">${esc(l.miro)}</td><td class="muted">${esc(l.nf)}</td><td class="n">${brl(l.valor)}</td></tr>`).join('')
    }</tbody><tfoot><tr><td colspan="10">${r.length} item(ns)</td><td class="n">${brl(sum(r.map(x=>x.l.valor)))}</td></tr></tfoot></table>`;
  };
  $('#mbody').innerHTML = `<div class="grid two-eq"><div class="card"><h3 class="mh3">Por mês em que foi lançada</h3>${mini(byMes,'Mês lançado')}</div><div class="card"><h3 class="mh3">Por centro de custo</h3><div class="scroll" style="max-height:220px">${mini(byCC,'CC')}</div></div></div>
    <div class="card sec"><div class="card-h"><h3 class="mh3">Lista</h3><input type="search" id="mq" placeholder="Buscar fornecedor, descrição, pedido…" style="min-width:240px"></div>
    <div class="scroll" id="mlist" style="max-height:52vh"></div>
    <div class="small muted" style="margin-top:6px">"Refere-se a" = mês/ano citado na descrição ou na origem do lançamento (passe o mouse para ver o motivo). Regra automática: pode haver item que não foi identificado, se a descrição não citar o período.</div></div>`;
  draw(''); $('#mq').oninput = e=>draw(e.target.value);
  const md = $('#modal'); md.classList.add('on'); md.setAttribute('aria-hidden','false'); $('#mclose').focus();
}
function closeModal(){ const md=$('#modal'); md.classList.remove('on'); md.setAttribute('aria-hidden','true'); }

/* ---------- tabela acumulada (meses nas colunas) ---------- */
function accTable(sel, D, rows, opt={}){
  const lc = M.meta.lastClosed, n = D.ymY.length;
  const head = D.ymY.map(ym=>`<th class="n ${ym<=lc?'':'open'}">${MES[+ym.slice(5)-1]}${ym===IDX.curYM?'*':''}${ym<=lc?' ✓':''}</th>`).join('');
  let h = `<table class="acc"><thead><tr><th></th>${head}<th class="n tot">${opt.totLabel||'Ano'}</th></tr></thead><tbody>`;
  rows.forEach(r=>{
    if (r.sep) { h += `<tr class="sepr"><td colspan="${n+2}">${r.sep}</td></tr>`; return; }
    h += `<tr class="${r.cls||''}"><td>${r.l}${r.hint?` <span class="muted small">${r.hint}</span>`:''}</td>` + r.v.map((v,i)=>{
      if (v==null) return '<td class="n muted">—</td>';
      const txt = r.pct ? pct(v,0) : short(v);
      const st = r.sign ? `color:${v>=0?'var(--good-ink)':'var(--crit-ink)'}` : '';
      return `<td class="n ${D.ymY[i]<=lc?'':'open'}" style="${st}" title="${r.pct?pct(v,1):brl(v)}">${txt}</td>`;
    }).join('') + `<td class="n tot" ${r.sign&&r.tot!=null?`style="color:${r.tot>=0?'var(--good-ink)':'var(--crit-ink)'}"`:''}>${r.tot==null?'':(r.pct?pct(r.tot,0):short(r.tot))}</td></tr>`;
  });
  $(sel).innerHTML = h + '</tbody></table>' + (opt.after||'');
}
const cumul = a => { let s=0; return a.map(v=> v==null ? null : (s+=v)); };
function renderAccGeral(D){
  const P = D.mCF;                                   // fechado = realizado, aberto = projeção
  const sal = D.mB.map((b,i)=>b-P[i]);
  const aB = cumul(D.mB), aP = cumul(P), aS = aB.map((b,i)=>b-aP[i]);
  const use = aB.map((b,i)=> b ? aP[i]/b : null);
  accTable('#g-acc', D, [
    {sep:'No mês'},
    {l:'Orçado', v:D.mB, tot:D.t.B},
    {l:'Realizado / Projeção', v:P, tot:D.t.CF},
    {l:'↳ de anos anteriores', v:(IDX.prior||priorData(D)).byM.map(v=>v||null), tot:(IDX.prior||priorData(D)).totR, cls:'subr'},
    {l:'Saldo do mês', v:sal, sign:1, tot:D.t.B-D.t.CF},
    {sep:'Acumulado no ano'},
    {l:'Orçado acumulado', v:aB, tot:aB[aB.length-1]},
    {l:'Realizado / Projeção acumulado', v:aP, tot:aP[aP.length-1]},
    {l:'Saldo acumulado', v:aS, sign:1, cls:'key', tot:aS[aS.length-1]},
    {l:'% do orçado usado', v:use, pct:1, tot:use[use.length-1]}
  ], { after:`<div class="small muted" style="margin-top:6px">✓ = mês fechado · * = mês em andamento · colunas em cinza = projeção. Saldo positivo (verde) = abaixo do orçado; negativo (vermelho) = acima. Até ${ymLabel(D.t.closedN?D.ymY[D.t.closedN-1]:null)} o saldo acumulado realizado é <b>${short(D.t.closedN?aS[D.t.closedN-1]:0)}</b>.</div>` });
}
function renderAccDesp(D){
  const lc = M.meta.lastClosed;
  const fe = D.ymY.map(ym=> ym<=lc);                       // meses fechados
  const R = D.mR.map((v,i)=> fe[i] || D.ymY[i]===IDX.curYM ? v : null);   // mês em andamento aparece parcial
  const Bm = D.mB.map((v,i)=> fe[i]?v:null);
  const dif = D.mR.map((v,i)=> fe[i]? D.mB[i]-v : null);
  const aB = cumul(Bm), aR = cumul(D.mR.map((v,i)=> fe[i]?v:null)), aS = aB.map((b,i)=> b==null?null:b-aR[i]);
  const k = fe.lastIndexOf(true);
  accTable('#d-acc', D, [
    {sep:'No mês'},
    {l:'Orçado', v:Bm, tot:k>=0?aB[k]:0},
    {l:'Realizado', v:R, tot:k>=0?aR[k]:0},
    {l:'↳ de anos anteriores', v:(IDX.prior||priorData(D)).byM.map((v,i)=> (fe[i]||D.ymY[i]===IDX.curYM) && v ? v : null), tot:(IDX.prior||priorData(D)).byM.reduce((s,v,i)=>s+(fe[i]?v:0),0), cls:'subr'},
    {l:'Saldo do mês', v:dif, sign:1, tot:k>=0?aS[k]:0},
    {sep:'Acumulado no ano'},
    {l:'Orçado acumulado', v:aB, tot:k>=0?aB[k]:0},
    {l:'Realizado acumulado', v:aR, tot:k>=0?aR[k]:0},
    {l:'Saldo acumulado', v:aS, sign:1, cls:'key', tot:k>=0?aS[k]:0},
    {l:'% do orçado usado', v:aB.map((b,i)=> b? aR[i]/b : null), pct:1, tot:(k>=0&&aB[k])?aR[k]/aB[k]:null}
  ], { totLabel:'Até '+(k>=0?ymLabel(D.ymY[k]):'—'), after:`<div class="small muted" style="margin-top:6px">Saldo e acumulado consideram só meses fechados. O mês marcado com * está em andamento: mostra apenas o que já foi lançado até hoje (${short(D.mR[D.ymY.indexOf(IDX.curYM)]||0)}).</div>` });
}

/* ---------- tabela genérica ---------- */
function table(sel, cols, rows, opt={}){
  let h = `<table><thead><tr>${cols.map(c=>`<th class="${c.n?'n':''}">${c.l}</th>`).join('')}</tr></thead><tbody>`;
  rows.forEach((r,i)=>{ h += `<tr class="${opt.click?'click':''}" data-i="${i}">${cols.map(c=>`<td class="${c.n?'n':''}">${c.f(r,i)}</td>`).join('')}</tr>`; });
  h += `</tbody>${opt.foot?`<tfoot><tr>${opt.foot.map((v,i)=>`<td class="${cols[i]&&cols[i].n?'n':''}">${v}</td>`).join('')}</tr></tfoot>`:''}</table>${opt.after||''}`;
  const el = $(sel); el.innerHTML = h;
  if (opt.click) el.querySelectorAll('tbody tr').forEach(tr=>tr.onclick=()=>opt.click(rows[+tr.dataset.i]));
}
function miniBar(p){ const w = Math.min(100, Math.max(0,(p||0)*100)); return `<div class="mini"><span>${p==null?'—':pct(p,0)}</span><div class="bar"><i class="${p>1?'over':''}" style="width:${w}%"></i></div></div>`; }
const setCC = o => { S.cc = S.cc===o.k?'':o.k; buildFilters(); render(); };
const setTipo = o => { S.tipo = S.tipo===o.k?'':o.k; buildFilters(); render(); };
const ccCell = o => `<b>${esc(o.k)}</b> <span class="muted">${esc(o.label)}</span>`;
const vCell = v => `<span style="color:${colorV(v)}">${short(v)}</span>`;

function renderGrp(){
  const D0 = compute(true);
  const rows = D0.byGrp.sort((a,b)=>GROUPS.findIndex(g=>g.k===a.k)-GROUPS.findIndex(g=>g.k===b.k));
  const tot = sum(rows.map(r=>r.CF));
  const gc = k => (GROUPS.find(g=>g.k===k)||{}).c;
  table('#g-tbGrp', [
    {l:'Grupo', f:o=>`<span class="grp-dot" style="background:${gc(o.k)}"></span><b>${esc(o.label)}</b>${S.grps.includes(o.k)?' <span class="pill">selecionado</span>':''}`},
    {l:'Orçado', n:1, f:o=>short(o.B)}, {l:'Realizado', n:1, f:o=>short(o.R)}, {l:'A lançar', n:1, f:o=>short(o.C)},
    {l:'Projeção', n:1, f:o=>short(o.CF)}, {l:'Sobra', n:1, f:o=>vCell(o.V)}, {l:'% da projeção', n:1, f:o=>miniBar(tot?o.CF/tot:0)}
  ], rows, { click:o=>toggleGrp(o.k),
    foot:['Total', short(sum(rows.map(r=>r.B))), short(sum(rows.map(r=>r.R))), short(sum(rows.map(r=>r.C))), short(tot), short(sum(rows.map(r=>r.V))), ''],
    after:`<div class="small muted" style="margin-top:6px">Esta tabela ignora o filtro de grupo para permitir a comparação. Clique em um ou mais grupos para combinar (ex.: FC + RetailX).</div>` });
}

/* ---------- pontos de atenção ---------- */
function monthAdd(ym, k){ let y=+ym.slice(0,4), m=+ym.slice(5)-1+k; y+=Math.floor(m/12); m=((m%12)+12)%12; return y+'-'+String(m+1).padStart(2,'0'); }
function renderAlerts(D){
  const A = []; const t = D.t, lc = M.meta.lastClosed;
  const add = (sev, title, detail, value, what, sug) => A.push({sev,title,detail,value,what,sug});
  if (t.B>0){
    const p = (t.CF-t.B)/t.B;
    if (p>0.02) add('critical', `Projeção ${D.Y} acima do orçado`, `Projeção supera o orçado em ${pct(p)}.`, '+'+short(t.CF-t.B),
      'A projeção do ano (realizado até agora + previsto para os meses abertos) ficou maior que o orçamento aprovado.',
      'Use o gráfico “Quanto sobra ou passa do orçado, por CC” para achar quem está acima. Avalie remanejamento entre CCs com sobra (precisa de aprovação) ou renegociação/adiamento de contratos.');
    else if (p<-0.15) add('info', `Projeção ${D.Y} ${pct(-p,0)} abaixo do orçado`, 'Sobra grande: confirme se é economia real, atraso de contratação ou projeção desatualizado.', short(t.B-t.CF),
      'Sobra muito alta raramente é só economia: costuma ser projeto atrasado, contrato que não renovou ou projeção que não foi atualizado.',
      'Valide com os responsáveis dos CCs de maior sobra se o gasto foi cancelado ou só adiado. Se cancelado, o saldo pode ser liberado para remanejamento; se adiado, atualize a projeção dos próximos meses.');
    else if (p<=0) add('good', `Projeção ${D.Y} dentro do orçado`, `Abaixo do orçado em ${pct(-p)}.`, short(t.B-t.CF),
      'A projeção do ano está abaixo do orçamento.', 'Acompanhe os CCs que individualmente estão acima — o total dentro do orçado pode esconder CCs acima do orçado compensados por outros abaixo.');
  }
  const over = D.byCC.filter(o=>o.B>0 && o.CF-o.B>1000).sort((a,b)=>(b.CF-b.B)-(a.CF-a.B));
  over.slice(0,3).forEach(o=> add((o.CF-o.B)/o.B>0.1?'critical':'serious', `CC ${o.k} ${o.label} acima do orçado`, `Projeção ${pct((o.CF-o.B)/o.B)} acima do orçado (${short(o.B)} → ${short(o.CF)}).`, '+'+short(o.CF-o.B),
    `A projeção do CC ${o.k} passou do valor orçado para o ano.`,
    'Filtre o CC (clique na barra do gráfico) e veja na aba Despesas quais fornecedores cresceram. Antes de pedir remanejamento, confira se há gasto lançado no CC errado ou fora do escopo previsto.'));
  const tipoOver = D.byTipo.filter(o=>o.B>1000 && (o.R+o.C)/o.B>1.1).sort((a,b)=>(b.R+b.C-b.B)-(a.R+a.C-a.B));
  if (tipoOver.length) add('serious', `${tipoOver.length} tipo(s) de despesa já consumiram mais que o orçado`, tipoOver.slice(0,3).map(o=>`${o.k} (${pct((o.R+o.C)/o.B,0)})`).join(' · '), '+'+short(sum(tipoOver.map(o=>o.R+o.C-o.B))),
    'Realizado + a lançar do tipo de despesa já passaram do orçado anual — antes do fim do ano.',
    'Normalmente é classificação de conta diferente da usada no orçamento (ex.: gasto orçado como Consultoria e lançado como Pessoal). Compare conta contábil do lançamento com a do orçamento; se o gasto é novo, formalize o remanejamento.');
  const semB = D.byCC.filter(o=>Math.abs(o.B)<1 && (o.R+o.C)>1000).sort((a,b)=>(b.R+b.C)-(a.R+a.C));
  if (semB.length) add('serious', `${semB.length} CC(s) com gasto sem orçado`, semB.slice(0,4).map(o=>`${o.k} ${o.label} (${short(o.R+o.C)})`).join(' · ') + (semB.length>4?' …':''), short(sum(semB.map(o=>o.R+o.C))),
    'Existe realizado ou item a lançar em centros de custo que não têm nenhuma linha no orçamento. Ou é gasto não planejado, ou está classificado no CC errado.',
    'Confira se o lançamento deveria estar em outro CC. Se o gasto é legítimo e recorrente, inclua uma linha no Orçamento Projeção para ele parar de aparecer como acima do orçado.');
  const fut = M.lanc.filter(r=>pass(r)).filter(l=>l.classif==='Realizado' && l.comp && l.comp>IDX.curYM);
  if (fut.length) add('critical', `${fut.length} lançamento(s) LANÇADO em mês que ainda não começou`, fut.slice(0,3).map(l=>`${l.forn.split(' ').slice(0,2).join(' ')} (${ymLabel(l.comp)})`).join(', '), short(sum(fut.map(l=>l.valor))),
    'A competência é o mês em que a nota foi lançada no sistema. Um item LANÇADO não pode estar num mês futuro: a competência foi digitada errada.',
    'Confira a data de lançamento da MIRO no SAP (MIR5/FB03) e corrija a coluna Competência na aba Lançamentos.');
  const pend = D.LY.filter(l=>l.classif!=='Realizado' && l.comp<=lc);
  if (pend.length) add('warning', `${pend.length} item(ns) a lançar em meses já fechados`, `Status ainda não LANÇADO com competência ≤ ${ymLabel(lc)}: ` + Object.entries(pend.reduce((a,l)=>(a[l.status]=(a[l.status]||0)+1,a),{})).map(([k,v])=>`${k} ${v}`).join(' · '), short(sum(pend.map(l=>l.valor))),
    'Itens previstos para meses que já fecharam e que ainda não foram lançados no SAP. Como a competência é o mês do lançamento, quando forem lançados eles vão para o mês atual.',
    'Lance esses itens ou passe a competência prevista para o mês atual (lista na aba A lançar). Se algum não vai mais acontecer, retire da base para não inflar a projeção.');
  const today = new Date().toISOString().slice(0,10), lim = new Date(Date.now()+15*864e5).toISOString().slice(0,10);
  const venc = M.lanc.filter(r=>pass(r)).filter(l=>l.classif!=='Realizado' && l.venc && l.venc<=lim);
  const vencAtras = venc.filter(l=>l.venc<today);
  if (vencAtras.length) add('critical', `${vencAtras.length} item(ns) não lançado(s) com vencimento já passado`, 'Ex.: ' + vencAtras.slice(0,2).map(l=>`${l.forn.split(' ').slice(0,2).join(' ')} (${dBR(l.venc)})`).join(', '), short(sum(vencAtras.map(l=>l.valor))),
    'A lançar com data de vencimento já passada e sem lançamento: risco de multa, juros ou suspensão do serviço.',
    'Verifique se já foram pagas por outro caminho (e então baixe a item a lançar). Se não, cobre a NF/fatura do fornecedor e regularize folha/MIRO com urgência.');
  const vencProx = venc.filter(l=>l.venc>=today);
  if (vencProx.length) add('warning', `${vencProx.length} vencimento(s) nos próximos 15 dias ainda não lançado(s)`, 'Itens a lançar que precisam de folha/MIRO para pagar em dia.', short(sum(vencProx.map(l=>l.valor))),
    'Pagamentos que vencem nas próximas duas semanas e ainda dependem de etapa no SAP.',
    'Garanta pedido liberado e folha criada alguns dias antes do vencimento. Ordene a lista da aba A lançar por vencimento.');
  const mv = IDX.mov;
  if (mv && mv.avg>0){ const p = (mv.cur-mv.avg)/mv.avg; if (Math.abs(p)>0.2) add(p>0?'serious':'info', `${ymLabel(lc)} ${p>0?'acima':'abaixo'} da média recente`, `Total de ${short(mv.cur)} contra média de ${short(mv.avg)} nos 3 meses anteriores (${p>0?'+':''}${pct(p)}).`, short(mv.cur-mv.avg),
    'O total do último mês fechado ficou mais de 20% distante da média dos três meses anteriores.',
    'Veja a seção Movimentação: pode ser fatura anual, cobrança retroativa ou acúmulo de competências. Separe o pontual do recorrente antes de concluir que houve aumento de custo.'); }
  if (mv && mv.missing.length) add('warning', `${mv.missing.length} fornecedor(es) recorrente(s) sem lançamento em ${ymLabel(lc)}`, mv.missing.slice(0,3).map(x=>x.forn.split(' ').slice(0,3).join(' ')).join(', ') + (mv.missing.length>3?' …':''), '≈ '+short(sum(mv.missing.map(x=>x.avg))),
    'Fornecedores que lançaram nos três meses anteriores não têm nenhum lançamento no último mês.',
    'Confirme com o fornecedor se a fatura foi emitida (pode estar parada no e-mail ou no fiscal). Se o contrato foi encerrado, ajuste a projeção.');
  const dmap = {}; M.lanc.filter(r=>pass(r)).forEach(l=>{ if(l.nf && !/REEMBOLSO/i.test(l.origem)){ const k='nf|'+[l.forn,l.nf,l.valor,l.comp,l.cc,l.div,l.desc].join('|'); (dmap[k]=dmap[k]||[]).push(l); } if(l.miro && l.classif==='Realizado' && !/REEMBOLSO/i.test(l.origem)){ const k2='miro|'+[l.miro,l.valor,l.cc,l.div,l.conta].join('|'); (dmap[k2]=dmap[k2]||[]).push(l); } });
  const seenD = new Set();
  const dups = Object.values(dmap).filter(a=>a.length>1).filter(a=>{ const k=a.map(l=>l.id).sort().join(','); if(seenD.has(k)) return false; seenD.add(k); return true; });
  if (dups.length) add('serious', `${dups.length} possível(is) duplicidade(s)`, dups.slice(0,2).map(a=>`${a[0].forn.split(' ').slice(0,2).join(' ')} ${a[0].miro?'MIRO '+a[0].miro:'NF '+a[0].nf} (linhas ID ${a.map(l=>l.id).join(' e ')})`).join(', '), short(sum(dups.map(a=>a[0].valor*(a.length-1)))),
    'Lançamentos repetidos: mesma MIRO com o mesmo valor, CC e conta em mais de uma linha (rateio entre CCs diferentes não entra), ou mesmo fornecedor, NF, valor, mês, CC e descrição.',
    'Confira no SAP se são duas MIROs da mesma NF. Se for rateio legítimo, diferencie a descrição ou o CC na planilha para não cair neste alerta.');
  const ver = D.LY.filter(l=>/verificar/i.test(l.ccDesc+' '+l.contaDesc));
  if (ver.length) add('info', `${ver.length} lançamento(s) com conta “(verificar)”`, 'Classificação pendente na aba Parâmetros.', short(sum(ver.map(l=>l.valor))),
    'A conta contábil desses lançamentos ainda está marcada como “(verificar)” na aba Parâmetros, ou seja, o tipo de despesa pode estar errado.',
    'Atualize a descrição e o Tipo de Lançamento da conta em Parâmetros; a leitura por tipo melhora na próxima carga.');
  if (t.closedN>=3){
    const idle = D.byKey.filter(o=>o.Bytd>5000 && o.Rytd<=0.5);
    if (idle.length) add('info', `${idle.length} linha(s) orçada(s) sem nenhum realizado no ano`, 'Combinação filial | CC | conta com orçado até o mês fechado e zero realizado.', short(sum(idle.map(o=>o.Bytd))),
      'Há orçamento previsto até o mês fechado em combinações filial/CC/conta que não tiveram nenhum gasto.',
      'Candidatas a sobra real ou remanejamento. Valide se o serviço ainda será contratado no ano; se não, registre a sobra para o próximo orçamento.');
  }
  if (IDX.years.includes(D.nextY)){
    const ix = M.meta.months.map((m,i)=>m.startsWith(D.nextY)?i:-1).filter(i=>i>=0);
    const bN = sum(D.B.map(b=>sum(ix.map(i=>b.m[i]))));
    if (t.CF>0 && bN>0){ const p=(bN-t.CF)/t.CF; if(Math.abs(p)>0.1) add(p>0?'warning':'info', `Orçado ${D.nextY} ${p>0?'+':''}${pct(p,0)} vs projeção ${D.Y}`, `${short(t.CF)} → ${short(bN)}.`, short(bN),
      `O orçamento de ${D.nextY} está bem ${p>0?'acima':'abaixo'} do que se projeta gastar em ${D.Y}.`,
      p<0 ? `Pode faltar contrato recorrente no orçamento de ${D.nextY}. Compare os fornecedores da projeção atual com as linhas do próximo ano (aba Orçamento).` : 'Garanta que o crescimento está justificado (reajustes, novos projetos) antes da aprovação.'); }
  }
  A.forEach(a=>{ const t = a.title; const m = t.match(/^CC (\S+) .* acima do orçado/);
    if (m) a.go = ()=>{ S.cc = m[1]; buildFilters(); render(); };
    else if (/a lançar em meses|vencimento|não lançado/.test(t)) a.go = ()=>goView('prov');
    else if (/^Projeção|^Orçado \d{4}/.test(t)) a.go = ()=>goView('fcst');
    else if (/tipo\(s\) de despesa|acima da média|abaixo da média/.test(t)) a.go = ()=>goView('desp');
    else if (/sem orçado|orçada\(s\)/.test(t)) a.go = ()=>goView('orc');
    else if (/fornecedor/.test(t)) a.go = ()=>goView('forn');
    else if (/duplicidade|LANÇADO em mês|verificar/.test(t)) a.go = ()=>goView('lanc'); });
  const order = {critical:0, serious:1, warning:2, info:3, good:4};
  A.sort((a,b)=>order[a.sev]-order[b.sev]);
  const icon = {critical:'!', serious:'▲', warning:'!', info:'i', good:'✓'};
  const lbl = {critical:'Crítico', serious:'Alto', warning:'Atenção', info:'Info', good:'OK'};
  $('#alerts').innerHTML = A.length ? A.map((a,i)=>`<li class="${a.go?'go':''}" data-li="${i}"><span class="ic ${a.sev}" tabindex="0" data-i="${i}" aria-label="${esc(a.what+' Sugestão: '+a.sug)}">${icon[a.sev]}</span><div><div class="tl"><span class="sevtag">${lbl[a.sev]}</span>${esc(a.title)}</div><div class="dt">${esc(a.detail)}</div></div><span class="v">${esc(a.value||'')}</span></li>`).join('') : '<li><span class="ic good">✓</span><div class="tl">Nada relevante para os filtros atuais.</div><span></span></li>';
  const tipHTML = a => `<div class="t">${esc(a.title)}</div><div class="h">O que é</div><p>${esc(a.what)}</p><div class="h">Sugestão</div><p>${esc(a.sug)}</p>`;
  $$('#alerts .ic[data-i]').forEach(ic=>{
    const a = A[+ic.dataset.i];
    ic.addEventListener('mouseenter', ev=>showTT(ev, tipHTML(a), true));
    ic.addEventListener('mousemove', ev=>showTT(ev, tipHTML(a), true));
    ic.addEventListener('mouseleave', hideTT);
    ic.addEventListener('focus', ()=>{ const r = ic.getBoundingClientRect(); showTT({pageX:r.right+window.scrollX, pageY:r.top+window.scrollY}, tipHTML(a), true); });
    ic.addEventListener('blur', hideTT);
  });
  $$('#alerts li[data-li]').forEach(li=>{ const a = A[+li.dataset.li]; if (a && a.go) li.onclick = e=>{ if (e.target.closest('.ic')) return; hideTT(); a.go(); }; });
}

/* ---------- movimentação ---------- */
function computeMov(){
  const lc = M.meta.lastClosed, prev = [1,2,3].map(k=>monthAdd(lc,-k));
  const L = M.lanc.filter(r=>pass(r));
  const by = {}; L.forEach(l=>{ if(!l.comp) return; const f = l.forn||'(sem fornecedor)'; const o = by[f] || (by[f] = {forn:f, cur:0, p:{}, first:l.comp}); if (l.comp<o.first) o.first=l.comp; if (l.comp===lc) o.cur+=l.valor; else if (prev.includes(l.comp)) o.p[l.comp]=(o.p[l.comp]||0)+l.valor; });
  const arr = Object.values(by).map(o=>{ o.avg = sum(prev.map(m=>o.p[m]||0))/3; o.d = o.cur-o.avg; o.nprev = prev.filter(m=>o.p[m]).length; return o; });
  return { lc, cur: sum(L.filter(l=>l.comp===lc).map(l=>l.valor)), avg: sum(prev.map(m=>sum(L.filter(l=>l.comp===m).map(l=>l.valor))))/3,
    up: arr.filter(o=>o.d>0 && o.nprev>0 && o.cur>0).sort((a,b)=>b.d-a.d).slice(0,6),
    down: arr.filter(o=>o.d<0 && o.cur>0).sort((a,b)=>a.d-b.d).slice(0,6),
    novos: arr.filter(o=>o.first===lc && o.cur>0).sort((a,b)=>b.cur-a.cur),
    missing: arr.filter(o=>o.nprev===3 && o.cur===0).sort((a,b)=>b.avg-a.avg),
    nForn: arr.filter(o=>o.cur>0).length, nLanc: L.filter(l=>l.comp===lc).length };
}
function renderMov(p){
  const mv = IDX.mov, d = mv.avg? (mv.cur-mv.avg)/mv.avg : null;
  $('#'+p+'-movStats').innerHTML = `<span>Total do mês <b>${short(mv.cur)}</b></span><span>Média 3 meses anteriores <b>${short(mv.avg)}</b></span><span>Variação <b class="delta ${d>0.005?'up':d<-0.005?'down':'flat'}">${d==null?'—':(d>0?'▲ +':'▼ ')+pct(d)}</b></span><span>Lançamentos <b>${mv.nLanc}</b></span><span>Fornecedores <b>${mv.nForn}</b></span>`;
  const list = (arr, fmt) => arr.length ? `<ol>${arr.map(o=>`<li title="${esc(o.forn)}"><span>${esc(o.forn)}</span><span>${fmt(o)}</span></li>`).join('')}</ol>` : '<div class="empty">Nenhum.</div>';
  $('#'+p+'-mov').innerHTML =
    `<div><h3>▲ Maiores altas vs média</h3>${list(mv.up, o=>`<span class="delta up">+${short(o.d)}</span>`)}</div>`+
    `<div><h3>▼ Maiores quedas vs média</h3>${list(mv.down, o=>`<span class="delta down">${short(o.d)}</span>`)}</div>`+
    `<div><h3>✚ Fornecedores novos no mês</h3>${list(mv.novos.slice(0,6), o=>short(o.cur))}</div>`+
    `<div><h3>? Fornecedores mensais que não lançaram no mês</h3>${list(mv.missing.slice(0,6), o=>'média '+short(o.avg))}</div>`;
}

/* ---------- ORÇAMENTO ---------- */
function renderOrc(D){
  const t = D.t, lc = M.meta.lastClosed;
  const yrs = IDX.years.map(y=>{ const ix = M.meta.months.map((m,i)=>m.startsWith(y)?i:-1).filter(i=>i>=0); return {y, v: sum(D.B.map(b=>sum(ix.map(i=>b.m[i]))))}; });
  const nxt = yrs.find(o=>o.y===D.nextY);
  const lines = D.B.map(b=>({b, Y: sum(D.yIx.map(i=>b.m[i])), ytd: sum(D.yIx.slice(0,t.closedN).map(i=>b.m[i]))})).filter(o=>Math.abs(o.Y)>0.5);
  const top = lines.slice().sort((a,b)=>b.Y-a.Y)[0];
  const cons = t.B? (t.R+t.C)/t.B : null;
  $('#o-kpis').innerHTML = [
    kpi(`Orçado ${D.Y}`, short(t.B), `${lines.length} linhas com valor no ano`, '', brl(t.B)),
    kpi(`Orçado até ${ymLabel(t.closedN?D.ymY[t.closedN-1]:null)}`, t.closedN?short(t.Bytd):'—', t.closedN?`${pct(t.B?t.Bytd/t.B:0,0)} do ano`:'nenhum mês fechado', '', brl(t.Bytd)),
    kpi('Orçado restante', short(t.B-t.Bytd), `${12-t.closedN} mês(es) em aberto`, '', brl(t.B-t.Bytd)),
    kpi('% do orçado já usado', pct(cons,0), `realizado + a lançar ÷ orçado`, meter(t.B?t.R/t.B:0, t.B?t.C/t.B:0)),
    nxt ? kpi(`Orçado ${D.nextY}`, short(nxt.v), t.B?deltaSpan(nxt.v-t.B, t.B)+` vs ${D.Y}`:'', '', brl(nxt.v)) : kpi('Próximo ano', '—', 'fora do horizonte'),
    top ? kpi('Maior linha do ano', short(top.Y), esc((top.b.forn||top.b.desc).slice(0,38)), '', `${top.b.forn} — ${top.b.desc}`) : kpi('Maior linha','—','')
  ].join('');
  barLine('#o-cYears', { labels: yrs.map(o=>o.y), bars: yrs.map(o=>o.v), color: i=> yrs[i].y===D.Y?'var(--real)':'var(--fcst)', valueLabels:true, maxBar:70, aria:'Orçado por ano',
    tip: i=>{ const o=yrs[i], p=yrs[i-1]; return `<div class="t">Orçado ${o.y}</div>`+row('','Valor',brl0(o.v))+(p&&p.v?row('','vs ano anterior',pct((o.v-p.v)/p.v)):'')+row('',`Acumulado ${yrs[0].y}–${o.y}`,brl0(sum(yrs.slice(0,i+1).map(x=>x.v)))); } });
  $('#o-tMonthly').textContent = `Orçado mensal — ${D.Y}`;
  const mx = D.mB.indexOf(Math.max(...D.mB));
  $('#o-hMonthly').textContent = `pico em ${ymLabel(D.ymY[mx])} (${short(D.mB[mx])})`;
  barLine('#o-cMonthly', { labels: D.ymY.map(m=>MES[+m.slice(5)-1]), bars: D.mB, color: i=> D.ymY[i]<=lc?'var(--real)':'var(--fcst)', split: t.closedN, splitL:'← fechado', splitR:'aberto →', aria:'Orçado mensal',
    tip: i=> `<div class="t">${ymLabel(D.ymY[i])}</div>`+row('','Orçado',brl0(D.mB[i]))+row('','Orçado acumulado no ano',brl0(sum(D.mB.slice(0,i+1))))+row('','Realizado',brl0(D.mR[i]))+row('','A lançar',brl0(D.mC[i])) });
  const nl = `Orçado ${D.nextY}`;
  const ccR = D.byCC.filter(o=>o.B||o.Bn).sort((a,b)=>b.B-a.B);
  table('#o-tbCC', [
    {l:'CC', f:ccCell}, {l:`Orçado ${D.Y}`, n:1, f:o=>short(o.B)}, {l:'% usado', n:1, f:o=>miniBar(o.B?(o.R+o.C)/o.B:null)},
    {l:nl, n:1, f:o=>short(o.Bn)}, {l:'Δ%', n:1, f:o=>o.B?`<span style="color:${o.Bn>o.B?'var(--crit-ink)':'var(--ink-2)'}">${pct((o.Bn-o.B)/o.B,0)}</span>`:(o.Bn?'<span class="pill">novo</span>':'—')}
  ], ccR, { click:setCC, foot:['Total', short(sum(ccR.map(o=>o.B))), miniBar(t.B?(t.R+t.C)/t.B:null), short(sum(ccR.map(o=>o.Bn))), ''] });
  const tpR = D.byTipo.filter(o=>o.B||o.Bn).sort((a,b)=>b.B-a.B);
  table('#o-tbTipo', [
    {l:'Tipo', f:o=>esc(o.k)}, {l:`Orçado ${D.Y}`, n:1, f:o=>short(o.B)}, {l:'% usado', n:1, f:o=>miniBar(o.B?(o.R+o.C)/o.B:null)},
    {l:nl, n:1, f:o=>short(o.Bn)}, {l:'Δ%', n:1, f:o=>o.B?pct((o.Bn-o.B)/o.B,0):'—'}
  ], tpR, { click:setTipo });
  renderOrcLines(D, lines);
}
function renderOrcLines(D, lines){
  lines = lines || IDX.orcLines; IDX.orcLines = lines;
  const q = S.oq.trim().toLowerCase();
  let rows = lines.filter(o=>!q || (o.b.forn+' '+o.b.desc+' '+o.b.ccDesc+' '+o.b.contaDesc+' '+o.b.id).toLowerCase().includes(q)).sort((a,b)=>b.Y-a.Y);
  const shown = rows.slice(0, S.oLimit);
  table('#o-tbLines', [
    {l:'ID', f:o=>`<span class="muted">${esc(o.b.id)}</span>`}, {l:'Origem', f:o=>`<span class="pill">${esc(o.b.origem)}</span>`},
    {l:'Filial', f:o=>esc(IDX.divDesc[o.b.div]||o.b.div)}, {l:'CC', f:o=>esc(o.b.cc)}, {l:'Tipo', f:o=>`<span class="muted">${esc(o.b.tipo)}</span>`},
    {l:'Fornecedor', f:o=>esc(o.b.forn)}, {l:'Descrição', f:o=>`<span class="muted">${esc(o.b.desc)}</span>`},
    {l:`Ano ${D.Y}`, n:1, f:o=>brl0(o.Y)}, {l:'Até mês fechado', n:1, f:o=>brl0(o.ytd)}, {l:'Restante', n:1, f:o=>brl0(o.Y-o.ytd)}
  ], shown);
  $('#o-info').innerHTML = `${rows.length} linha(s) · total ${brl0(sum(rows.map(o=>o.Y)))}` + (rows.length>S.oLimit?` · mostrando ${S.oLimit} <button id="o-more">mostrar mais</button>`:'');
  const mb = $('#o-more'); if (mb) mb.onclick = ()=>{ S.oLimit += 300; renderOrcLines(D); };
}

/* ---------- DESPESAS ---------- */
function renderDesp(D){
  const t = D.t, lc = M.meta.lastClosed, mv = IDX.mov;
  const R = D.LY.filter(l=>l.classif==='Realizado');
  const fm = {}; R.forEach(l=>{ fm[l.forn]=(fm[l.forn]||0)+l.valor; });
  const fr = Object.entries(fm).sort((a,b)=>b[1]-a[1]);
  const top10 = t.R? sum(fr.slice(0,10).map(x=>x[1]))/t.R : null;
  const avgM = t.closedN? t.Rytd/t.closedN : 0;
  const lastR = t.closedN? D.mR[t.closedN-1] : 0;
  const dLast = avgM? (lastR-avgM)/avgM : null;
  $('#d-kpis').innerHTML = [
    kpi(`Realizado ${D.Y}`, short(t.R), `${R.length} lançamentos`, '', brl(t.R)),
    kpi('vs orçado do período', t.Bytd?pct(t.Rytd/t.Bytd,0):'—', t.closedN?`${short(t.Rytd)} de ${short(t.Bytd)} até ${ymLabel(D.ymY[t.closedN-1])}`:'sem meses fechados', meter(t.Bytd?t.Rytd/t.Bytd:0)),
    kpi('Média mensal', short(avgM), t.closedN?`${t.closedN} mês(es) fechado(s)`:'—', '', brl(avgM)),
    kpi(`Realizado ${t.closedN?ymLabel(D.ymY[t.closedN-1]):'último mês'}`, short(lastR), (dLast==null?'—':`<span class="delta ${dLast>0?'up':'down'}">${dLast>0?'▲ +':'▼ '}${pct(dLast)}</span> vs média do ano`) + (t.closedN&&D.mC[t.closedN-1]>0?`<br>+ ${short(D.mC[t.closedN-1])} ainda em item a lançar no mês`:''), '', brl(lastR)),
    kpi('Fornecedores com gasto', String(fr.length), fr[0]?`maior: ${esc(fr[0][0].split(' ').slice(0,3).join(' '))}`:'', '', fr[0]?`${fr[0][0]} — ${brl(fr[0][1])}`:''),
    kpi('Peso dos 10 maiores fornecedores', pct(top10,0), 'do realizado nos 10 maiores fornecedores', meter(top10))
  ].join('');
  $('#d-tMonthly').textContent = `Realizado mês a mês — ${D.Y}`;
  $('#d-hMonthly').textContent = t.closedN<12 && t.closedN>0 ? `meses após ${ymLabel(lc)} mostram só o que já foi lançado` : '';
  barLine('#d-cMonthly', { labels: D.ymY.map(m=>MES[+m.slice(5)-1]), bars: D.mR, color:'var(--real)', line: D.mB, split: t.closedN, splitR:'aberto →', aria:'Realizado mensal', click: i=>goLanc({mes:D.ymY[i]}),
    tip: i=>{ const v = D.mR[i]-D.mB[i]; return `<div class="t">${ymLabel(D.ymY[i])}</div>`+row(swc('var(--real)'),'Realizado',brl0(D.mR[i]))+priorTip(D,i)+row(swl('var(--base)'),'Orçado',brl0(D.mB[i]))+row('','Saldo do mês',`<b style="color:${colorV(-v)}">${brl0(-v)}</b>`)+(()=>{ const aB=sum(D.mB.slice(0,i+1)), aR=sum(D.mR.slice(0,i+1)); return `<div class="h" style="margin-top:6px">Acumulado até ${ymLabel(D.ymY[i])}</div>`+row('','Orçado',brl0(aB))+row('','Realizado',brl0(aR))+row('','Saldo acumulado',`<b style="color:${colorV(aB-aR)}">${brl0(aB-aR)}</b>`); })(); } });
  const tp = D.byTipo.filter(o=>o.R||o.Bytd).sort((a,b)=>b.R-a.R);
  table('#d-tbTipo', [
    {l:'Tipo', f:o=>esc(o.k)}, {l:'Realizado', n:1, f:o=>short(o.R)}, {l:'Orçado até o mês', n:1, f:o=>short(o.Bytd)}, {l:'% usado', n:1, f:o=>miniBar(o.Bytd?o.Rytd/o.Bytd:null)}
  ], tp, { click:setTipo });
  renderMov('d');
  const cc = D.byCC.filter(o=>o.R||o.Bytd).sort((a,b)=>b.R-a.R);
  table('#d-tbCC', [
    {l:'CC', f:ccCell}, {l:'Realizado', n:1, f:o=>short(o.R)}, {l:'Orçado até o mês', n:1, f:o=>short(o.Bytd)}, {l:'Diferença', n:1, f:o=>vCell(o.Bytd-o.Rytd)}, {l:'% usado', n:1, f:o=>o.Bytd?miniBar(o.Rytd/o.Bytd):'<span class="pill late">sem orç.</span>'}
  ], cc, { click:setCC, foot:['Total', short(t.R), short(t.Bytd), short(t.Bytd-t.Rytd), miniBar(t.Bytd?t.Rytd/t.Bytd:null)] });
  let acc = 0;
  table('#d-tbForn', [
    {l:'#', f:(o,i)=>`<span class="muted">${i+1}</span>`}, {l:'Fornecedor', f:o=>esc(o[0])}, {l:'Realizado', n:1, f:o=>short(o[1])}, {l:'% acum.', n:1, f:o=>{ acc+=o[1]; return `<span class="muted">${pct(t.R?acc/t.R:0,0)}</span>`; }}
  ], fr.slice(0,40), { click:o=>openFornByName(o[0]) });
}

/* ---------- PROVISÕES ---------- */
function provBase(D){ const lc = M.meta.lastClosed; return D.L.filter(l=>l.classif!=='Realizado' && (!l.comp || l.comp.startsWith(D.Y) || l.comp<=lc)); }
function renderProv(D){
  const lc = M.meta.lastClosed, P = provBase(D);
  const today = new Date().toISOString().slice(0,10), lim = new Date(Date.now()+15*864e5).toISOString().slice(0,10);
  const late = P.filter(l=>l.comp && l.comp<=lc), venc = P.filter(l=>l.venc && l.venc<today), prox = P.filter(l=>l.venc && l.venc>=today && l.venc<=lim), semV = P.filter(l=>!l.venc);
  const tot = sum(P.map(l=>l.valor));
  $('#p-kpis').innerHTML = [
    kpi('Total a lançar', short(tot), `${P.length} itens ainda não lançados`, '', brl(tot)),
    kpi('Em meses já fechados', `<span style="color:${late.length?'var(--crit-ink)':'inherit'}">${short(sum(late.map(l=>l.valor)))}</span>`, `${late.length} itens com competência ≤ ${ymLabel(lc)}`, '', brl(sum(late.map(l=>l.valor)))),
    kpi('Vencidos', `<span style="color:${venc.length?'var(--crit-ink)':'inherit'}">${short(sum(venc.map(l=>l.valor)))}</span>`, `${venc.length} itens com vencimento passado`, '', brl(sum(venc.map(l=>l.valor)))),
    kpi('Vencem em 15 dias', short(sum(prox.map(l=>l.valor))), `${prox.length} itens até ${dBR(lim)}`, '', brl(sum(prox.map(l=>l.valor)))),
    kpi('Sem vencimento informado', String(semV.length), short(sum(semV.map(l=>l.valor))), '', 'Preencha o vencimento para controlar o fluxo de pagamento'),
    kpi('Peso na projeção do ano', pct(D.t.CF?D.t.C/D.t.CF:null,0), 'a lançar do ano ÷ projeção', meter(D.t.CF?D.t.C/D.t.CF:0))
  ].join('');
  // pipeline
  const order = ['CRIAR PEDIDO','LIBERAR PEDIDO','ENVIADO','CRIAR FOLHA','A LANÇAR'];
  const g = {}; P.forEach(l=>{ const o = g[l.status] || (g[l.status]={s:l.status,n:0,v:0,late:0,lv:0}); o.n++; o.v+=l.valor; if(l.comp && l.comp<=lc){ o.late++; o.lv+=l.valor; } });
  const oi = s => { const i = order.indexOf(s); return i<0?99:i; };
  const rows = Object.values(g).sort((a,b)=>oi(a.s)-oi(b.s));
  if (!rows.length) $('#p-pipe').innerHTML = '<div class="empty">Nada a lançar.</div>';
  else table('#p-pipe', [
    {l:'Etapa', f:r=>`<span class="pill pend">${esc(r.s)}</span>`}, {l:'Itens', n:1, f:r=>r.n}, {l:'Valor', n:1, f:r=>short(r.v)},
    {l:'Participação', n:1, f:r=>miniBar(tot?r.v/tot:0)}, {l:'Em mês fechado', n:1, f:r=>r.late?`<span class="pill late">${r.late} · ${short(r.lv)}</span>`:'—'}
  ], rows, { click:r=>{ S.pst = S.pst===r.s?'':r.s; buildFilters(); renderProvList(D); }, foot:['Total', P.length, short(tot), '', short(sum(rows.map(r=>r.lv)))],
    after:'<div class="small muted" style="margin-top:8px">Clique numa etapa para filtrar a lista abaixo.</div>' });
  // aging por competência
  const comps = [...new Set(P.map(l=>l.comp).filter(Boolean))].sort();
  const vals = comps.map(c=>sum(P.filter(l=>l.comp===c).map(l=>l.valor)));
  if (comps.length) barLine('#p-cAging', { labels: comps.map(ymLabel), bars: vals, color: i=> comps[i]<=lc?'var(--neg)':'var(--fcst)', aria:'A lançar por mês previsto', h:240,
    tip: i=> `<div class="t">Competência ${ymLabel(comps[i])}</div>`+row('','Valor',brl0(vals[i]))+row('','Itens',P.filter(l=>l.comp===comps[i]).length)+row('','Situação', comps[i]<=lc?'<b style="color:var(--crit-ink)">mês fechado</b>':'futuro') });
  else $('#p-cAging').innerHTML = '<div class="empty">Nada a lançar.</div>';
  const fm = {}; P.forEach(l=>{ const o = fm[l.forn] || (fm[l.forn]={f:l.forn||'(sem fornecedor)',v:0,n:0,late:0}); o.v+=l.valor; o.n++; if(l.comp && l.comp<=lc) o.late+=l.valor; });
  table('#p-tbForn', [
    {l:'Fornecedor', f:o=>esc(o.f)}, {l:'Itens', n:1, f:o=>o.n}, {l:'Valor', n:1, f:o=>short(o.v)}, {l:'Em mês fechado', n:1, f:o=>o.late?`<span class="pill late">${short(o.late)}</span>`:'—'}
  ], Object.values(fm).sort((a,b)=>b.v-a.v).slice(0,40), { click:o=>{ S.pq=o.f; $('#p-q').value=o.f; renderProvList(D); $('#p-tbList').scrollIntoView({behavior:'smooth',block:'center'}); } });
  const cm = {}; P.forEach(l=>{ const o = cm[l.cc] || (cm[l.cc]={k:l.cc,label:IDX.ccDesc[l.cc]||'',v:0,n:0,late:0}); o.v+=l.valor; o.n++; if(l.comp && l.comp<=lc) o.late+=l.valor; });
  table('#p-tbCC', [
    {l:'CC', f:ccCell}, {l:'Itens', n:1, f:o=>o.n}, {l:'Valor', n:1, f:o=>short(o.v)}, {l:'Em mês fechado', n:1, f:o=>o.late?`<span class="pill late">${short(o.late)}</span>`:'—'}
  ], Object.values(cm).sort((a,b)=>b.v-a.v), { click:setCC });
  renderProvList(D);
}
function renderProvList(D){
  D = D || IDX.D; const lc = M.meta.lastClosed, today = new Date().toISOString().slice(0,10);
  const q = S.pq.trim().toLowerCase();
  let rows = provBase(D).filter(l=>!S.pst || l.status===S.pst).filter(l=>!q || (l.forn+' '+l.desc+' '+l.pedido+' '+l.nf).toLowerCase().includes(q));
  rows.sort((a,b)=>(a.venc||'9999').localeCompare(b.venc||'9999'));
  table('#p-tbList', [
    {l:'Venc.', f:l=>{ const v = l.venc && l.venc<today; return l.venc?`<span class="${v?'pill late':''}">${dBR(l.venc)}</span>`:'<span class="muted">—</span>'; }},
    {l:'Comp.', f:l=> `<span class="${l.comp&&l.comp<=lc?'pill late':''}">${ymLabel(l.comp)}</span>`},
    {l:'Etapa', f:l=>`<span class="pill pend">${esc(l.status)}</span>`}, {l:'Fornecedor', f:l=>esc(l.forn)}, {l:'Descrição', f:l=>`<span class="muted">${esc(l.desc)}</span>`},
    {l:'CC', f:l=>esc(l.cc)}, {l:'Filial', f:l=>esc(l.divDesc)}, {l:'Pedido', f:l=>`<span class="muted">${esc(l.pedido)}</span>`}, {l:'Valor', n:1, f:l=>brl(l.valor)}
  ], rows.slice(0,S.pLimit));
  $('#p-info').innerHTML = `${rows.length} item(ns) a lançar · total ${brl(sum(rows.map(r=>r.valor)))} · ordenado por vencimento` + (rows.length>S.pLimit?` · mostrando ${S.pLimit} <button id="p-more">mostrar mais</button>`:'') + (S.pst||S.pq?' · <button id="p-clr">limpar</button>':'');
  const mb = $('#p-more'); if (mb) mb.onclick = ()=>{ S.pLimit += 300; renderProvList(); };
  const cb = $('#p-clr'); if (cb) cb.onclick = ()=>{ S.pst=''; S.pq=''; $('#p-q').value=''; buildFilters(); renderProvList(); };
}

/* ---------- FORECAST ---------- */
function renderFcst(D){
  const t = D.t;
  const yrs = IDX.years.map(y=>{ const ix = M.meta.months.map((m,i)=>m.startsWith(y)?i:-1).filter(i=>i>=0); return {y, b: sum(D.B.map(b=>sum(ix.map(i=>b.m[i])))), cf: sum(D.CF.map(c=>sum(ix.map(i=>c.m[i]))))}; });
  const hor = {b: sum(yrs.map(o=>o.b)), cf: sum(yrs.map(o=>o.cf))};
  const nx = yrs.find(o=>o.y===D.nextY);
  $('#f-kpis').innerHTML = [
    kpi(`Projeção ${D.Y}`, short(t.CF), deltaSpan(t.CF-t.B, t.B)+' vs orçado', '', brl(t.CF)),
    kpi('Deve sobrar do orçado', `<span style="color:${colorV(t.B-t.CF)}">${short(t.B-t.CF)}</span>`, `Orçado ${short(t.B)} − Projeção`, '', brl(t.B-t.CF)),
    kpi('Já realizado da projeção', pct(t.CF?t.CFytd/t.CF:null,0), `${short(t.CFytd)} fechado · ${short(t.CF-t.CFytd)} a realizar`, meter(t.CF?t.CFytd/t.CF:0)),
    kpi('Projeção meses abertos', short(t.CF-t.CFytd), `${12-t.closedN} mês(es) · média ${short((t.CF-t.CFytd)/Math.max(1,12-t.closedN))}/mês`, '', brl(t.CF-t.CFytd)),
    nx ? kpi(`Projeção ${D.nextY}`, short(nx.cf), deltaSpan(nx.cf-nx.b, nx.b)+' vs orçado', '', brl(nx.cf)) : kpi('Próximo ano','—',''),
    kpi(`Horizonte ${IDX.years[0]}–${IDX.years[IDX.years.length-1]}`, short(hor.cf), deltaSpan(hor.cf-hor.b, hor.b)+' vs orçado', '', brl(hor.cf))
  ].join('');
  cumChart('#f-cCum', D); ccChart('#f-cCC', D);
  { let ab=0, ac=0; yrs.forEach(o=>{ ab+=o.b; ac+=o.cf; o.ab=ab; o.ac=ac; }); }
  table('#f-tbYears', [
    {l:'Ano', f:o=>`<b>${o.y}</b>${o.y===D.Y?' <span class="pill">selecionado</span>':''}`}, {l:'Orçado', n:1, f:o=>short(o.b)}, {l:'Projeção', n:1, f:o=>short(o.cf)},
    {l:'Saldo do ano', n:1, f:o=>vCell(o.b-o.cf)}, {l:'Orçado acum.', n:1, f:o=>short(o.ab)}, {l:'Projeção acum.', n:1, f:o=>short(o.ac)}, {l:'Saldo acumulado', n:1, f:o=>`<b>${vCell(o.ab-o.ac)}</b>`}
  ], yrs, { click:o=>{ S.year=o.y; buildFilters(); render(); }, foot:['Total', short(hor.b), short(hor.cf), vCell(hor.b-hor.cf), '', '', ''], after:'<div class="small muted" style="margin-top:6px">Acumulado = soma desde '+yrs[0].y+' até o ano da linha. Clique num ano para selecioná-lo.</div>' });
  const cc = D.byCC.filter(o=>o.B||o.CF).sort((a,b)=>b.CF-a.CF);
  table('#f-tbCC', [
    {l:'CC', f:ccCell}, {l:'Orçado', n:1, f:o=>short(o.B)}, {l:'Projeção', n:1, f:o=>short(o.CF)}, {l:'Sobra', n:1, f:vo=>vCell(vo.V)}, {l:'%', n:1, f:o=>o.B?pct(o.V/o.B,0):'<span class="pill late">sem orç.</span>'}
  ], cc, { click:setCC, foot:['Total', short(t.B), short(t.CF), vCell(t.B-t.CF), t.B?pct((t.B-t.CF)/t.B,0):'—'] });
}

/* ---------- LANÇAMENTOS ---------- */
/* ---------- CONFERÊNCIA SAP ---------- */
const SIT = [
  ['Conferido','ok','Mesmo pedido, CC, mês e valor na planilha e na KSB1.','Nada a fazer.'],
  ['Pendente de lançamento','late','Está na planilha como LANÇADO, mas o SAP não tem esse pedido/CC no mês nem em outro mês com o mesmo valor.','Confira se a MIRO foi lançada. Se foi lançada depois, ajuste a competência na planilha.'],
  ['Lançado após o fechamento','pend','O SAP lançou com data do mês fechado, mas depois do dia 6 (pela data de entrada ou porque só apareceu na KSB1 seguinte). Pela regra de fechamento, entra no mês atual.','Mude a competência da linha na planilha para o mês atual.'],
  ['Lançado em outro mês','pend','O SAP lançou o mesmo valor em outro mês (ex.: a planilha diz agosto, o SAP lançou em setembro).','Troque a competência na planilha para o mês do SAP.'],
  ['Valor diferente','pend','Pedido, CC e mês batem, mas o valor não.','Veja se falta parcela, estorno ou se o valor da nota foi digitado errado.'],
  ['Rateio diferente','pend','O pedido bate no total do mês, mas a divisão entre CCs não.','Ajuste o CC SAP das linhas do rateio na planilha.'],
  ['Pedido não encontrado no SAP','late','O pedido da planilha não aparece em nenhum mês da KSB1. Pode ser CC fora do filtro da exportação ou pedido digitado errado.','Confira o número do pedido e se o CC está na lista usada para exportar a KSB1. Enquanto isso, o realizado usa o valor da planilha.'],
  ['Lançado só no SAP','late','O pedido está na planilha, mas este lançamento (CC/mês) não tem linha correspondente.','Inclua a linha na planilha (normalmente uma parcela ou nota que faltou registrar).'],
  ['Estorno / soma negativa','late','A soma do pedido no mês ficou negativa: estorno sem o lançamento original.','Confira no SAP o documento de estorno e registre na planilha com status ESTORNADO.'],
  ['Verificar: fornecedor nosso, pedido fora da planilha','pend','Lançamento na KSB1 de um fornecedor que é nosso, com pedido que não está na planilha. Não entra no realizado.','Se a despesa é de TI, inclua na planilha com o pedido. Se é de outra área, ignore.']
];
const SITI = Object.fromEntries(SIT.map((s,i)=>[s[0],i]));
function confRows(){ return (M.conf||[]).filter(c=>pass(c) && c.ym && c.ym.startsWith(S.year)); }
function renderConf(D){
  const rows = confRows();
  const g = {}; rows.forEach(c=>{ const o = g[c.sit] || (g[c.sit]={n:0,v:0,p:0,s:0}); o.n++; o.p+=c.vPlan||0; o.s+=c.vSap||0; o.v+=Math.abs((c.vSap||0)-(c.vPlan||0)); });
  const ok = g['Conferido']||{n:0,s:0}, tot = rows.filter(c=>!/^Verificar/.test(c.sit)).length;
  const prob = rows.filter(c=>c.sit!=='Conferido' && !/^Verificar/.test(c.sit));
  const soSap = g['Lançado só no SAP']||{n:0,s:0}, ver = g['Verificar: fornecedor nosso, pedido fora da planilha']||{n:0,s:0};
  const ms = M.meta.sapMonths||[];
  $('#c-kpis').innerHTML = [
    kpi('Meses com KSB1', ms.length?String(ms.length):'0', ms.length?`${ymLabel(ms[0])} a ${ymLabel(ms[ms.length-1])}`:'coloque a KSB1 em Orçamento\\MM.AAAA'),
    kpi('Conferidos', tot?pct(ok.n/tot,0):'—', `${ok.n} de ${tot} pedido/CC/mês · ${short(ok.s)}`, meter(tot?ok.n/tot:0)),
    kpi('Com diferença', String(prob.length), `${short(sum(prob.map(c=>Math.abs((c.vSap||0)-(c.vPlan||0)))))} de diferença absoluta`),
    kpi('Lançado só no SAP', short(soSap.s), `${soSap.n} item(ns) de pedidos nossos sem linha na planilha`),
    kpi('Verificar', short(ver.s), `${ver.n} lançamento(s) de fornecedor nosso com pedido fora da planilha`),
    kpi('Ignorado (outras áreas)', short(sum(Object.entries(M.meta.ignorado||{}).filter(([cc])=>!S.cc||cc===S.cc).map(([,v])=>v))), 'KSB1 de pedidos de outras áreas nos mesmos CCs')
  ].join('');
  const sr = SIT.map(s=>({s, o:g[s[0]]})).filter(x=>x.o);
  if (!sr.length) $('#c-sit').innerHTML = '<div class="empty">Sem KSB1 para conferir (ou nada nos filtros atuais).</div>';
  else table('#c-sit', [
    {l:'Situação', f:x=>`<span class="pill ${x.s[1]}" title="${esc(x.s[2]+' Sugestão: '+x.s[3])}">${esc(x.s[0])}</span>${S.cst===x.s[0]?' <span class="pill">filtrado</span>':''}`},
    {l:'Itens', n:1, f:x=>x.o.n}, {l:'Planilha', n:1, f:x=>short(x.o.p)}, {l:'SAP', n:1, f:x=>short(x.o.s)}
  ], sr, { click:x=>{ S.cst = S.cst===x.s[0]?'':x.s[0]; renderConf(D); }, after:'<div class="small muted" style="margin-top:6px">Passe o mouse na situação para ver o que significa e o que fazer.</div>' });
  const lc = M.meta.lastClosed;
  table('#c-src', [
    {l:'Mês', f:ym=>ymLabel(ym)+(ym===IDX.curYM?'*':'')},
    {l:'Fonte', f:ym=>{ const s=M.meta.monthSrc[ym]; return s==='SAP'?'<span class="pill ok">SAP (KSB1)</span>':s==='Planilha'?'<span class="pill late">Planilha (provisório)</span>':'<span class="pill">em aberto</span>'; }},
    {l:'Realizado no painel', n:1, f:ym=>{ const i=D.ymY.indexOf(ym); return i<0?'—':short(D.mR[i]); }}
  ], D.ymY, { after:`<div class="small muted" style="margin-top:6px">Mês fechado sem KSB1 na pasta usa a planilha. Exporte a KSB1 acumulada (jan até o mês) todo dia 5 e salve em Orçamento\\MM.AAAA.</div>` });
  $('#c-st').innerHTML = `<option value="">Todas as situações</option>` + SIT.map(s=>`<option ${S.cst===s[0]?'selected':''}>${esc(s[0])}</option>`).join('');
  renderConfList();
}
function renderConfList(){
  const q = (S.cq||'').trim().toLowerCase();
  let rows = confRows().filter(c=>!S.cst || c.sit===S.cst).filter(c=>!q || (c.ped+' '+c.forn+' '+c.desc+' '+c.cc).toLowerCase().includes(q));
  if (!S.cst) rows = rows.filter(c=>c.sit!=='Conferido');
  rows.sort((a,b)=>(SITI[a.sit]-SITI[b.sit]) || (Math.abs((b.vSap||0)-(b.vPlan||0)) - Math.abs((a.vSap||0)-(a.vPlan||0))));
  table('#c-list', [
    {l:'Situação', f:c=>{ const s=SIT[SITI[c.sit]]; return `<span class="pill ${s[1]}" title="${esc(s[3])}">${esc(c.sit)}</span>`; }},
    {l:'Mês', f:c=>ymLabel(c.ym)+(c.ymSap?` → <b>${ymLabel(c.ymSap)}</b>`:'')}, {l:'Pedido', f:c=>esc(c.ped)}, {l:'Fornecedor', f:c=>esc(c.forn)},
    {l:'CC', f:c=>esc(c.cc)}, {l:'Filial', f:c=>esc(c.divDesc||c.div)}, {l:'Descrição', f:c=>`<span class="muted">${esc((c.desc||'').slice(0,80))}</span>`},
    {l:'Planilha', n:1, f:c=>brl(c.vPlan)}, {l:'SAP', n:1, f:c=>brl(c.vSap)}, {l:'Diferença', n:1, f:c=>{ const d=(c.vSap||0)-(c.vPlan||0); return Math.abs(d)<=1?'—':`<span style="color:var(--crit-ink)">${brl(d)}</span>`; }}
  ], rows.slice(0,S.cLimit));
  $('#c-info').innerHTML = `${rows.length} item(ns)${S.cst?'':' (conferidos ocultos — escolha “Conferido” para ver)'} · planilha ${brl(sum(rows.map(c=>c.vPlan||0)))} · SAP ${brl(sum(rows.map(c=>c.vSap||0)))}` + (rows.length>S.cLimit?` · mostrando ${S.cLimit} <button id="c-more">mostrar mais</button>`:'');
  const mb = $('#c-more'); if (mb) mb.onclick = ()=>{ S.cLimit += 500; renderConfList(); };
}

const VIEWNAME = {geral:'Visão geral', desp:'Realizado', forn:'Fornecedores', lanc:'Lançamentos', orc:'Orçado', prov:'A lançar', fcst:'Projeção', conf:'Conferência SAP'};
function renderBack(){
  const on = !!(S.back && S.view==='lanc');
  $('#backBar').style.display = on ? 'block' : 'none';
  if (on){ $('#btnBack').textContent = '← Voltar para ' + VIEWNAME[S.back.view]; $('#backInfo').textContent = ''; }
}
function goBack(){
  const b = S.back; if (!b) return;
  S.back = null; S.lf = {mes:'', fk:'', fname:''}; S.view = b.view;
  buildFilters(); render(); requestAnimationFrame(()=>window.scrollTo({top:b.y}));
}
function goView(v){
  hideTT(); S.view = v; if (v!=='fcst') S.year = IDX.curYear; if (v!=='lanc') S.back = null;
  store.set('fcxDash.view', v); buildFilters(); render(); window.scrollTo({top:0, behavior:'smooth'});
}
function goLanc(f){
  hideTT(); if (S.view!=='lanc') S.back = { view:S.view, y:window.scrollY };
  S.lf = { mes:f.mes||'', fk:f.fk||'', fname:f.fname||'' }; S.q=''; S.status=''; $('#q').value=''; S.lancLimit=200;
  S.view = 'lanc'; S.year = IDX.curYear; buildFilters(); render(); window.scrollTo({top:0});
}
function openFornByName(name){
  const k = fornKey(name), o = fornData(IDX.D).find(x=>x.k===k);
  if (o) openFornModal(o); else goLanc({fk:k, fname:name});
}
function wireKpis(sel, views){
  $$(sel+' > .kpi').forEach((el,i)=>{ const v = views[i]; if (!v) return;
    el.classList.add('go'); el.tabIndex = 0; el.setAttribute('role','button'); el.title = 'Abrir ' + VIEWNAME[v];
    el.onclick = ()=>goView(v); el.onkeydown = e=>{ if (e.key==='Enter'||e.key===' '){ e.preventDefault(); goView(v); } }; });
}
function wireMov(p){
  $$('#'+p+'-mov li').forEach(li=>{ li.classList.add('go'); li.onclick = ()=>openFornByName(li.title); });
}
function renderChips(){
  const c = [];
  S.grps.forEach(k=>c.push(['grp',k,GLABEL[k]]));
  if (S.gest){ const g = GESTORES.find(x=>x.k===S.gest); if (g) c.push(['gest','', 'Gestor '+g.nome+' ('+g.desc+')']); }
  if (S.div) c.push(['div','', 'Filial '+(IDX.divDesc[S.div]||S.div)]);
  if (S.cc) c.push(['cc','', 'CC '+S.cc+' '+(IDX.ccDesc[S.cc]||'')]);
  if (S.tipo) c.push(['tipo','', S.tipo]);
  if (S.conta) c.push(['conta','', 'Conta '+S.conta+' '+(IDX.contaDesc[S.conta]||'')]);
  $('#chips').innerHTML = c.length ? c.map(([k,v,l])=>`<span class="chip">${esc(l)}<button data-k="${k}" data-v="${esc(v)}" aria-label="Remover ${esc(l)}">×</button></span>`).join('')
    : '<span class="none">Todo o TI · clique nos gráficos ou em Filtrar para focar</span>';
  $$('#chips button').forEach(b=>b.onclick=()=>{ const k=b.dataset.k; if (k==='grp') toggleGrp(b.dataset.v); else { S[k]=''; buildFilters(); render(); } });
  $('#yearLab').textContent = S.view==='fcst' ? 'Projeção ' + S.year : 'Ano ' + IDX.curYear;
  $('#btnClear').style.display = c.length ? '' : 'none';
}
function renderLanc(){
  const q = S.q.trim().toLowerCase(), Y = S.year;
  let rows = M.lancPlan.filter(r=>pass(r)).filter(l=> !l.comp || l.comp.startsWith(Y));
  if (S.lf.mes) rows = rows.filter(l=>l.comp===S.lf.mes);
  if (S.lf.fk) rows = rows.filter(l=>fornKey(l.forn)===S.lf.fk);
  { const lc0 = M.meta.lastClosed, ms = M.meta.months.filter(m=>m.startsWith(Y));
    $('#l-meses').innerHTML = `<button data-m="" class="${!S.lf.mes?'on':''}">Ano todo</button>` + ms.map(m=>`<button data-m="${m}" class="${S.lf.mes===m?'on':''} ${m>lc0?'fut':''}">${MES[+m.slice(5)-1]}</button>`).join('');
    $$('#l-meses button').forEach(b=>b.onclick=()=>{ S.lf.mes = b.dataset.m; S.lancLimit=200; renderLanc(); });
    $('#l-chips').innerHTML = S.lf.fk ? `<span class="chip">${esc(fornShort(S.lf.fname||S.lf.fk))}<button aria-label="Remover fornecedor">×</button></span>` : '';
    const cb = $('#l-chips button'); if (cb) cb.onclick = ()=>{ S.lf.fk=''; S.lf.fname=''; renderLanc(); }; }
  if (S.status) rows = rows.filter(l=>l.status===S.status);
  const PG = (window.FCX_PAG && window.FCX_PAG.byId) || {};
  rows.forEach(l=>{ const p = PG[l.id]; l.pagSt = p ? p.st : (l.classif==='Realizado' ? 'Sem dado' : 'A lançar'); l.dtComp = (p && p.dtComp) || ''; l._pg = p || null; });
  if (q) rows = rows.filter(l=>(l.forn+' '+l.desc+' '+l.pedido+' '+l.nf+' '+l.miro+' '+l.folha+' '+l.ccDesc+' '+l.divDesc+' '+(l.cnpj||'')+' '+(l.bp||'')+' '+((l._pg&&l._pg.docComp)||'')).toLowerCase().includes(q));
  const hasNF = l => !!String(l.nf||'').trim();
  const lancR = rows.filter(l=>l.classif==='Realizado'), notaR = rows.filter(l=>l.classif!=='Realizado' && hasNF(l)), waitR = rows.filter(l=>l.classif!=='Realizado' && !hasNF(l));
  $('#l-view').innerHTML = [['lanc','Lançadas',lancR],['nota','Nota recebida, falta lançar',notaR]].map(([k,l,a])=>`<button data-lv="${k}" class="${S.lv===k?'on':''}">${l} (${a.length})</button>`).join('');
  $$('#l-view button').forEach(b=>b.onclick=()=>{ S.lv=b.dataset.lv; S.pag=''; S.lancLimit=200; renderLanc(); });
  $('#l-viewHint').textContent = S.lv==='nota' ? 'nota já chegou (NF preenchida) e ainda não foi lançada no SAP: acompanhe o vencimento' : 'lançadas no SAP (MIRO feita)';
  $('#l-wait').innerHTML = waitR.length ? `${waitR.length} item(ns) aguardando a nota do mês (${short(sum(waitR.map(r=>r.valor)))}) não aparecem aqui — <button id="goProv">ver em A lançar</button>` : '';
  { const gp = $('#goProv'); if (gp) gp.onclick = ()=>{ goView('prov'); window.scrollTo({top:0}); }; }
  rows = S.lv==='nota' ? notaR : lancR;
  if (S.lv==='lanc') renderPag(rows); else $('#l-pag').innerHTML='';
  if (S.lv==='lanc' && S.pag) rows = rows.filter(l=>l.pagSt===S.pag);
  const k = S.lancSort.k, d = S.lancSort.d;
  rows.sort((a,b)=>{ const x=a[k]??'', y=b[k]??''; return (typeof x==='number'? x-y : String(x).localeCompare(String(y)))*d; });
  const lc = M.meta.lastClosed, today = new Date().toISOString().slice(0,10);
  const cols = [['comp','Comp.'],['status','Status'],['forn','Fornecedor'],['bp','BP'],['desc','Descrição'],['cc','CC'],['divDesc','Filial'],['nf','NF'],['pedido','Pedido'],['venc','Venc.'],['valor','Valor']].concat(S.lv==='lanc'?[['pagSt','Pagamento'],['dtComp','Pago em']]:[]);
  let h = `<table><thead><tr>${cols.map(([c,l])=>`<th data-k="${c}" class="${c==='valor'?'n':''}">${l}${S.lancSort.k===c?(d>0?' ↑':' ↓'):''}</th>`).join('')}</tr></thead><tbody>`;
  rows.slice(0,S.lancLimit).forEach(l=>{
    const late = l.classif!=='Realizado' && ((l.comp && l.comp<=lc) || (l.venc && l.venc<today));
    const pill = l.classif==='Realizado' ? `<span class="pill ok" title="${l.kind==='X'?'Conferido pelo SAP: o realizado oficial deste mês vem da KSB1':''}">${esc(l.status)}${l.kind==='X'?' ✓SAP':''}</span>` : `<span class="pill ${late?'late':'pend'}">${esc(l.status)}</span>`;
    h += `<tr><td>${ymLabel(l.comp)}</td><td>${pill}</td><td>${esc(l.forn)}${l.cnpj?`<div class="small muted">${esc(maskDoc(l.cnpj))}</div>`:''}</td><td class="muted">${esc(l.bp||'')}</td><td class="muted">${esc(l.desc)}</td><td>${esc(l.cc)}</td><td>${esc(l.divDesc)}</td><td class="muted">${esc(l.nf)}</td><td class="muted">${esc(l.pedido)}</td><td>${dBR(l.venc)}</td><td class="n">${brl(l.valor)}</td>${S.lv==='lanc'?`<td>${pagPill(l)}${l._pg&&l._pg.compDif?` <span class="pill pend" title="Na FBL1N a nota entrou no SAP em ${ymLabel(l._pg.ymSap)} (regra do dia 6). Ajuste a competência da linha na planilha.">SAP: ${ymLabel(l._pg.ymSap)}</span>`:''}</td><td>${l.dtComp?dBR(l.dtComp):''}</td>`:''}</tr>`;
  });
  $('#tbLanc').innerHTML = h + '</tbody></table>';
  $$('#tbLanc th').forEach(th=>th.onclick=()=>{ const c=th.dataset.k; S.lancSort = {k:c, d: S.lancSort.k===c? -S.lancSort.d : (c==='valor'||c==='comp'?-1:1)}; renderLanc(); });
  const nDif = rows.filter(l=>l._pg&&l._pg.compDif).length;
  $('#lancInfo').innerHTML = `${rows.length} item(ns) em ${Y} · total ${brl(sum(rows.map(r=>r.valor)))}` + (nDif?` · <span class="pill pend">${nDif} com competência diferente da FBL1N</span>`:'') + (rows.length>S.lancLimit?` · mostrando ${S.lancLimit} <button id="more">mostrar mais</button>`:'') + (S.status||S.q||S.pag?` · <button id="clrL">limpar busca/status</button>`:'') + (window.FCX_PAG?` · pagamento: ${esc(window.FCX_PAG.meta.fonte)}, extraída em ${dBR(window.FCX_PAG.meta.extraido)} (empresas ${esc(window.FCX_PAG.meta.empresas.join(', '))})`:'');
  const mb = $('#more'); if (mb) mb.onclick = ()=>{ S.lancLimit += 300; renderLanc(); };
  const cb = $('#clrL'); if (cb) cb.onclick = ()=>{ S.status=''; S.q=''; S.pag=''; $('#q').value=''; buildFilters(); renderLanc(); };
  renderOrf(q);
}
const PAGCLS = {'Paga':'ok','Em aberto':'pend','Vencida':'late','Não localizada':'late','Outra empresa':'out','Sem dado':'out','A lançar':'out'};
function pagPill(l){
  if (l.pagSt==='A lançar') return '<span class="muted small">—</span>';
  const p = l._pg, t = p ? [p.how, p.docComp?('doc. compensação '+p.docComp):'', p.liq!=null?('líquido pago/a pagar '+brl(p.liq)):'', p.vencSap?('venc. SAP '+dBR(p.vencSap)):'', p.ref?('NF no SAP '+p.ref):'', p.emp?('empresa '+p.emp):'', p.bloq?'BLOQUEIO DE PAGAMENTO':''].filter(Boolean).join(' · ') : '';
  return `<span class="pill ${PAGCLS[l.pagSt]||''}" title="${esc(t)}">${esc(l.pagSt)}${p&&p.bloq?' ⚠':''}</span>`;
}
function renderPag(rows){
  const el = $('#l-pag'); if (!window.FCX_PAG){ el.innerHTML=''; return; }
  const segs = [['Paga','Pagas','compensadas no SAP'],['Em aberto','Em aberto','a vencer'],['Vencida','Vencidas','não pagas e vencidas'],['Não localizada','Não localizadas','sem nota no FBL1N']];
  el.innerHTML = '<div class="pgs">' + segs.map(([k,l,f])=>{ const r = rows.filter(x=>x.pagSt===k); return `<button data-p="${k}" class="${S.pag===k?'on':''}" title="Filtrar: ${l}"><span class="l">${l}</span><span class="v">${short(sum(r.map(x=>x.valor)))}</span><span class="n">${r.length} item(ns) · ${f}</span></button>`; }).join('') + '</div>';
  $$('#l-pag button').forEach(b=>b.onclick=()=>{ S.pag = S.pag===b.dataset.p ? '' : b.dataset.p; S.lancLimit=200; renderLanc(); });
}
function renderOrf(q){
  const P = window.FCX_PAG; const card = $('#l-orfCard'); if (!P || S.lv!=='lanc' || ACL.ok){ card.style.display='none'; return; } card.style.display='';
  let o = P.orphans.slice();
  if (S.lf.fk) o = o.filter(x=>fornKey(x.forn)===S.lf.fk);
  if (S.lf.mes) o = o.filter(x=>(x.dtLanc||'').slice(0,7)===S.lf.mes);
  if (q) o = o.filter(x=>(x.forn+' '+x.ref+' '+x.doc+' '+x.txt+' '+x.docComp+' '+x.bp).toLowerCase().includes(q));
  o.sort((a,b)=>b.liq-a.liq);
  $('#l-orfInfo').textContent = `${o.length} nota(s) · ${short(sum(o.map(x=>x.liq)))} · ${o.filter(x=>!x.dtComp).length} em aberto`;
  let h = '<table><thead><tr><th>Lançado em</th><th>Empresa</th><th>Fornecedor</th><th>BP</th><th>NF</th><th>Doc. SAP</th><th>Divisão</th><th>Venc.</th><th class="n">Líquido</th><th>Pagamento</th></tr></thead><tbody>';
  o.slice(0,400).forEach(x=>{ h += `<tr><td>${dBR(x.dtLanc)}</td><td class="muted">${esc(x.emp)}</td><td>${esc(x.forn)}</td><td class="muted">${esc(x.bp)}</td><td>${esc(x.ref)}</td><td class="muted">${esc(x.doc)}</td><td class="muted">${esc(x.div)}</td><td>${dBR(x.venc)}</td><td class="n">${brl(x.liq)}</td><td>${x.dtComp?`<span class="pill ok">Paga ${dBR(x.dtComp)}</span>`:`<span class="pill ${x.venc&&x.venc<new Date().toISOString().slice(0,10)?'late':'pend'}">Em aberto</span>`}</td></tr>`; });
  $('#tbOrf').innerHTML = h + '</tbody></table>' + (o.length>400?`<div class="small muted">mostrando 400 de ${o.length}; use a busca para filtrar</div>`:'');
}

/* ---------- FORNECEDORES ---------- */
/* Os nomes de fornecedor vêm escritos de jeitos diferentes no orçamento e no SAP
   ("INGRAM", "INGRAM MICRO BRASIL LTDA", "RETAILX_INGRAM MICRO BRASIL LTDA").
   fornKey() cria uma chave comum. Se algum agrupamento sair errado, corrija em FORN_ALIAS
   (nome normalizado → chave) ou inclua o primeiro nome em FORN_FULL. */
const FORN_GEN = new Set(['CONSULTORIA','CENTRO','SERVICOS','SERVICO','TECNOLOGIA','TECNOLOGIAS','BRASIL','DO','DA','DE','DOS','DAS','E','EM','CONSULT','INFORMATICA','SISTEMAS','SOLUCOES','TELECOM','TELECOMUNICACOES','COMERCIO','GRUPO','CIA','DISTRIBUIDORA','ASSESSORIA','EMPRESARIAL','EMPRESA','MANUTENCAO','VIAGEM']);
const FORN_FULL = new Set(['PAULO']);               // nomes de pessoa: não agrupar só pelo primeiro nome
const FORN_ALIAS = { 'L V DE M CAVALCANTI EQUIPAMENTO':'CAVALCANTI', 'L V DE M CAVALCANTI':'CAVALCANTI' };                               // ajuste manual: 'NOME NORMALIZADO': 'CHAVE'
function fornNorm(s){
  let n = String(s||'').normalize('NFD').replace(/[\u0300-\u036f]/g,'').toUpperCase().trim();
  n = n.replace(/^(RETAILX|RX)[_\- ]+/,'');
  n = n.split(/\s\/\s|\s-\s/)[0];
  n = n.replace(/\b(LTDA|LTD|LT|S\.?\s?A|SA|S\/A|EIRELI|ME|EPP|INC|PTY|LLC)\b\.?/g,' ');
  return n.replace(/[^A-Z0-9& ]/g,' ').replace(/\s+/g,' ').trim();
}
function fornKey(s){
  const n = fornNorm(s); if (!n) return '(sem fornecedor)';
  if (FORN_ALIAS[n]) return FORN_ALIAS[n];
  let t = n.split(' '), init = false;
  if (t.length>1 && t[0].length===1){ init = true;                 // iniciais/artigo (A MAIS, J NOBRE, L V DE M ...)
    while (t.length>1 && (t[0].length===1 || FORN_GEN.has(t[0]))) t = t.slice(1);
  } else if (FORN_GEN.has(t[0])) return n;           // começa com palavra genérica: usa o nome inteiro
  const sig = t.filter(w=>w.length>1 && !FORN_GEN.has(w));
  if (!sig.length) return n;
  if (FORN_FULL.has(sig[0])) return n;
  if (init) return sig.slice(0,2).join(' ');           // após iniciais, o primeiro nome sozinho é pouco (A MAIS × E MAIS)
  return sig[0].length<=2 && sig[1] ? sig[0]+' '+sig[1] : sig[0];   // siglas curtas (IP, PF, HI) levam a palavra seguinte
}

function fornData(D){
  const lc = M.meta.lastClosed, g = {};
  const get = raw => { const k = fornKey(raw); return g[k] || (g[k] = { k, names:{}, B:0, R:0, C:0, late:0, n:0, last:'',
    Bm:D.ymY.map(()=>0), Rm:D.ymY.map(()=>0), Cm:D.ymY.map(()=>0), ccs:new Set(), grps:new Set(), lines:[], lanc:[] }); };
  const nm = (o, raw, w) => { const r = raw || '(sem fornecedor)'; o.names[r] = (o.names[r]||0) + w; };
  D.B.forEach(b=>{ const v = sum(D.yIx.map(i=>b.m[i])); if (Math.abs(v)<0.5) return;
    const o = get(b.forn); o.B += v; D.yIx.forEach((i,j)=>o.Bm[j]+=b.m[i]); o.ccs.add(b.cc); o.grps.add(b.grp); o.lines.push(b); nm(o, b.forn, 0); });
  D.LY.forEach(l=>{ const o = get(l.forn), j = D.ymY.indexOf(l.comp);
    if (l.classif==='Realizado'){ o.R += l.valor; if (j>=0) o.Rm[j] += l.valor; o.n++; if (l.comp > o.last) o.last = l.comp; }
    else { o.C += l.valor; if (j>=0) o.Cm[j] += l.valor; if (l.comp && l.comp<=lc) o.late += l.valor; }
    o.ccs.add(l.cc); o.grps.add(l.grp); o.lanc.push(l); nm(o, l.forn, 1 + Math.abs(l.valor||0)); });
  return Object.values(g).map(o=>{
    const ns = Object.entries(o.names).sort((a,b)=>b[1]-a[1]);
    o.name = ns[0][0]; o.nameList = ns.map(x=>x[0]);
    o.U = o.R + o.C; o.S = o.B - o.U; o.p = o.B>0 ? o.U/o.B : null;
    o.over = o.B>0 && o.U - o.B > 1; o.semB = o.B<=0.5 && o.U>0.5;
    return o;
  }).filter(o=>Math.abs(o.B)+Math.abs(o.U)>0.5);
}
const pctF = p => p!=null && p>9.995 ? '>999%' : pct(p,0);
const miniBarF = p => miniBar(p).replace(/<span>[^<]*<\/span>/, `<span>${pctF(p)}</span>`);
const fornShort = s => { const w = String(s).split(' '); return w.length>4 ? w.slice(0,4).join(' ')+'…' : s; };
const gColor = k => (GROUPS.find(g=>g.k===k)||{}).c || 'var(--ink-3)';
const grpDots = o => [...o.grps].sort().map(k=>`<span class="grp-dot" title="${esc(GLABEL[k]||k)}" style="background:${gColor(k)};margin-right:3px"></span>`).join('');

function renderForn(D){
  const F = IDX.forn = fornData(D), t = D.t, lc = M.meta.lastClosed;
  const act = F.filter(o=>o.U>0.5).sort((a,b)=>b.U-a.U), totU = sum(act.map(o=>o.U));
  const top5 = totU ? sum(act.slice(0,5).map(o=>o.U))/totU : null;
  const over = F.filter(o=>o.over), semB = F.filter(o=>o.semB), parado = F.filter(o=>o.B>0.5 && o.U<=0.5);
  $('#fo-kpis').innerHTML = [
    kpi(`Fornecedores com gasto em ${D.Y}`, String(act.length), `${act.filter(o=>o.B>0.5).length} com orçamento · ${act.filter(o=>o.B<=0.5).length} sem`, '', 'Realizado ou a lançar no ano'),
    kpi('Realizado + a lançar', short(totU), `${short(sum(act.map(o=>o.R)))} realizado · ${short(sum(act.map(o=>o.C)))} a lançar`, t.B?meter(sum(act.map(o=>o.R))/t.B, sum(act.map(o=>o.C))/t.B):'', brl(totU)),
    kpi('Peso dos 5 maiores', pct(top5,0), act.slice(0,3).map(o=>esc(fornShort(o.name).split(' ').slice(0,2).join(' '))).join(' · '), meter(top5), act.slice(0,5).map(o=>o.name+': '+brl0(o.U)).join('\n')),
    kpi('Acima do orçado do ano', `<span style="color:${over.length?'var(--crit-ink)':'inherit'}">${over.length}</span>`, over.length?`<span class="delta up">▲ ${short(sum(over.map(o=>o.U-o.B)))}</span> além do orçado`:'nenhum fornecedor passou do orçado', '', 'Realizado + a lançar maior que o orçado do ano inteiro'),
    kpi('Gasto sem orçamento', `<span style="color:${semB.length?'var(--crit-ink)':'inherit'}">${short(sum(semB.map(o=>o.U)))}</span>`, `${semB.length} fornecedor(es) sem linha no orçamento`, '', brl(sum(semB.map(o=>o.U)))),
    kpi('Orçado sem nenhum lançamento', short(sum(parado.map(o=>o.B))), `${parado.length} fornecedor(es) ainda sem nota em ${D.Y}`, '', 'Contrato atrasado, cancelado ou lançado com outro nome de fornecedor')
  ].join('');
  $('#fo-tTop').textContent = `Maiores fornecedores — ${D.Y}`;
  fornBars('#fo-cTop', act.slice(0,12));
  renderFornAlerts(D, F);
  renderFornTable(D);
}
function fornBars(sel, rows){
  const el = $(sel); if (!rows.length){ el.innerHTML = '<div class="empty">Nenhum gasto para os filtros atuais.</div>'; return; }
  const W = el.clientWidth || 600, rowH = 28, T = 6, H = T + rows.length*rowH + 6;
  const lab = Math.min(230, Math.max(140, W*0.34)), R = 64, iw = W - lab - R;
  const xmax = niceMax(Math.max(...rows.map(o=>Math.max(o.U, o.B)), 1));
  const x = v => lab + Math.max(0,v)/xmax*iw;
  let g = '';
  for (let k=0;k<=4;k++){ const v = xmax*k/4; g += `<line x1="${x(v)}" x2="${x(v)}" y1="${T}" y2="${H-6}" stroke="var(--line)"/>`; }
  rows.forEach((o,i)=>{
    const y = T + i*rowH, bh = 14, yb = y + (rowH-bh)/2;
    g += `<g class="frow" data-i="${i}" style="cursor:pointer"><rect x="0" y="${y}" width="${W}" height="${rowH}" fill="transparent"/>`;
    g += `<text x="${lab-8}" y="${y+rowH/2+4}" text-anchor="end" font-size="11.5" fill="var(--ink)">${esc(fornShort(o.name))}</text>`;
    if (o.R>0) g += `<rect x="${lab}" y="${yb}" width="${Math.max(1,x(o.R)-lab)}" height="${bh}" fill="var(--real)"/>`;
    if (o.C>0) g += `<rect x="${x(o.R)}" y="${yb}" width="${Math.max(1,x(o.U)-x(o.R))}" height="${bh}" fill="url(#fcx-hatch)"/>`;
    if (o.B>0.5) g += `<rect x="${x(o.B)-1.5}" y="${yb-4}" width="3" height="${bh+8}" fill="var(--base)"/>`;
    g += `<text x="${x(Math.max(o.U,o.B))+8}" y="${y+rowH/2+4}" font-size="11" fill="${o.over||o.semB?'var(--crit-ink)':'var(--ink-2)'}" font-weight="${o.over||o.semB?700:400}">${axisShort(o.U)}</text></g>`;
  });
  el.innerHTML = `<svg viewBox="0 0 ${W} ${H}" height="${H}" role="img" aria-label="Maiores fornecedores">${g}</svg>`;
  el.querySelectorAll('.frow').forEach(r=>{
    const o = rows[+r.dataset.i];
    r.addEventListener('mousemove', ev=>showTT(ev, `<div class="t">${esc(o.name)}</div>`+row(swl('var(--base)'),'Orçado do ano',o.B>0.5?brl0(o.B):'sem orçamento')+row(swc('var(--real)'),'Realizado',brl0(o.R))+row(swc('var(--fcst)'),'A lançar',brl0(o.C))+row('','Saldo',`<b style="color:${colorV(o.S)}">${brl0(o.S)}</b>`)+'<div class="small muted" style="margin-top:4px">Clique para ver o detalhe</div>'));
    r.addEventListener('mouseleave', hideTT);
    r.addEventListener('click', ()=>{ hideTT(); openFornModal(o); });
  });
}
function renderFornAlerts(D, F){
  const A = [], lc = M.meta.lastClosed, add = (sev,title,detail,value,what,sug,o) => A.push({sev,title,detail,value,what,sug,o});
  F.filter(o=>o.over && o.U-o.B>1000).sort((a,b)=>(b.U-b.B)-(a.U-a.B)).slice(0,5).forEach(o=>add((o.U-o.B)/o.B>0.1?'critical':'serious',
    `${fornShort(o.name)} acima do orçado`, `Realizado + a lançar ${short(o.U)} contra ${short(o.B)} orçado para o ano (${pctF(o.U/o.B)}).`, '+'+short(o.U-o.B),
    'O que já foi lançado ou está para lançar com este fornecedor passou do orçamento do ano inteiro.',
    'Abra o detalhe e compare as notas com as linhas do orçamento: reajuste não previsto, escopo extra ou nota lançada no fornecedor errado. Se o gasto continua, formalize o remanejamento.', o));
  F.filter(o=>o.semB && o.U>=10000).sort((a,b)=>b.U-a.U).slice(0,5).forEach(o=>add('serious',
    `${fornShort(o.name)} sem orçamento`, `${short(o.U)} em ${D.Y} sem nenhuma linha no orçamento com este fornecedor.`, short(o.U),
    'Há gasto com este fornecedor, mas nenhuma linha do orçamento está no nome dele.',
    'Pode ser um fornecedor orçado com outro nome (veja os nomes agrupados no detalhe) ou um gasto não planejado. Se for recorrente, inclua no orçamento do próximo ano.', o));
  F.filter(o=>o.late>=1000).sort((a,b)=>b.late-a.late).slice(0,5).forEach(o=>add('warning',
    `${fornShort(o.name)}: a lançar em mês fechado`, `Itens com competência até ${ymLabel(lc)} ainda não lançados.`, short(o.late),
    'Notas deste fornecedor que já deveriam estar lançadas no SAP.', 'Confira a etapa de cada item no detalhe (pedido, folha, envio) e cobre a pendência antes do próximo fechamento.', o));
  if (D.t.closedN>=3) F.filter(o=>o.B>=50000 && o.U<=0.5).sort((a,b)=>b.B-a.B).slice(0,4).forEach(o=>add('info',
    `${fornShort(o.name)} sem lançamento em ${D.Y}`, `Orçado ${short(o.B)} no ano e nenhuma nota até agora.`, short(o.B),
    'Fornecedor com orçamento relevante e nenhum lançamento no ano.', 'Verifique se o contrato começou, foi adiado ou cancelado, ou se as notas estão saindo com outro nome. Se cancelado, o valor pode ir para remanejamento.', o));
  const order = {critical:0, serious:1, warning:2, info:3};
  A.sort((a,b)=>order[a.sev]-order[b.sev]);
  const icon = {critical:'!', serious:'▲', warning:'!', info:'i'}, lbl = {critical:'Crítico', serious:'Alto', warning:'Atenção', info:'Info'};
  const el = $('#fo-alerts');
  el.innerHTML = A.length ? A.map((a,i)=>`<li class="click" data-i="${i}" style="cursor:pointer"><span class="ic ${a.sev}" tabindex="0" data-i="${i}" aria-label="${esc(a.what+' Sugestão: '+a.sug)}">${icon[a.sev]}</span><div><div class="tl"><span class="sevtag">${lbl[a.sev]}</span>${esc(a.title)}</div><div class="dt">${esc(a.detail)}</div></div><span class="v">${esc(a.value)}</span></li>`).join('')
    : '<li><span class="ic good">✓</span><div class="tl">Nada relevante para os filtros atuais.</div><span></span></li>';
  const tip = a => `<div class="t">${esc(a.title)}</div><div class="h">O que é</div><p>${esc(a.what)}</p><div class="h">Sugestão</div><p>${esc(a.sug)}</p>`;
  el.querySelectorAll('.ic[data-i]').forEach(ic=>{ const a = A[+ic.dataset.i];
    ic.addEventListener('mousemove', ev=>showTT(ev, tip(a), true)); ic.addEventListener('mouseleave', hideTT);
    ic.addEventListener('focus', ()=>{ const r = ic.getBoundingClientRect(); showTT({pageX:r.right+window.scrollX, pageY:r.top+window.scrollY}, tip(a), true); }); ic.addEventListener('blur', hideTT); });
  el.querySelectorAll('li[data-i]').forEach(li=>li.addEventListener('click', ()=>{ hideTT(); openFornModal(A[+li.dataset.i].o); }));
}
function sparkFor(o){
  const n = o.Rm.length, mx = Math.max(...o.Rm.map((v,i)=>v+o.Cm[i]), ...o.Bm, 1), W = 96, H = 24, bw = W/n;
  let g = '';
  o.Rm.forEach((v,i)=>{ const c = o.Cm[i], hR = Math.max(0,v)/mx*H, hC = Math.max(0,c)/mx*H, x = i*bw+1, w = Math.max(1,bw-2);
    if (hR>0) g += `<rect x="${x}" y="${H-hR}" width="${w}" height="${hR}" fill="var(--real)"/>`;
    if (hC>0) g += `<rect x="${x}" y="${H-hR-hC}" width="${w}" height="${hC}" fill="url(#fcx-hatch)"/>`; });
  if (o.B>0.5) g += `<polyline points="${o.Bm.map((v,i)=>`${i*bw+bw/2},${H-Math.max(0,v)/mx*H}`).join(' ')}" fill="none" stroke="var(--base)" stroke-width="1" stroke-dasharray="2 2"/>`;
  return `<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" aria-hidden="true" style="display:block;margin-left:auto">${g}</svg>`;
}
function renderFornTable(D){
  D = D || IDX.D; const F = IDX.forn || fornData(D), lc = M.meta.lastClosed;
  const segs = [['all','Todos'],['over','Acima do orçado'],['semB','Sem orçamento'],['late','Com pendência'],['parado','Sem lançamento']];
  $('#fo-seg').innerHTML = segs.map(([k,l])=>`<button data-s="${k}" class="${S.fo.seg===k?'on':''}">${l}</button>`).join('');
  $$('#fo-seg button').forEach(b=>b.onclick=()=>{ S.fo.seg=b.dataset.s; S.fo.limit=100; renderFornTable(); });
  const q = S.fo.q.trim().toLowerCase();
  const segF = { all:()=>true, over:o=>o.over, semB:o=>o.semB, late:o=>o.late>0.5, parado:o=>o.B>0.5 && o.U<=0.5 }[S.fo.seg];
  let rows = F.filter(segF).filter(o=>!q || o.nameList.join(' ').toLowerCase().includes(q));
  const { k, d } = S.fo.sort;
  const val = (o,k) => k==='name' ? o.name : k==='ccs' ? o.ccs.size : k==='last' ? (o.last||'') : (o[k] ?? -Infinity);
  rows.sort((a,b)=>{ const x = val(a,k), y = val(b,k); return (typeof x==='string' ? x.localeCompare(y) : x-y) * d; });
  const cols = [['name','Fornecedor'],['grp','Grupo'],['ccs','CCs'],['B','Orçado ano'],['R','Realizado'],['C','A lançar'],['S','Saldo'],['p','% usado'],['spark',`Mês a mês ${D.Y}`],['last','Último lanç.']];
  const num = new Set(['ccs','B','R','C','S','p','spark','last']), nosort = new Set(['grp','spark']);
  let h = `<table><thead><tr>${cols.map(([c,l])=>`<th class="${num.has(c)?'n':''}" ${nosort.has(c)?'style="cursor:default"':`data-k="${c}"`}>${l}${k===c?(d>0?' ↑':' ↓'):''}</th>`).join('')}</tr></thead><tbody>`;
  rows.slice(0,S.fo.limit).forEach((o,i)=>{
    const extra = o.nameList.length>1 ? ` <span class="pill" title="${esc(o.nameList.join('\n'))}">+${o.nameList.length-1} nome${o.nameList.length>2?'s':''}</span>` : '';
    const flag = o.semB ? ' <span class="pill late">sem orç.</span>' : (o.late>0.5 ? ` <span class="pill pend" title="a lançar em mês fechado">pend. ${short(o.late)}</span>` : '');
    h += `<tr class="click" data-i="${i}"><td><b>${esc(o.name)}</b>${extra}${flag}</td><td>${grpDots(o)}</td><td class="n" title="${esc([...o.ccs].sort().map(c=>c+' '+(IDX.ccDesc[c]||'')).join('\n'))}">${o.ccs.size}</td>
      <td class="n">${o.B>0.5?short(o.B):'<span class="muted">—</span>'}</td><td class="n">${short(o.R)}</td><td class="n">${o.C>0.5?short(o.C):'<span class="muted">—</span>'}</td>
      <td class="n">${vCell(o.S)}</td><td class="n">${o.B>0.5?miniBarF(o.p):'<span class="muted small">sem orç.</span>'}</td><td class="n">${sparkFor(o)}</td><td class="n">${o.last?ymLabel(o.last):'<span class="muted">—</span>'}</td></tr>`;
  });
  const T = k => sum(rows.map(o=>o[k]));
  h += `</tbody><tfoot><tr><td colspan="3">${rows.length} fornecedor(es)</td><td class="n">${short(T('B'))}</td><td class="n">${short(T('R'))}</td><td class="n">${short(T('C'))}</td><td class="n">${vCell(T('S'))}</td><td class="n">${T('B')?pctF((T('R')+T('C'))/T('B')):'—'}</td><td></td><td></td></tr></tfoot></table>`;
  const el = $('#fo-tb'); el.innerHTML = h; const shown = rows.slice(0,S.fo.limit);
  el.querySelectorAll('tbody tr').forEach(tr=>tr.onclick=()=>openFornModal(shown[+tr.dataset.i]));
  el.querySelectorAll('th[data-k]').forEach(th=>th.onclick=()=>{ const c = th.dataset.k; S.fo.sort = { k:c, d: S.fo.sort.k===c ? -S.fo.sort.d : (c==='name'||c==='last'?1:-1) }; renderFornTable(); });
  $('#fo-info').innerHTML = `Saldo = orçado do ano inteiro − realizado − a lançar (mesma conta do “Saldo disponível” da Visão geral). Não há projeção por fornecedor: a aba Current Forecast é por divisão, CC e conta. Nomes escritos de formas diferentes no orçamento e no SAP foram unificados; passe o mouse em “+n nomes” para conferir.`
    + (rows.length>S.fo.limit?` · mostrando ${S.fo.limit} de ${rows.length} <button id="fo-more">mostrar mais</button>`:'');
  const mb = $('#fo-more'); if (mb) mb.onclick = ()=>{ S.fo.limit += 200; renderFornTable(); };
}
function openFornModal(o){
  if (!o) return; const D = IDX.D, lc = M.meta.lastClosed, today = new Date().toISOString().slice(0,10);
  $('#mtitle').textContent = o.name;
  $('#msub').innerHTML = `${o.nameList.length>1?`Nomes agrupados: ${esc(o.nameList.join(' · '))}<br>`:''}${D.Y} · filtros do painel aplicados`;
  const k4 = [
    kpi('Orçado do ano', o.B>0.5?short(o.B):'—', o.B>0.5?`${o.lines.length} linha(s) no orçamento`:'nenhuma linha no orçamento', '', brl(o.B)),
    kpi('Realizado', short(o.R), `${o.n} nota(s) lançada(s)${o.last?' · última em '+ymLabel(o.last):''}`, o.B>0.5?meter(o.R/o.B, o.C/o.B):'', brl(o.R)),
    kpi('A lançar', short(o.C), o.late>0.5?`<span class="delta up">▲ ${short(o.late)}</span> em meses já fechados`:'nada pendente em mês fechado', '', brl(o.C)),
    kpi('Saldo do ano', `<span style="color:${colorV(o.S)}">${short(o.S)}</span>`, 'orçado − realizado − a lançar', '', brl(o.S))
  ].join('');
  const byCC = {}; const gcc = c => byCC[c] || (byCC[c] = { k:c, label:IDX.ccDesc[c]||'', B:0, R:0, C:0 });
  o.lines.forEach(b=>{ gcc(b.cc).B += sum(D.yIx.map(i=>b.m[i])); });
  o.lanc.forEach(l=>{ const x = gcc(l.cc); if (l.classif==='Realizado') x.R += l.valor; else x.C += l.valor; });
  const ccRows = Object.values(byCC).sort((a,b)=>(b.R+b.C+b.B)-(a.R+a.C+a.B));
  const lin = o.lines.map(b=>({b, v:sum(D.yIx.map(i=>b.m[i]))})).sort((a,b)=>b.v-a.v);
  $('#mbody').innerHTML = `<div class="mbar"><button class="btn primary" id="fo-goL">Ver lançamentos deste fornecedor</button></div><div class="grid" style="grid-template-columns:repeat(auto-fit,minmax(200px,1fr))">${k4}</div>
    <div class="card sec"><div class="card-h"><h3 class="mh3">Mês a mês</h3><span class="hint">por mês de lançamento no SAP</span></div>
      <div class="legend"><span><i class="sw" style="background:var(--real)"></i>Realizado</span><span><i class="sw" style="background:var(--fcst)"></i>A lançar</span><span><i class="sw line" style="background:var(--base)"></i>Orçado</span></div>
      <div class="chart" id="fo-mChart"></div></div>
    <div class="grid two-eq sec">
      <div class="card"><h3 class="mh3">Por centro de custo</h3><div class="scroll" style="max-height:260px" id="fo-mCC"></div></div>
      <div class="card"><h3 class="mh3">Linhas do orçamento</h3><div class="scroll" style="max-height:260px" id="fo-mLin"></div></div>
    </div>
    <div class="card sec"><h3 class="mh3">Lançamentos</h3><div class="scroll" style="max-height:46vh" id="fo-mLanc"></div></div>`;
  const md = $('#modal'); md.classList.add('on'); md.setAttribute('aria-hidden','false'); $('#mclose').focus();
  $('#fo-goL').onclick = ()=>{ closeModal(); goLanc({fk:o.k, fname:o.name}); };
  barLine('#fo-mChart', { labels: D.ymY.map(m=>MES[+m.slice(5)-1]), bars: o.Rm, bars2: o.Cm, color:'var(--real)', line: o.B>0.5?o.Bm:null, split: D.t.closedN, h:220, aria:'Mês a mês do fornecedor',
    tip: i=>`<div class="t">${ymLabel(D.ymY[i])}</div>`+row(swc('var(--real)'),'Realizado',brl0(o.Rm[i]))+row(swc('var(--fcst)'),'A lançar',brl0(o.Cm[i]))+row(swl('var(--base)'),'Orçado',brl0(o.Bm[i])) });
  table('#fo-mCC', [ {l:'CC', f:ccCell}, {l:'Orçado', n:1, f:x=>x.B?short(x.B):'—'}, {l:'Realizado', n:1, f:x=>short(x.R)}, {l:'A lançar', n:1, f:x=>x.C?short(x.C):'—'}, {l:'Saldo', n:1, f:x=>vCell(x.B-x.R-x.C)} ], ccRows);
  if (lin.length) table('#fo-mLin', [ {l:'CC', f:x=>esc(x.b.cc)}, {l:'Conta', f:x=>`<span title="${esc(x.b.contaDesc)}">${esc(x.b.conta)}</span>`}, {l:'Descrição', f:x=>`<span class="muted">${esc(x.b.desc||x.b.forn)}</span>`}, {l:'Orçado ano', n:1, f:x=>short(x.v)} ], lin);
  else $('#fo-mLin').innerHTML = '<div class="empty">Nenhuma linha do orçamento com este fornecedor. Se ele foi orçado com outro nome, procure na aba Orçado.</div>';
  const L = o.lanc.slice().sort((a,b)=>(b.comp||'').localeCompare(a.comp||'') || b.valor-a.valor);
  table('#fo-mLanc', [
    {l:'Comp.', f:l=>ymLabel(l.comp)},
    {l:'Status', f:l=>{ const late = l.classif!=='Realizado' && ((l.comp && l.comp<=lc) || (l.venc && l.venc<today)); return `<span class="pill ${l.classif==='Realizado'?'ok':(late?'late':'pend')}">${esc(l.status)}</span>`; }},
    {l:'Descrição', f:l=>`<span class="muted">${esc(l.desc)}</span>`}, {l:'CC', f:l=>esc(l.cc)}, {l:'Filial', f:l=>esc(l.divDesc)},
    {l:'Pedido', f:l=>`<span class="muted">${esc(l.pedido)}</span>`}, {l:'NF', f:l=>`<span class="muted">${esc(l.nf)}</span>`}, {l:'Venc.', f:l=>dBR(l.venc)}, {l:'Valor', n:1, f:l=>brl(l.valor)}
  ], L, { foot:[`${L.length} item(ns)`,'','','','','','','', brl(sum(L.map(l=>l.valor)))] });
}


/* ---------- eventos ---------- */
$$('#tabs button').forEach(b=>b.onclick=()=>{ if (b.dataset.v==='lanc' && S.view!=='lanc'){ S.lf={mes:'',fk:'',fname:''}; S.back=null; } goView(b.dataset.v); });
$$('[data-goto]').forEach(b=>b.onclick=()=>goView(b.dataset.goto));
{ const pop = $('#fpop'), bf = $('#btnFilt');
  const setPop = on => { pop.hidden = !on; bf.setAttribute('aria-expanded', on?'true':'false'); };
  bf.onclick = e=>{ e.stopPropagation(); setPop(pop.hidden); };
  document.addEventListener('click', e=>{ if (!pop.hidden && !e.target.closest('#filters')) setPop(false); });
  document.addEventListener('keydown', e=>{ if (e.key==='Escape') setPop(false); }); }
$('#btnBack').onclick = goBack;
['#g-prior','#d-prior'].forEach(s=>{ const el=$(s); el.onclick=()=>openPriorModal(); el.onkeydown=e=>{ if(e.key==='Enter'||e.key===' '){ e.preventDefault(); openPriorModal(); } }; });
$('#mclose').onclick = closeModal;
$('#modal').onclick = e=>{ if (e.target.id==='modal') closeModal(); };
document.addEventListener('keydown', e=>{ if (e.key==='Escape') closeModal(); });
$('#fCC').onchange = e=>{ S.cc=e.target.value; buildFilters(); render(); };
$('#fDiv').onchange = e=>{ S.div=e.target.value; buildFilters(); render(); };
$('#fTipo').onchange = e=>{ S.tipo=e.target.value; buildFilters(); render(); };
$('#fConta').onchange = e=>{ S.conta=e.target.value; buildFilters(); render(); };
$('#fStatus').onchange = e=>{ S.status=e.target.value; renderLanc(); };
$('#p-st').onchange = e=>{ S.pst=e.target.value; renderProvList(); };
let qT; $('#q').oninput = e=>{ clearTimeout(qT); qT=setTimeout(()=>{ S.q=e.target.value; S.lancLimit=200; renderLanc(); },180); };
let oT; $('#o-q').oninput = e=>{ clearTimeout(oT); oT=setTimeout(()=>{ S.oq=e.target.value; S.oLimit=150; renderOrcLines(IDX.D); },180); };
let foT; $('#fo-q').oninput = e=>{ clearTimeout(foT); foT=setTimeout(()=>{ S.fo.q=e.target.value; S.fo.limit=100; renderFornTable(); },180); };
let pT; $('#p-q').oninput = e=>{ clearTimeout(pT); pT=setTimeout(()=>{ S.pq=e.target.value; renderProvList(); },180); };
$('#c-st').onchange = e=>{ S.cst=e.target.value; renderConf(IDX.D); };
let cT; $('#c-q').oninput = e=>{ clearTimeout(cT); cT=setTimeout(()=>{ S.cq=e.target.value; renderConfList(); },180); };
$('#btnClear').onclick = ()=>{ Object.assign(S,{grps:[],cc:'',div:'',tipo:'',conta:'',ex:true,oq:'',pq:'',pst:'',cq:'',cst:'',gest:'',mes:''}); S.fo.q=''; S.fo.seg='all'; ['#q','#o-q','#p-q','#fo-q','#c-q'].forEach(s=>$(s).value=''); buildFilters(); render(); };
$('#gMes').onchange = e=>{ S.mes = e.target.value; render(); };
$('#gGest').onchange = e=>{ S.gest = e.target.value; S.cc = ''; buildFilters(); render(); };
$('#gCC').onchange = e=>{ S.cc = e.target.value; buildFilters(); render(); };
$('#btnPdf').onclick = ()=>exportResumo();
$('#btnTheme').onclick = ()=>{ const r=document.documentElement; const dark = r.dataset.theme ? r.dataset.theme==='dark' : matchMedia('(prefers-color-scheme: dark)').matches; r.dataset.theme = dark?'light':'dark'; store.set('fcxDash.theme', r.dataset.theme); render(); };
try{ localStorage.removeItem('fcxDash.snaps'); }catch(e){}
const th = store.get('fcxDash.theme'); if (th) document.documentElement.dataset.theme = th;
const sv = store.get('fcxDash.view'); if (sv && ['geral','orc','desp','prov','forn','fcst','lanc','conf'].includes(sv)) S.view = sv;
let rT; window.addEventListener('resize', ()=>{ clearTimeout(rT); rT=setTimeout(()=>M&&render(),150); });

function showErr(msg){ const e=$('#err'); e.textContent = msg; e.style.display = msg?'block':'none'; if (msg) showOk(''); }
function showOk(msg){ const e=$('#okmsg'); if (!e) return; e.innerHTML = msg; e.style.display = msg?'block':'none'; }
const nrm = s => String(s||'').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g,'');
function setPrev(){}

// ---- Integração com o sistema (FB_APU05) ----------------------------------
// O painel roda num iframe isolado (sandbox, sem acesso à sessão do sistema): não lê arquivo nem
// chama API. O sistema (servidor) manda o modelo JÁ FILTRADO pelo perfil (o gestor só recebe os
// próprios CCs) e o perfil por mensagem; "Importar planilha" pede ao sistema para abrir a aba de importação.
const API = { url:'', hosted:true };
const irImportar = ()=> window.parent.postMessage({ type:'fcx-importar' }, '*');
['#btnLoad','#btnMain','#btnPlan','#btnSync','#btnFbl'].forEach(q=>{ const b=$(q); if (b) b.onclick = irImportar; });
// "Enviar por e-mail": o painel não fala com o servidor nem desenha o PDF (o iframe é isolado e o html2canvas
// precisa de um frame da mesma origem). Ele só MONTA o mesmo relatório do "Exportar resumo (PDF)"
// (exportResumo(true) preenche o #rel) e entrega o HTML ao sistema, que gera o PDF e abre o diálogo de envio.
if ($('#btnMail')) $('#btnMail').onclick = ()=>{
  try{
    showErr('');
    const D = IDX.D || compute(), mes = S.mes || (D.ymY.filter(m=>m<=M.meta.lastClosed).pop() || D.ymY[0]);
    const g = GESTORES.find(x=>x.k===S.gest), info = exportResumo(true);
    if (!info) throw new Error('não foi possível montar o resumo');
    const rel = $('#rel'); const relHtml = rel.innerHTML; rel.innerHTML = '';
    window.parent.postMessage({ type:'fcx-enviar-resumo', mes, mesRotulo: ymLabel(mes), gest: S.gest||'', gestNome: g ? g.nome : '', quem: info.quem,
      arquivo: ('Resumo TI - ' + info.quem + ' - ' + info.L).replace(/[\\/:*?"<>|·]/g,' ').replace(/\s+/g,' ').trim().replace('/','-') + '.pdf', relHtml }, '*');
  }catch(err){ console.error(err); showErr('Não consegui montar o resumo: '+(err.message||err)); }
};
window.addEventListener('message', e=>{
  if (e.source !== window.parent) return;
  const d = e.data; if (!d) return;
  if (d.type === 'fcx-eu'){ setAcl(d.eu); if (M) render(); return; }
  if (d.type !== 'fcx-modelo' || !d.modelo) return;
  try{ showErr(''); loadModel(d.modelo); }
  catch(err){ console.error(err); showErr('Não consegui montar o painel: '+err.message); }
});
/* ---------- fechamento automático do dia 6 ---------- */
function renderCloseBanner(){
  const el = $('#closeBanner'); if (!M || !M.meta){ el.style.display='none'; return; }
  const now = new Date(), d = now.getDate(), cur = now.getFullYear()+'-'+String(now.getMonth()+1).padStart(2,'0');
  const prev = monthAdd(cur,-1), corte = new Date(now.getFullYear(), now.getMonth(), 6);
  const f = M.meta.fechamento || null;                       // servidor: {mes, em, planilha_enviada_em}
  const parsed = M.meta.parsedAt ? new Date(M.meta.parsedAt) : null;
  const plan = (M.meta.sources && M.meta.sources.main) || {};
  const planTxt = f && f.planilha_enviada_em ? 'planilha enviada em '+new Date(f.planilha_enviada_em).toLocaleString('pt-BR') : (plan.mtime ? 'planilha de '+new Date(plan.mtime).toLocaleString('pt-BR') : 'última planilha');
  let cls = '', html = '';
  if (d < 6){
    cls = 'due'; html = `<b>Fechamento de ${ymLabel(prev)}: dia 6, automático.</b> Até o dia 5 confira as MIROs e publique a planilha: retroativos (data de ${ymLabel(prev)}) lançados até o dia 5 ficam em ${ymLabel(prev)}; lançamentos com data de ${ymLabel(cur)} e tudo que entrar no SAP do dia 6 em diante contam em ${ymLabel(cur)}. No dia 6 o e-mail de fechamento vai para Welton e Wanessa.`;
  } else if (f && f.mes === prev && f.status === 'aguardando_planilha'){
    cls = 'due'; html = `<b>${ymLabel(prev)}: SAP fechado em ${new Date(f.em).toLocaleString('pt-BR')}.</b> Falta a planilha atualizada para concluir o fechamento. Se não for publicada até 23:59 do dia 6, o mês fecha com a ${planTxt}.`;
  } else if (f && f.mes === prev){
    cls = 'ok'; html = `<b>${ymLabel(prev)} fechado</b> em ${new Date(f.em).toLocaleString('pt-BR')} com a ${planTxt}. Lançamentos posteriores ao corte contam em ${ymLabel(cur)}.`;
  } else if (!API.url && parsed && parsed >= corte){
    cls = 'ok'; html = `<b>${ymLabel(prev)} fechado</b> (dia 6) · painel sincronizado em ${parsed.toLocaleString('pt-BR')} com a ${planTxt}.`;
  } else {
    cls = 'due'; html = `<b>${ymLabel(prev)} fechou no dia 6.</b> ${API.url?'O servidor fecha automaticamente com a última planilha publicada.':''} Publique a planilha mais recente para consolidar o fechamento.`;
  }
  const need = cls === 'due';
  el.className = 'banner-close ' + cls; el.style.display = 'flex';
  el.innerHTML = `<span>${html}</span>${need?'<button class="btn primary" id="cbGo">Importar planilha</button>':''}`;
  if (need && $('#cbGo')) $('#cbGo').onclick = irImportar;
}


window.parent.postMessage({ type:'fcx-pronto' }, '*');
})();
