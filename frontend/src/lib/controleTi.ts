// Controle TI — tipos do modelo (GET /api/ti/modelo) e formatação pt-BR. O cálculo
// dos gráficos e indicadores é do painel (frontend/public/painel-ti/), que usa o
// mesmo código do painel HTML de 2026.

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
