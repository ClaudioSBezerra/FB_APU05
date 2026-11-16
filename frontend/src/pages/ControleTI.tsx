import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { cn } from '@/lib/utils';
import { brl, curto, dataBR, rotuloMes, type TiLanc, type TiModelo } from '@/lib/controleTi';

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

interface ResumoSap {
  linhas: number;
  aproveitadas: number;
  sem_pedido: number;
  totais: number;
  primeiro_mes: string;
  ultimo_mes: string;
}

type TipoArquivo = 'forecast' | 'ksb1' | 'ksb2';

interface ResultadoArquivo {
  arquivo: string;
  tipo?: TipoArquivo;
  ok: boolean;
  erro?: string;
  forecast?: ResumoImportacao;
  sap?: ResumoSap;
}

const ROTULO_TIPO: Record<TipoArquivo, string> = {
  forecast: 'Planilha Forecast',
  ksb1: 'KSB1 · realizado',
  ksb2: 'KSB2 · compromissado',
};

type Aba = 'painel' | 'lancamentos' | 'conferencia' | 'importar';

const ABAS: { id: Aba; rotulo: string; ajuda: string }[] = [
  { id: 'painel', rotulo: 'Painel', ajuda: 'gráficos e análises' },
  { id: 'lancamentos', rotulo: 'Lançamentos', ajuda: 'nota a nota' },
  { id: 'conferencia', rotulo: 'Conferência SAP', ajuda: 'planilha × SAP' },
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
  const temSaldo = Boolean(modelo.meta.sap.ksb2Em);

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
            {temSaldo && (
              <TableHead className="text-right" title="Saldo em aberto do pedido na KSB2 (itens a lançar)">
                Saldo SAP
              </TableHead>
            )}
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
              {temSaldo && (
                <TableCell className="whitespace-nowrap text-right tabular-nums text-fg-secondary">{l.saldoSap == null ? '—' : brl(l.saldoSap)}</TableCell>
              )}
            </TableRow>
          ))}
          {linhas.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={temSaldo ? 9 : 8} className="py-6 text-center text-fg-helper">
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

// ---- Conferência SAP: planilha × KSB1 por pedido + CC + mês ----

interface ConfLinha {
  situacao: string;
  pedido: string;
  ccSap: string;
  div: string;
  cc: string;
  mes: string;
  valorPlanilha: number;
  valorSap: number;
  diferenca: number;
  valor: number;
  fornecedor: string;
  descricao: string;
  mesSap?: string;
  ids?: string[];
  acao: string;
}

interface SituacaoInfo {
  codigo: string;
  rotulo: string;
  significado: string;
  acao: string;
}

interface ConfResposta {
  resumo: {
    total: number;
    base: number;
    conferidos: number;
    pctConferido: number;
    por: Record<string, { qtd: number; valor: number }>;
    primeiroMes: string;
    ultimoMes: string;
    linhasKsb1: number;
    pedidosKsb1: number;
    pedidosSemKsb1: number;
  };
  linhas: ConfLinha[];
  situacoes: SituacaoInfo[];
}

const VARIANTE_SITUACAO: Record<string, BadgeProps['variant']> = {
  conferido: 'default',
  outro_mes: 'warning',
  valor_diferente: 'destructive',
  rateio_diferente: 'warning',
  pendente: 'info',
  nao_encontrado: 'destructive',
  so_sap: 'info',
  estorno: 'warning',
  verificar: 'secondary',
};

function ConferenciaSap({ onImportar }: { onImportar: () => void }) {
  const { data, error, isLoading } = useQuery({
    queryKey: ['ti-conferencia'],
    queryFn: () => pedir<ConfResposta>('/api/ti/conferencia'),
  });
  const [situacao, setSituacao] = useState('');
  const [busca, setBusca] = useState('');
  const [mes, setMes] = useState('');
  const [limite, setLimite] = useState(200);

  const linhas = useMemo(() => {
    if (!data) return [];
    const q = busca.trim().toLowerCase();
    return data.linhas.filter(
      (l) =>
        (!situacao || l.situacao === situacao) &&
        (!mes || l.mes === mes) &&
        (!q || l.pedido.includes(q) || l.fornecedor.toLowerCase().includes(q) || l.descricao.toLowerCase().includes(q) || l.cc.includes(q)),
    );
  }, [data, situacao, mes, busca]);

  const meses = useMemo(() => [...new Set((data?.linhas ?? []).map((l) => l.mes))].sort().reverse(), [data]);

  if (isLoading) return <p className="text-sm text-fg-helper">Carregando...</p>;
  if (error)
    return (
      <p role="alert" className="rounded-md border border-danger-border bg-danger-bg px-4 py-3 text-sm text-danger-text">
        Não foi possível carregar a conferência. {(error as Error).message}
      </p>
    );
  if (!data || data.resumo.total === 0)
    return (
      <div className="flex flex-col items-start gap-3 rounded-lg border border-border bg-card p-6">
        <p className="text-sm text-fg-secondary">
          A conferência compara o que a planilha marca como realizado com o que o SAP lançou. Para começar, importe a <strong>KSB1</strong> (acumulada do ano) junto
          da planilha Forecast.
        </p>
        <Button onClick={onImportar}>
          <Upload />
          Importar KSB1
        </Button>
      </div>
    );

  const r = data.resumo;
  const info = data.situacoes.find((x) => x.codigo === situacao);

  return (
    <div className="flex flex-col gap-4">
      <section className="flex flex-wrap items-center gap-x-8 gap-y-2 rounded-lg border border-border bg-card px-4 py-3">
        <div>
          <p className="text-xs text-fg-helper">Conferido</p>
          <p className="text-2xl font-semibold tabular-nums text-foreground">{(r.pctConferido * 100).toLocaleString('pt-BR', { maximumFractionDigits: 1 })}%</p>
          <p className="text-xs text-fg-helper">
            {r.conferidos.toLocaleString('pt-BR')} de {r.base.toLocaleString('pt-BR')} pedidos/CC/mês
          </p>
        </div>
        <div className="text-xs text-fg-helper">
          <p>
            Meses da KSB1: <span className="font-medium text-fg-secondary">{rotuloMes(r.primeiroMes)} a {rotuloMes(r.ultimoMes)}</span>
          </p>
          <p>
            {r.linhasKsb1.toLocaleString('pt-BR')} linhas · {r.pedidosKsb1.toLocaleString('pt-BR')} pedidos no SAP
          </p>
          {r.pedidosSemKsb1 > 0 && <p>{r.pedidosSemKsb1.toLocaleString('pt-BR')} pedidos da planilha não aparecem na KSB1</p>}
          <p>“Verificar” não entra na base: são pedidos fora da planilha.</p>
        </div>
      </section>

      <section aria-label="Situações" className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-5">
        {data.situacoes.map((s) => {
          const t = r.por[s.codigo] ?? { qtd: 0, valor: 0 };
          const ativo = situacao === s.codigo;
          return (
            <button
              key={s.codigo}
              type="button"
              disabled={t.qtd === 0}
              aria-pressed={ativo}
              onClick={() => {
                setSituacao(ativo ? '' : s.codigo);
                setLimite(200);
              }}
              title={`${s.significado}. ${s.acao}.`}
              className={cn(
                'flex flex-col gap-0.5 rounded-lg border px-3 py-2 text-left transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-40',
                ativo ? 'border-[var(--blue-l650)] bg-[var(--blue-l950)]' : 'border-border bg-card enabled:hover:bg-border',
              )}
            >
              <span className="text-xs text-fg-helper">{s.rotulo}</span>
              <span className="text-lg font-semibold tabular-nums text-foreground">{t.qtd.toLocaleString('pt-BR')}</span>
              <span className="text-xs tabular-nums text-fg-helper">{curto(t.valor)}</span>
            </button>
          );
        })}
      </section>

      <section className="flex flex-col gap-4 rounded-lg border border-border bg-card p-4">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
          <div className="grid gap-1">
            <Label htmlFor="conf-busca" className="text-xs font-normal text-fg-helper">
              Buscar
            </Label>
            <Input
              id="conf-busca"
              className="h-9 text-[13px]"
              placeholder="Pedido, fornecedor, descrição ou CC"
              value={busca}
              onChange={(e) => {
                setBusca(e.target.value);
                setLimite(200);
              }}
            />
          </div>
          <FiltroSelect
            rotulo="Mês"
            valor={mes}
            onChange={(v) => {
              setMes(v);
              setLimite(200);
            }}
            opcoes={meses.map((m) => ({ valor: m, rotulo: rotuloMes(m) }))}
          />
          {(situacao || mes || busca) && (
            <Button
              variant="ghost"
              size="sm"
              className="self-end justify-self-start"
              onClick={() => {
                setSituacao('');
                setMes('');
                setBusca('');
              }}
            >
              Limpar filtros
            </Button>
          )}
        </div>

        {info && (
          <p className="rounded-md bg-muted px-3 py-2 text-[13px] text-fg-secondary">
            <strong>{info.rotulo}:</strong> {info.significado}. <strong>O que fazer:</strong> {info.acao}.
          </p>
        )}

        <p className="text-xs text-fg-helper" aria-live="polite">
          {linhas.length.toLocaleString('pt-BR')} {linhas.length === 1 ? 'grupo' : 'grupos'}
        </p>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Situação</TableHead>
              <TableHead>Pedido</TableHead>
              <TableHead>CC</TableHead>
              <TableHead>Mês</TableHead>
              <TableHead>Fornecedor</TableHead>
              <TableHead className="text-right">Planilha</TableHead>
              <TableHead className="text-right">SAP</TableHead>
              <TableHead className="text-right">Diferença</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {linhas.slice(0, limite).map((l) => (
              <TableRow key={`${l.pedido}|${l.ccSap}|${l.mes}|${l.situacao}`}>
                <TableCell>
                  <Badge variant={VARIANTE_SITUACAO[l.situacao] ?? 'secondary'}>{data.situacoes.find((x) => x.codigo === l.situacao)?.rotulo ?? l.situacao}</Badge>
                </TableCell>
                <TableCell className="tabular-nums">{l.pedido}</TableCell>
                <TableCell className="whitespace-nowrap" title={l.ccSap}>
                  {l.cc}
                </TableCell>
                <TableCell className="whitespace-nowrap tabular-nums">
                  {rotuloMes(l.mes)}
                  {l.mesSap && (
                    <span className="text-fg-helper" title={`O SAP lançou em ${rotuloMes(l.mesSap)}`}>
                      {' '}
                      → {rotuloMes(l.mesSap)}
                    </span>
                  )}
                </TableCell>
                <TableCell className="max-w-[210px] truncate" title={`${l.fornecedor} — ${l.descricao}`}>
                  {l.fornecedor || '—'}
                </TableCell>
                <TableCell className="whitespace-nowrap text-right tabular-nums">{Math.abs(l.valorPlanilha) < 0.005 ? '—' : brl(l.valorPlanilha)}</TableCell>
                <TableCell className="whitespace-nowrap text-right tabular-nums">{Math.abs(l.valorSap) < 0.005 ? '—' : brl(l.valorSap)}</TableCell>
                <TableCell className={cn('whitespace-nowrap text-right tabular-nums', Math.abs(l.diferenca) >= 0.015 && 'font-medium text-danger-text')}>
                  {Math.abs(l.diferenca) < 0.015 ? '—' : brl(l.diferenca)}
                </TableCell>
              </TableRow>
            ))}
            {linhas.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={8} className="py-6 text-center text-fg-helper">
                  Nenhum grupo para estes filtros.
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
    </div>
  );
}

// Painel — os gráficos e análises do painel de despesas de TI de 2026 (7 telas:
// Visão geral, Realizado, Fornecedores, Lançamentos e as bases Orçado, A lançar e
// Projeção), levados para dentro do sistema SEM reescrever o cálculo: é o mesmo
// código do painel original, o que garante o mesmo visual e os mesmos números.
//
// Roda num <iframe sandbox="allow-scripts"> (origem opaca: não enxerga a sessão do
// sistema nem chama a API) e uma CSP restrita no nginx (sem rede). Os dados chegam
// por postMessage — o painel avisa "fcx-pronto" e recebe "fcx-modelo". Fonte da
// verdade do código: frontend/public/painel-ti/ (ver docs/referencia/04-CONTROLE-TI.md).
function Painel({ modelo, onImportar }: { modelo: TiModelo; onImportar: () => void }) {
  const frame = useRef<HTMLIFrameElement>(null);

  const enviar = useCallback(() => {
    frame.current?.contentWindow?.postMessage({ type: 'fcx-modelo', modelo }, '*');
  }, [modelo]);

  useEffect(() => {
    function aoReceber(e: MessageEvent) {
      // só mensagens do NOSSO iframe (a origem dele é opaca, então a identidade é a janela)
      if (e.source !== frame.current?.contentWindow) return;
      if (e.data?.type === 'fcx-pronto') enviar();
      if (e.data?.type === 'fcx-importar') onImportar();
    }
    window.addEventListener('message', aoReceber);
    return () => window.removeEventListener('message', aoReceber);
  }, [enviar, onImportar]);

  // dados novos (depois de importar uma planilha) com o painel já aberto
  useEffect(() => {
    enviar();
  }, [enviar]);

  return (
    <iframe
      ref={frame}
      src="/painel-ti/index.html"
      title="Painel de despesas de TI"
      sandbox="allow-scripts"
      className="block w-full rounded-lg border border-border bg-card"
      style={{ height: 'calc(100vh - 10.5rem)', minHeight: 560 }}
    />
  );
}

function Importar({ modelo }: { modelo: TiModelo }) {
  const queryClient = useQueryClient();
  const entrada = useRef<HTMLInputElement>(null);
  const [arquivos, setArquivos] = useState<File[]>([]);
  const [confirmando, setConfirmando] = useState(false);
  const [resultados, setResultados] = useState<ResultadoArquivo[] | null>(null);

  const importar = useMutation({
    mutationFn: async (files: File[]) => {
      const corpo = new FormData();
      files.forEach((f) => corpo.append('arquivo', f));
      const resp = await fetch('/api/ti/importar', { method: 'POST', body: corpo });
      const data = (await resp.json().catch(() => ({}))) as { resultados?: ResultadoArquivo[]; error?: string };
      // 400 com resultados = todos os arquivos falharam, e cada um traz a causa
      if (!resp.ok && !data.resultados) throw new Error(data.error || `Erro HTTP ${resp.status}`);
      return data.resultados ?? [];
    },
    onSuccess: (r) => {
      setResultados(r);
      setArquivos([]);
      if (entrada.current) entrada.current.value = '';
      queryClient.invalidateQueries({ queryKey: ['ti-modelo'] });
      queryClient.invalidateQueries({ queryKey: ['ti-conferencia'] });
      const ok = r.filter((x) => x.ok).length;
      if (ok === r.length) toast.success(ok === 1 ? 'Arquivo importado' : `${ok} arquivos importados`);
      else if (ok > 0) toast.warning(`${ok} de ${r.length} arquivos importados — veja os erros abaixo`);
      else toast.error('Nenhum arquivo foi importado — veja os erros abaixo');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const quando = modelo.meta.importadoEm
    ? new Date(modelo.meta.importadoEm).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
    : null;
  const sap = modelo.meta.sap;
  const ksb2Quando = sap.ksb2Em ? new Date(sap.ksb2Em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' }) : null;
  const tamanho = (f: File) => (f.size >= 1048576 ? `${(f.size / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(f.size / 1024))} KB`);

  return (
    <section className="flex max-w-3xl flex-col gap-4 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-base font-semibold text-foreground">Importar planilhas</h2>
        <p className="mt-1 text-sm text-fg-secondary">
          Pode enviar os arquivos <strong>juntos</strong>; o sistema reconhece cada um pelo conteúdo.
        </p>
        <ul className="mt-2 flex list-disc flex-col gap-1 pl-5 text-sm text-fg-secondary">
          <li>
            <strong>Planilha Forecast</strong> (FC FY27 Forecast, completa, de janeiro ao mês atual) — <strong>substitui</strong> parâmetros, orçamento e
            lançamentos que vieram de planilha. Lançamentos digitados no sistema ou recebidos do SAP não são alterados.
          </li>
          <li>
            <strong>KSB1</strong> (realizado, acumulada do ano, filtrada pelos centros de custo da relação) — substitui só os meses que ela cobre.
          </li>
          <li>
            <strong>KSB2</strong> (compromissado) — é a foto do saldo em aberto: substitui a anterior.
          </li>
        </ul>
      </div>

      <div className="flex flex-col gap-0.5 text-xs text-fg-helper">
        <p>
          {quando ? (
            <>
              Planilha: <span className="font-medium text-fg-secondary">{modelo.meta.fileName || 'arquivo sem nome'}</span> em {quando}.
            </>
          ) : (
            'Nenhuma planilha Forecast importada ainda.'
          )}
        </p>
        <p>
          KSB1:{' '}
          {sap.ksb1De ? (
            <span className="font-medium text-fg-secondary">
              {rotuloMes(sap.ksb1De)} a {rotuloMes(sap.ksb1Ate)}
            </span>
          ) : (
            'nenhuma carregada.'
          )}{' '}
          · KSB2: {ksb2Quando ? <span className="font-medium text-fg-secondary">de {ksb2Quando}</span> : 'nenhuma carregada.'}
        </p>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="ti-arquivo">Planilhas (.xlsx, até 25 MB cada)</Label>
        <Input
          id="ti-arquivo"
          ref={entrada}
          type="file"
          multiple
          accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          onChange={(e) => setArquivos(Array.from(e.target.files ?? []))}
        />
        {arquivos.length > 0 && (
          <ul className="mt-1 flex flex-col gap-0.5 text-xs text-fg-helper" aria-label="Arquivos escolhidos">
            {arquivos.map((f) => (
              <li key={f.name}>
                {f.name} <span className="text-fg-muted">· {tamanho(f)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Button className="self-start" disabled={arquivos.length === 0 || importar.isPending} onClick={() => setConfirmando(true)}>
        <Upload />
        {importar.isPending ? 'Importando...' : arquivos.length > 1 ? `Importar ${arquivos.length} arquivos` : 'Importar'}
      </Button>

      {resultados && (
        <ul className="flex flex-col gap-2" aria-label="Resultado da importação">
          {resultados.map((r) => (
            <li
              key={r.arquivo}
              role={r.ok ? 'status' : 'alert'}
              className={cn(
                'rounded-md border px-4 py-3 text-sm',
                r.ok ? 'border-success-border bg-success-bg text-success-text' : 'border-danger-border bg-danger-bg text-danger-text',
              )}
            >
              <p className="flex flex-wrap items-center gap-2 font-medium">
                <span aria-hidden>{r.ok ? '✓' : '✕'}</span>
                {r.arquivo}
                {r.tipo && <Badge variant={r.ok ? 'default' : 'destructive'}>{ROTULO_TIPO[r.tipo]}</Badge>}
              </p>
              {!r.ok && <p className="mt-1 text-[13px]">{r.erro}</p>}
              {r.ok && r.forecast && (
                <ul className="mt-1 list-inside list-disc text-[13px]">
                  <li>
                    {r.forecast.orcamento_linhas.toLocaleString('pt-BR')} linhas de orçamento ({r.forecast.meses} meses, {rotuloMes(r.forecast.primeiro_mes)} a{' '}
                    {rotuloMes(r.forecast.ultimo_mes)})
                  </li>
                  <li>{r.forecast.lancamentos.toLocaleString('pt-BR')} lançamentos</li>
                </ul>
              )}
              {r.ok && r.sap && r.tipo === 'ksb1' && (
                <p className="mt-1 text-[13px]">
                  {r.sap.aproveitadas.toLocaleString('pt-BR')} linhas com pedido de {r.sap.linhas.toLocaleString('pt-BR')} ({r.sap.sem_pedido.toLocaleString('pt-BR')} sem
                  pedido, ignoradas) · meses {rotuloMes(r.sap.primeiro_mes)} a {rotuloMes(r.sap.ultimo_mes)}
                </p>
              )}
              {r.ok && r.sap && r.tipo === 'ksb2' && (
                <p className="mt-1 text-[13px]">
                  {r.sap.aproveitadas.toLocaleString('pt-BR')} linhas de saldo ({r.sap.totais.toLocaleString('pt-BR')} linhas de total ignoradas)
                </p>
              )}
            </li>
          ))}
        </ul>
      )}

      <Dialog open={confirmando} onOpenChange={setConfirmando}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Importar {arquivos.length === 1 ? 'o arquivo' : `os ${arquivos.length} arquivos`}?</DialogTitle>
            <DialogDescription>
              A planilha Forecast, se estiver entre eles, <strong>substitui</strong> os dados que vieram de planilha. A KSB1 substitui os meses que cobre, e a
              KSB2 substitui a anterior. Se um arquivo falhar, nada é alterado por ele, e os outros seguem.
            </DialogDescription>
          </DialogHeader>
          <ul className="max-h-40 overflow-auto text-sm text-fg-secondary">
            {arquivos.map((f) => (
              <li key={f.name} className="truncate">
                {f.name}
              </li>
            ))}
          </ul>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmando(false)}>
              Cancelar
            </Button>
            <Button
              onClick={() => {
                if (arquivos.length > 0) importar.mutate(arquivos);
                setConfirmando(false);
              }}
            >
              Importar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export default function ControleTI() {
  const [aba, setAba] = useState<Aba>('painel');
  const { data: modelo, error, isLoading } = useQuery({
    queryKey: ['ti-modelo'],
    queryFn: () => pedir<TiModelo>('/api/ti/modelo'),
  });

  const vazio = modelo && modelo.budget.length === 0 && modelo.lanc.length === 0;

  return (
    <div className={cn('mx-auto flex w-full flex-col gap-4 p-6', aba === 'painel' ? 'max-w-none' : 'max-w-7xl')}>
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

      {modelo && modelo.meta.sap.provisorios.length > 0 && (
        <p role="status" className="rounded-md border border-warning-border bg-warning-bg px-4 py-3 text-sm text-warning-text">
          <strong>Provisório:</strong> {modelo.meta.sap.provisorios.map(rotuloMes).join(', ')} {modelo.meta.sap.provisorios.length === 1 ? 'está fechado' : 'estão fechados'} sem KSB1
          e {modelo.meta.sap.provisorios.length === 1 ? 'mostra' : 'mostram'} o valor da planilha. Importe a KSB1 acumulada do ano para o SAP virar o oficial.
        </p>
      )}
      {modelo && modelo.meta.sap.oficiais.length > 0 && (
        <p role="status" className="rounded-md border border-info-border bg-info-bg px-4 py-3 text-sm text-info-text">
          <strong>Realizado oficial do SAP:</strong> {modelo.meta.sap.oficiais.map(rotuloMes).join(', ')}. Nesses meses, o valor dos pedidos vem da KSB1; itens sem
          pedido e imobilizado continuam da planilha.
        </p>
      )}

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

      {modelo && !vazio && aba === 'painel' && <Painel modelo={modelo} onImportar={() => setAba('importar')} />}
      {modelo && !vazio && aba === 'lancamentos' && <Lancamentos modelo={modelo} />}
      {modelo && !vazio && aba === 'conferencia' && <ConferenciaSap onImportar={() => setAba('importar')} />}
      {modelo && aba === 'importar' && <Importar modelo={modelo} />}
    </div>
  );
}
