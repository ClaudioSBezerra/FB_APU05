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
import { brl, dataBR, rotuloMes, type TiLanc, type TiModelo } from '@/lib/controleTi';

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

type Aba = 'painel' | 'lancamentos' | 'importar';

const ABAS: { id: Aba; rotulo: string; ajuda: string }[] = [
  { id: 'painel', rotulo: 'Painel', ajuda: 'gráficos e análises' },
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
      {modelo && aba === 'importar' && <Importar modelo={modelo} />}
    </div>
  );
}
