// Controle TI — tipos do modelo (GET /api/ti/modelo) e o cálculo da visão geral.
// É o porte do `compute` do painel HTML de 2026, com as mesmas regras:
//   - CC 6600 (Lei do Bem) fica fora do painel;
//   - realizado = lançamentos classificados como "Realizado"; a lançar = o resto;
//   - projeção: mês fechado = realizado, mês aberto = maior entre orçado e lançado
//     (a regra já vem aplicada em `cf`, calculada no servidor);
//   - sobra = orçado − projeção; saldo disponível = orçado − realizado − a lançar.

export interface TiMeta {
  fileName: string;
  lastClosed: string; // AAAA-MM
  refDate: string;
  parsedAt: string;
  months: string[]; // AAAA-MM, mesmo índice dos arrays `m`
  importadoEm: string; // última importação (RFC 3339); vazio se nunca houve
}

export interface TiBudget {
  id: string;
  origem: string;
  div: string;
  cc: string;
  ccDesc: string;
  conta: string;
  contaDesc: string;
  tipo: string;
  forn: string;
  desc: string;
  m: number[];
  divDesc: string;
  grp: string;
}

export interface TiLanc {
  id: string;
  origem: string;
  status: string;
  pedido: string;
  folha: string;
  miro: string;
  nf: string;
  comp: string | null; // AAAA-MM
  venc: string | null; // AAAA-MM-DD
  valor: number;
  forn: string;
  desc: string;
  div: string;
  divDesc: string;
  cc: string;
  ccDesc: string;
  conta: string;
  contaDesc: string;
  tipo: string;
  classif: string;
  ccSap: string;
  grp: string;
}

export interface TiCF {
  key: string;
  div: string;
  cc: string;
  ccDesc: string;
  conta: string;
  contaDesc: string;
  tipo: string;
  divDesc: string;
  grp: string;
  m: number[];
}

export interface TiModelo {
  meta: TiMeta;
  budget: TiBudget[];
  cf: TiCF[];
  lanc: TiLanc[];
}

export const GRUPOS = [
  { k: 'FC', l: 'Ferreira Costa' },
  { k: 'RX', l: 'RetailX' },
  { k: 'IMP', l: 'Importadoras' },
  { k: 'OUT', l: 'Sem divisão' },
] as const;

export interface Filtros {
  ano: string;
  grp: string; // '' = todos
  cc: string;
  div: string;
  tipo: string;
  conta: string;
}

export const SEM_FILTRO: Omit<Filtros, 'ano'> = { grp: '', cc: '', div: '', tipo: '', conta: '' };

// Lei do Bem: fora do painel (regra de negócio do Welton).
const CC_FORA = '6600';

type ComDimensoes = { cc: string; div: string; tipo: string; conta: string; grp: string };

export function passa(r: ComDimensoes, f: Filtros): boolean {
  if (r.cc === CC_FORA) return false;
  if (f.grp && r.grp !== f.grp) return false;
  if (f.cc && r.cc !== f.cc) return false;
  if (f.div && r.div !== f.div) return false;
  if (f.tipo && r.tipo !== f.tipo) return false;
  if (f.conta && r.conta !== f.conta) return false;
  return true;
}

const soma = (xs: number[]) => xs.reduce((a, b) => a + b, 0);

export interface Totais {
  B: number; // orçado do ano
  CF: number; // projeção do ano
  R: number; // realizado do ano
  C: number; // a lançar do ano
  Bytd: number; // orçado até o último mês fechado
  Rytd: number; // realizado até o último mês fechado
  Copen: number; // a lançar em meses já fechados (atrasado)
  fechados: number; // meses fechados no ano
}

export interface PorCC {
  cc: string;
  desc: string;
  B: number;
  R: number;
  C: number;
  CF: number;
}

export interface Calculo {
  ano: string;
  meses: string[]; // AAAA-MM do ano
  mB: number[];
  mCF: number[];
  mR: number[];
  mC: number[];
  t: Totais;
  porCC: PorCC[];
}

export function anosDo(m: TiModelo): string[] {
  return [...new Set(m.meta.months.map((x) => x.slice(0, 4)))];
}

export function calcular(m: TiModelo, f: Filtros): Calculo {
  const lc = m.meta.lastClosed;
  const idx = m.meta.months.map((ym, i) => (ym.startsWith(f.ano) ? i : -1)).filter((i) => i >= 0);
  const meses = idx.map((i) => m.meta.months[i]);
  const fechados = meses.filter((ym) => ym <= lc).length;

  const B = m.budget.filter((r) => passa(r, f));
  const CF = m.cf.filter((r) => passa(r, f));
  const L = m.lanc.filter((r) => passa(r, f) && r.comp && r.comp.startsWith(f.ano));

  const mB = idx.map((i) => soma(B.map((b) => b.m[i])));
  const mCF = idx.map((i) => soma(CF.map((c) => c.m[i])));
  const mR = meses.map(() => 0);
  const mC = meses.map(() => 0);
  for (const l of L) {
    const k = meses.indexOf(l.comp as string);
    if (k < 0) continue;
    if (l.classif === 'Realizado') mR[k] += l.valor;
    else mC[k] += l.valor;
  }

  const t: Totais = {
    B: soma(mB),
    CF: soma(mCF),
    R: soma(mR),
    C: soma(mC),
    Bytd: soma(mB.slice(0, fechados)),
    Rytd: soma(mR.slice(0, fechados)),
    Copen: soma(mC.slice(0, fechados)),
    fechados,
  };

  const mapa = new Map<string, PorCC>();
  const obter = (cc: string, desc: string) => {
    let o = mapa.get(cc);
    if (!o) {
      o = { cc, desc, B: 0, R: 0, C: 0, CF: 0 };
      mapa.set(cc, o);
    }
    return o;
  };
  for (const b of B) {
    const o = obter(b.cc || 'N/D', b.ccDesc);
    for (const i of idx) o.B += b.m[i];
  }
  for (const c of CF) {
    const o = obter(c.cc || 'N/D', c.ccDesc);
    for (const i of idx) o.CF += c.m[i];
  }
  for (const l of L) {
    const o = obter(l.cc || 'N/D', l.ccDesc);
    if (l.classif === 'Realizado') o.R += l.valor;
    else o.C += l.valor;
  }
  const porCC = [...mapa.values()]
    .filter((o) => Math.abs(o.B) + Math.abs(o.R) + Math.abs(o.C) + Math.abs(o.CF) > 0.5)
    .sort((a, b) => b.B - a.B || a.cc.localeCompare(b.cc));

  return { ano: f.ano, meses, mB, mCF, mR, mC, t, porCC };
}

// ---------- formatação (pt-BR) ----------

const brl2 = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });
const brl0n = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL', maximumFractionDigits: 0 });
const num1 = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 });
const num2 = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 });

export const brl = (v: number) => brl2.format(v);
export const brl0 = (v: number) => brl0n.format(Math.round(v) === 0 ? 0 : v);

// R$ 1,24 mi / R$ 308 mil — o formato curto dos cartões.
export function curto(v: number): string {
  const a = Math.abs(v);
  const s = v < 0 ? '−' : '';
  if (a >= 1e6) return `${s}R$ ${(a >= 1e7 ? num1 : num2).format(a / 1e6)} mi`;
  if (a >= 1e3) return `${s}R$ ${(a >= 1e5 ? new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 0 }) : num1).format(a / 1e3)} mil`;
  return `${s}R$ ${new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 0 }).format(a)}`;
}

export const pct = (v: number | null | undefined, d = 1) =>
  v == null || !Number.isFinite(v)
    ? '—'
    : `${(v * 100).toLocaleString('pt-BR', { minimumFractionDigits: d, maximumFractionDigits: d })}%`;

const MESES = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];
export function rotuloMes(ym: string): string {
  const [a, m] = ym.split('-');
  return `${MESES[Number(m) - 1] ?? m}/${a.slice(2)}`;
}

export function dataBR(iso: string | null): string {
  if (!iso) return '—';
  const [a, m, d] = iso.split('-');
  return `${d}/${m}/${a}`;
}
