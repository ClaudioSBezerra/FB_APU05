import { useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Upload } from 'lucide-react';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { cn } from '@/lib/utils';
import {
  GRUPOS,
  SEM_FILTRO,
  anosDo,
  brl,
  brl0,
  calcular,
  curto,
  dataBR,
  passa,
  pct,
  rotuloMes,
  type Filtros,
  type TiLanc,
  type TiModelo,
} from '@/lib/controleTi';

// Controle TI — orçado × realizado × a lançar × projeção das despesas de TI
// (substitui a planilha FC FY27 Forecast + painel HTML de 2026). Os números
// vêm de GET /api/ti/modelo; o cálculo é o mesmo do painel antigo
// (lib/controleTi.ts, conferido contra ele).

async function pedir<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error((data as { error?: string })?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

interface ResumoImportacao {
  parametros: Record<string, number>;
  orcamento_linhas: number;
  meses: number;
  primeiro_mes: string;
  ultimo_mes: string;
  lancamentos: number;
}

type Aba = 'geral' | 'lancamentos' | 'importar';

const ABAS: { id: Aba; rotulo: string; ajuda: string }[] = [
  { id: 'geral', rotulo: 'Visão geral', ajuda: 'o olhar rápido' },
  { id: 'lancamentos', rotulo: 'Lançamentos', ajuda: 'nota a nota' },
  { id: 'importar', rotulo: 'Importar planilha', ajuda: 'carga do Excel' },
];

const TODOS = '__todos__';

function FiltroSelect({
  rotulo,
  valor,
  onChange,
  opcoes,
}: {
  rotulo: string;
  valor: string;
  onChange: (v: string) => void;
  opcoes: { valor: string; rotulo: string }[];
}) {
  return (
    <div className="grid min-w-0 gap-1">
      <Label className="text-xs font-normal text-fg-helper">{rotulo}</Label>
      <Select value={valor || TODOS} onValueChange={(v) => onChange(v === TODOS ? '' : v)}>
        <SelectTrigger className="h-9 text-[13px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent className="max-h-72">
          <SelectItem value={TODOS}>Todos</SelectItem>
          {opcoes.map((o) => (
            <SelectItem key={o.valor} value={o.valor}>
              {o.rotulo}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function Indicador({
  rotulo,
  valor,
  detalhe,
  titulo,
  tom,
}: {
  rotulo: string;
  valor: string;
  detalhe: React.ReactNode;
  titulo?: string;
  tom?: 'bom' | 'ruim';
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border border-border bg-card px-4 py-3" title={titulo}>
      <span className="truncate text-xs text-fg-helper">{rotulo}</span>
      <span
        className={cn(
          'text-xl font-semibold tabular-nums',
          tom === 'bom' && 'text-success-text',
          tom === 'ruim' && 'text-danger-text',
          !tom && 'text-foreground',
        )}
      >
        {valor}
      </span>
      <span className="text-xs text-fg-helper">{detalhe}</span>
    </div>
  );
}

function VisaoGeral({ modelo }: { modelo: TiModelo }) {
  const anos = useMemo(() => anosDo(modelo), [modelo]);
  const anoInicial = anos.includes(modelo.meta.lastClosed.slice(0, 4)) ? modelo.meta.lastClosed.slice(0, 4) : anos[0] ?? '';
  const [f, setF] = useState<Filtros>({ ano: anoInicial, ...SEM_FILTRO });
  const set = (parcial: Partial<Filtros>) => setF((atual) => ({ ...atual, ...parcial }));

  const D = useMemo(() => calcular(modelo, f), [modelo, f]);
  const t = D.t;
  const lc = modelo.meta.lastClosed;

  // Opções dos filtros saem dos próprios dados (sem o CC fora do painel).
  const opcoes = useMemo(() => {
    const linhas = [...modelo.budget, ...modelo.lanc].filter((r) => passa(r, { ano: f.ano, ...SEM_FILTRO }));
    const unicos = (chave: (r: (typeof linhas)[number]) => [string, string]) => {
      const m = new Map<string, string>();
      for (const r of linhas) {
        const [v, rot] = chave(r);
        if (v && !m.has(v)) m.set(v, rot);
      }
      return [...m.entries()].map(([valor, rotulo]) => ({ valor, rotulo })).sort((a, b) => a.rotulo.localeCompare(b.rotulo, 'pt-BR'));
    };
    return {
      cc: unicos((r) => [r.cc, `${r.cc} — ${r.ccDesc}`]),
      div: unicos((r) => [r.div, `${r.div} — ${r.divDesc}`]),
      tipo: unicos((r) => [r.tipo, r.tipo]),
      conta: unicos((r) => [r.conta, `${r.conta} — ${r.contaDesc}`]),
    };
  }, [modelo, f.ano]);

  const filtrado = Boolean(f.grp || f.cc || f.div || f.tipo || f.conta);
  const exec = t.Bytd ? t.Rytd / t.Bytd : null;
  const sobra = t.B - t.CF;
  const livre = t.B - t.R - t.C;
  const desvio = t.B ? (t.CF - t.B) / t.B : null;

  return (
    <div className="flex flex-col gap-4">
      <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4" aria-label="Filtros">
        <div className="flex flex-wrap items-center gap-2">
          <span className="mr-1 text-xs text-fg-helper">Ano</span>
          {anosDo(modelo).map((a) => (
            <button
              key={a}
              type="button"
              onClick={() => set({ ano: a })}
              aria-pressed={f.ano === a}
              className={cn(
                'h-8 rounded-md border px-3 text-[13px] transition-colors duration-150',
                f.ano === a
                  ? 'border-[var(--blue-l650)] bg-[var(--blue-l950)] font-medium text-[var(--blue-d250)]'
                  : 'border-input bg-card text-fg-secondary hover:bg-border',
              )}
            >
              {a}
            </button>
          ))}
          {filtrado && (
            <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setF({ ano: f.ano, ...SEM_FILTRO })}>
              Limpar filtros
            </Button>
          )}
        </div>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
          <FiltroSelect
            rotulo="Empresa"
            valor={f.grp}
            onChange={(v) => set({ grp: v })}
            opcoes={GRUPOS.map((g) => ({ valor: g.k, rotulo: g.l }))}
          />
          <FiltroSelect rotulo="Centro de custo" valor={f.cc} onChange={(v) => set({ cc: v })} opcoes={opcoes.cc} />
          <FiltroSelect rotulo="Filial" valor={f.div} onChange={(v) => set({ div: v })} opcoes={opcoes.div} />
          <FiltroSelect rotulo="Tipo de despesa" valor={f.tipo} onChange={(v) => set({ tipo: v })} opcoes={opcoes.tipo} />
          <FiltroSelect rotulo="Conta contábil" valor={f.conta} onChange={(v) => set({ conta: v })} opcoes={opcoes.conta} />
        </div>
      </section>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-6" aria-label="Indicadores">
        <Indicador
          rotulo={`Orçado ${f.ano}`}
          valor={curto(t.B)}
          titulo={brl(t.B)}
          detalhe={`${brl0(t.B / 12)} por mês, em média`}
        />
        <Indicador
          rotulo={`Projeção ${f.ano}`}
          valor={curto(t.CF)}
          titulo={brl(t.CF)}
          detalhe={
            desvio == null ? 'sem orçado' : `${desvio > 0 ? '▲' : desvio < 0 ? '▼' : '●'} ${pct(Math.abs(desvio))} ${desvio > 0 ? 'acima' : 'abaixo'} do orçado`
          }
        />
        <Indicador
          rotulo="Deve sobrar do orçado"
          valor={curto(sobra)}
          titulo={brl(sobra)}
          tom={sobra >= 0 ? 'bom' : 'ruim'}
          detalhe={sobra >= 0 ? 'orçado − projeção: abaixo do orçado' : 'projeção acima do orçado'}
        />
        {t.fechados === 0 ? (
          <Indicador rotulo={`Realizado ${f.ano}`} valor="—" detalhe="ano ainda sem meses fechados" />
        ) : (
          <Indicador
            rotulo={`Realizado até ${rotuloMes(t.fechados === 12 ? `${f.ano}-12` : lc)}`}
            valor={curto(t.Rytd)}
            titulo={brl(t.Rytd)}
            detalhe={`${pct(exec)} do orçado do período (${curto(t.Bytd)})`}
          />
        )}
        <Indicador
          rotulo="A lançar no ano"
          valor={curto(t.C)}
          titulo={brl(t.C)}
          detalhe={t.Copen > 0 ? `▲ ${curto(t.Copen)} em meses já fechados` : 'nada pendente em mês fechado'}
        />
        <Indicador
          rotulo="Saldo disponível"
          valor={curto(livre)}
          titulo={brl(livre)}
          tom={livre >= 0 ? undefined : 'ruim'}
          detalhe="orçado − realizado − a lançar"
        />
      </div>

      <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
        <div>
          <h2 className="text-base font-semibold text-foreground">Mês a mês</h2>
          <p className="text-xs text-fg-helper">
            Meses fechados (até {rotuloMes(lc)}) mostram o realizado; os em aberto, a projeção. Sobra = orçado − projeção.
          </p>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Mês</TableHead>
              <TableHead className="text-right">Orçado</TableHead>
              <TableHead className="text-right">Realizado</TableHead>
              <TableHead className="text-right">A lançar</TableHead>
              <TableHead className="text-right">Projeção</TableHead>
              <TableHead className="text-right">Sobra</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {D.meses.map((ym, i) => (
              <TableRow key={ym} className={ym > lc ? 'text-fg-secondary' : undefined}>
                <TableCell>
                  {rotuloMes(ym)}
                  {ym <= lc && <span className="ml-1.5 text-xs text-success-text" title="mês fechado">fechado</span>}
                </TableCell>
                <Num v={D.mB[i]} />
                <Num v={D.mR[i]} />
                <Num v={D.mC[i]} />
                <Num v={D.mCF[i]} />
                <Num v={D.mB[i] - D.mCF[i]} sinal />
              </TableRow>
            ))}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell>Total</TableCell>
              <Num v={t.B} />
              <Num v={t.R} />
              <Num v={t.C} />
              <Num v={t.CF} />
              <Num v={t.B - t.CF} sinal />
            </TableRow>
          </TableFooter>
        </Table>
      </section>

      <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
        <div>
          <h2 className="text-base font-semibold text-foreground">Por centro de custo</h2>
          <p className="text-xs text-fg-helper">% usado = (realizado + a lançar) ÷ orçado. Em vermelho: acima do orçado ou sem orçamento.</p>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Centro de custo</TableHead>
              <TableHead className="text-right">Orçado</TableHead>
              <TableHead className="text-right">Realizado</TableHead>
              <TableHead className="text-right">A lançar</TableHead>
              <TableHead className="text-right">Projeção</TableHead>
              <TableHead className="text-right">% usado</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {D.porCC.map((o) => {
              const usado = o.B ? (o.R + o.C) / o.B : null;
              const estourou = o.B ? o.R + o.C > o.B : o.R + o.C > 0.5;
              return (
                <TableRow key={o.cc}>
                  <TableCell>
                    <span className="font-medium">{o.cc}</span> <span className="text-fg-helper">{o.desc}</span>
                  </TableCell>
                  <Num v={o.B} />
                  <Num v={o.R} />
                  <Num v={o.C} />
                  <Num v={o.CF} />
                  <TableCell className={cn('text-right tabular-nums', estourou && 'font-medium text-danger-text')}>{pct(usado)}</TableCell>
                </TableRow>
              );
            })}
            {D.porCC.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="py-6 text-center text-fg-helper">
                  Nenhum dado para estes filtros.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </section>
    </div>
  );
}

function Num({ v, sinal = false }: { v: number; sinal?: boolean }) {
  return (
    <TableCell className={cn('text-right tabular-nums', sinal && v < -0.5 && 'text-danger-text')}>
      {Math.abs(v) < 0.5 ? '—' : brl0(v)}
    </TableCell>
  );
}

const VARIANTE_STATUS: Record<string, BadgeProps['variant']> = {
  'LANÇADO': 'default',
  'A LANÇAR': 'warning',
  'ESTORNADO': 'destructive',
};

function Lancamentos({ modelo }: { modelo: TiModelo }) {
  const [busca, setBusca] = useState('');
  const [status, setStatus] = useState('');
  const [mes, setMes] = useState('');
  const [cc, setCc] = useState('');
  const [limite, setLimite] = useState(200);

  const opcoes = useMemo(() => {
    const unicos = (xs: string[]) => [...new Set(xs.filter(Boolean))].sort();
    return {
      status: unicos(modelo.lanc.map((l) => l.status)),
      meses: unicos(modelo.lanc.map((l) => l.comp ?? '')).reverse(),
      cc: [...new Map(modelo.lanc.filter((l) => l.cc).map((l) => [l.cc, `${l.cc} — ${l.ccDesc}`])).entries()]
        .map(([valor, rotulo]) => ({ valor, rotulo }))
        .sort((a, b) => a.valor.localeCompare(b.valor)),
    };
  }, [modelo]);

  const linhas = useMemo(() => {
    const q = busca.trim().toLowerCase();
    return modelo.lanc
      .filter((l) => l.cc !== '6600')
      .filter((l) => !status || l.status === status)
      .filter((l) => !mes || l.comp === mes)
      .filter((l) => !cc || l.cc === cc)
      .filter(
        (l) =>
          !q ||
          l.forn.toLowerCase().includes(q) ||
          l.desc.toLowerCase().includes(q) ||
          l.pedido.includes(q) ||
          l.nf.toLowerCase().includes(q) ||
          l.miro.includes(q),
      )
      .sort((a, b) => (b.comp ?? '').localeCompare(a.comp ?? '') || Number(b.id) - Number(a.id));
  }, [modelo, busca, status, mes, cc]);

  const total = linhas.reduce((acc, l) => acc + l.valor, 0);

  return (
    <section className="flex flex-col gap-4 rounded-lg border border-border bg-card p-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div className="grid gap-1">
          <Label htmlFor="ti-busca" className="text-xs font-normal text-fg-helper">
            Buscar
          </Label>
          <Input
            id="ti-busca"
            className="h-9 text-[13px]"
            placeholder="Fornecedor, descrição, pedido, NF ou MIRO"
            value={busca}
            onChange={(e) => {
              setBusca(e.target.value);
              setLimite(200);
            }}
          />
        </div>
        <FiltroSelect rotulo="Status" valor={status} onChange={(v) => { setStatus(v); setLimite(200); }} opcoes={opcoes.status.map((s) => ({ valor: s, rotulo: s }))} />
        <FiltroSelect rotulo="Competência" valor={mes} onChange={(v) => { setMes(v); setLimite(200); }} opcoes={opcoes.meses.map((m) => ({ valor: m, rotulo: rotuloMes(m) }))} />
        <FiltroSelect rotulo="Centro de custo" valor={cc} onChange={(v) => { setCc(v); setLimite(200); }} opcoes={opcoes.cc} />
      </div>

      <p className="text-xs text-fg-helper" aria-live="polite">
        {linhas.length.toLocaleString('pt-BR')} {linhas.length === 1 ? 'lançamento' : 'lançamentos'} · total {brl(total)}
      </p>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Competência</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Fornecedor</TableHead>
            <TableHead>Descrição</TableHead>
            <TableHead>CC</TableHead>
            <TableHead>Pedido</TableHead>
            <TableHead>Vencimento</TableHead>
            <TableHead className="text-right">Valor</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {linhas.slice(0, limite).map((l: TiLanc) => (
            <TableRow key={l.id}>
              <TableCell className="whitespace-nowrap tabular-nums">{l.comp ? rotuloMes(l.comp) : '—'}</TableCell>
              <TableCell>
                <Badge variant={VARIANTE_STATUS[l.status] ?? 'info'}>{l.status || '—'}</Badge>
              </TableCell>
              <TableCell className="max-w-[180px] truncate" title={l.forn}>{l.forn}</TableCell>
              <TableCell className="max-w-[200px] truncate text-fg-secondary" title={l.desc}>{l.desc}</TableCell>
              <TableCell className="whitespace-nowrap" title={l.ccDesc}>{l.cc || '—'}</TableCell>
              <TableCell className="tabular-nums">{l.pedido || '—'}</TableCell>
              <TableCell className="whitespace-nowrap tabular-nums">{dataBR(l.venc)}</TableCell>
              <TableCell className={cn('whitespace-nowrap text-right tabular-nums', l.valor < 0 && 'text-danger-text')}>{brl(l.valor)}</TableCell>
            </TableRow>
          ))}
          {linhas.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={8} className="py-6 text-center text-fg-helper">
                Nenhum lançamento para estes filtros.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      {linhas.length > limite && (
        <Button variant="outline" size="sm" className="self-center" onClick={() => setLimite((n) => n + 200)}>
          Mostrar mais ({(linhas.length - limite).toLocaleString('pt-BR')} restantes)
        </Button>
      )}
    </section>
  );
}

function Importar({ modelo }: { modelo: TiModelo }) {
  const queryClient = useQueryClient();
  const entrada = useRef<HTMLInputElement>(null);
  const [arquivo, setArquivo] = useState<File | null>(null);
  const [confirmando, setConfirmando] = useState(false);
  const [resumo, setResumo] = useState<ResumoImportacao | null>(null);

  const importar = useMutation({
    mutationFn: async (file: File) => {
      const corpo = new FormData();
      corpo.append('arquivo', file);
      return pedir<ResumoImportacao>('/api/ti/importar', { method: 'POST', body: corpo });
    },
    onSuccess: (r) => {
      setResumo(r);
      setArquivo(null);
      if (entrada.current) entrada.current.value = '';
      queryClient.invalidateQueries({ queryKey: ['ti-modelo'] });
      toast.success('Planilha importada');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const quando = modelo.meta.importadoEm
    ? new Date(modelo.meta.importadoEm).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
    : null;

  return (
    <section className="flex max-w-2xl flex-col gap-4 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-base font-semibold text-foreground">Importar planilha</h2>
        <p className="mt-1 text-sm text-fg-secondary">
          Carrega a planilha <strong>FC FY27 Forecast</strong> (.xlsx) com as abas Parâmetros, Orçamento Forecast e Lançamentos
          Mensais. Cada importação <strong>substitui</strong> os parâmetros, o orçamento e os lançamentos que vieram de planilha.
          Lançamentos digitados no sistema ou recebidos do SAP não são alterados.
        </p>
      </div>

      <p className="text-xs text-fg-helper">
        {quando ? (
          <>
            Última importação: <span className="font-medium text-fg-secondary">{modelo.meta.fileName || 'arquivo sem nome'}</span> em {quando}.
          </>
        ) : (
          'Nenhuma planilha importada ainda.'
        )}
      </p>

      <div className="grid gap-1.5">
        <Label htmlFor="ti-arquivo">Planilha (.xlsx, até 25 MB)</Label>
        <Input
          id="ti-arquivo"
          ref={entrada}
          type="file"
          accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          onChange={(e) => setArquivo(e.target.files?.[0] ?? null)}
        />
      </div>

      <Button className="self-start" disabled={!arquivo || importar.isPending} onClick={() => setConfirmando(true)}>
        <Upload />
        {importar.isPending ? 'Importando...' : 'Importar planilha'}
      </Button>

      {resumo && (
        <div role="status" className="rounded-md border border-success-border bg-success-bg px-4 py-3 text-sm text-success-text">
          <p className="font-medium">Importação concluída</p>
          <ul className="mt-1 list-inside list-disc text-[13px]">
            <li>{resumo.orcamento_linhas.toLocaleString('pt-BR')} linhas de orçamento ({resumo.meses} meses, {rotuloMes(resumo.primeiro_mes)} a {rotuloMes(resumo.ultimo_mes)})</li>
            <li>{resumo.lancamentos.toLocaleString('pt-BR')} lançamentos</li>
            <li>
              Parâmetros: {resumo.parametros.centros_custo} centros de custo, {resumo.parametros.contas} contas, {resumo.parametros.divisoes} divisões,{' '}
              {resumo.parametros.status} status
            </li>
          </ul>
        </div>
      )}

      <Dialog open={confirmando} onOpenChange={setConfirmando}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Substituir os dados da planilha?</DialogTitle>
            <DialogDescription>
              Parâmetros, orçamento e lançamentos vindos de planilha serão trocados pelo conteúdo de <strong>{arquivo?.name}</strong>. Se algo
              falhar, nada é alterado.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmando(false)}>
              Cancelar
            </Button>
            <Button
              onClick={() => {
                if (arquivo) importar.mutate(arquivo);
                setConfirmando(false);
              }}
            >
              Substituir
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export default function ControleTI() {
  const [aba, setAba] = useState<Aba>('geral');
  const { data: modelo, error, isLoading } = useQuery({
    queryKey: ['ti-modelo'],
    queryFn: () => pedir<TiModelo>('/api/ti/modelo'),
  });

  const vazio = modelo && modelo.budget.length === 0 && modelo.lanc.length === 0;

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-foreground">Controle TI</h1>
          <p className="text-xs text-fg-helper">Despesas de TI: orçado, realizado, a lançar e projeção.</p>
        </div>
        {modelo && (
          <p className="text-xs text-fg-helper">
            Mês fechado: <span className="font-medium text-fg-secondary">{rotuloMes(modelo.meta.lastClosed)}</span> · fecha sozinho a partir do dia 6
          </p>
        )}
      </div>

      <div role="tablist" aria-label="Seções do Controle TI" className="flex flex-wrap gap-2">
        {ABAS.map((a) => (
          <button
            key={a.id}
            role="tab"
            type="button"
            aria-selected={aba === a.id}
            onClick={() => setAba(a.id)}
            className={cn(
              'rounded-md border px-3 py-1.5 text-left text-[13px] transition-colors duration-150',
              aba === a.id
                ? 'border-[var(--blue-l650)] bg-[var(--blue-l950)] font-medium text-[var(--blue-d250)]'
                : 'border-input bg-card text-fg-secondary hover:bg-border',
            )}
          >
            {a.rotulo} <span className="ml-1 text-xs font-normal text-fg-muted">{a.ajuda}</span>
          </button>
        ))}
      </div>

      {isLoading && <p className="text-sm text-fg-helper">Carregando...</p>}
      {error && (
        <p role="alert" className="rounded-md border border-danger-border bg-danger-bg px-4 py-3 text-sm text-danger-text">
          Não foi possível carregar o Controle TI. {(error as Error).message}
        </p>
      )}

      {modelo && vazio && aba !== 'importar' && (
        <div className="flex flex-col items-start gap-3 rounded-lg border border-border bg-card p-6">
          <p className="text-sm text-fg-secondary">Ainda não há dados. Importe a planilha FC FY27 Forecast para começar.</p>
          <Button onClick={() => setAba('importar')}>
            <Upload />
            Importar planilha
          </Button>
        </div>
      )}

      {modelo && !vazio && aba === 'geral' && <VisaoGeral modelo={modelo} />}
      {modelo && !vazio && aba === 'lancamentos' && <Lancamentos modelo={modelo} />}
      {modelo && aba === 'importar' && <Importar modelo={modelo} />}
    </div>
  );
}
