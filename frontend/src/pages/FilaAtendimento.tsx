import { useMemo, useState } from 'react';
import { Input } from '@/components/ui/input';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';

// Fila de atendimento (Epic 4 — Stories 4.1 a 4.5). Duas listas: a fila
// propriamente dita (status='aberta', GET /api/fila/solicitacoes — Story
// 4.1) e "minhas solicitações" (status='em_atendimento' do admin logado,
// GET /api/fila/minhas — rota de apoio criada para esta tela), de onde
// saem pendência/comentário/finalizar/seleção para lote. Cada ação só
// manda `versao` (lock otimista) — nunca decide status no cliente.

interface ItemFila {
  id: string;
  codigo?: string;
  local_obra?: string | null;
  tipo_solicitacao: string;
  centro_custo_id: string | null;
  centro_custo_codigo?: string | null;
  centro_custo_nome?: string | null;
  status: string;
  versao: number;
  created_at: string;
  prazo_limite: string;
  atrasada: boolean;
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data?.erro?.mensagem || data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

// A API não devolve total/cursor (AD-14) e paginacaoDaQuery trava "tamanho"
// em 100 no servidor — então uma única chamada com tamanho=100 só mostra as
// 100 solicitações mais antigas (ORDER BY created_at) quando a fila passa
// disso, com o resto silenciosamente fora da tela. Pagina no cliente até
// vir uma página mais curta que o pedido, mesmo padrão já aplicado em
// Gestão de Usuários.
async function fetchTudoPaginado<T>(urlBase: string): Promise<T[]> {
  const tamanho = 100;
  let pagina = 1;
  const todos: T[] = [];
  for (;;) {
    const resp = await fetchJSON<{ items: T[] }>(
      `${urlBase}${urlBase.includes('?') ? '&' : '?'}pagina=${pagina}&tamanho=${tamanho}`,
    );
    todos.push(...resp.items);
    if (resp.items.length < tamanho) break;
    pagina++;
  }
  return todos;
}

function baixarArquivoBase64(nomeArquivo: string, base64: string) {
  const bytes = atob(base64);
  const buffer = new Uint8Array(bytes.length);
  for (let i = 0; i < bytes.length; i++) buffer[i] = bytes.charCodeAt(i);
  const blob = new Blob([buffer], { type: 'application/vnd.ms-excel.sheet.macroEnabled.12' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = nomeArquivo;
  a.click();
  URL.revokeObjectURL(url);
}

// Rótulos de negócio para o enum tipo_solicitacao (CHECK da migration 006).
const TIPO_ROTULO: Record<string, string> = {
  transferencia: 'Transferência',
  inclusao: 'Inclusão',
  inclusao_sfc: 'Inclusão SFC',
  imobilizado: 'Imobilizado',
  obras: 'Obras',
};
const rotuloTipo = (tipo: string) => TIPO_ROTULO[tipo] ?? tipo;

const dataHora = (iso: string) =>
  new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });

function CentroCustoCell({ item }: { item: ItemFila }) {
  if (item.centro_custo_codigo) {
    return (
      <span className="flex min-w-0 flex-col">
        <span className="font-medium text-foreground">{item.centro_custo_codigo}</span>
        {item.centro_custo_nome && <span className="truncate text-xs text-fg-helper">{item.centro_custo_nome}</span>}
      </span>
    );
  }
  // Obras não tem CC (migration 009): mostra o local da obra.
  if (item.local_obra) return <span className="truncate" title="Local da obra">{item.local_obra}</span>;
  return <span className="text-fg-muted">—</span>;
}

function PrazoCell({ atrasada, prazo }: { atrasada: boolean; prazo: string }) {
  return (
    <span className="flex flex-col items-start gap-1">
      <Badge variant={atrasada ? 'destructive' : 'default'}>{atrasada ? 'Atrasada' : 'No prazo'}</Badge>
      <span className="text-xs tabular-nums text-fg-helper">até {dataHora(prazo)}</span>
    </span>
  );
}

function SecaoFila({
  titulo,
  quantidade,
  acoes,
  children,
}: {
  titulo: string;
  quantidade?: number;
  acoes?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
          {titulo}
          {quantidade !== undefined && <Badge variant="secondary">{quantidade.toLocaleString('pt-BR')}</Badge>}
        </h2>
        {acoes}
      </div>
      {children}
    </section>
  );
}

function EstadoLista({ carregando, erro }: { carregando: boolean; erro: unknown }) {
  if (carregando) return <p className="text-sm text-fg-helper">Carregando...</p>;
  if (erro)
    return (
      <p role="alert" className="rounded-md border border-danger-border bg-danger-bg px-3 py-2 text-sm text-danger-text">
        Não foi possível carregar a lista. {(erro as Error).message}
      </p>
    );
  return null;
}

const FilaAtendimento = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [pendenciaItem, setPendenciaItem] = useState<ItemFila | null>(null);
  const [pendenciaTexto, setPendenciaTexto] = useState('');
  // Abas e filtros (retorno de 25/11, seção 12): os cartões do topo são as abas; os filtros valem para todas.
  const [aba, setAba] = useState<'fila' | 'comigo' | 'pendentes' | 'aprovacao'>('fila');
  const [filtroTipo, setFiltroTipo] = useState('todos');
  const [busca, setBusca] = useState('');
  const [soAtrasadas, setSoAtrasadas] = useState(false);
  const [selecionados, setSelecionados] = useState<Set<string>>(new Set());

  const filaQuery = useQuery({
    queryKey: ['fila-aberta'],
    queryFn: () => fetchTudoPaginado<ItemFila>('/api/fila/solicitacoes'),
  });

  // Transferências acima do limite esperando a diretoria. O e-mail com o link já foi ao aprovador; aqui o
  // atendimento registra a decisão à mão (aprovador que é cargo/grupo, sem e-mail, ou e-mail que não chegou).
  const aguardandoQuery = useQuery({
    queryKey: ['fila-aguardando-aprovacao'],
    queryFn: () => fetchTudoPaginado<ItemFila>('/api/fila/solicitacoes?status=aguardando_aprovacao'),
  });
  const [decisaoItem, setDecisaoItem] = useState<{ item: ItemFila; decisao: 'aprovada' | 'rejeitada' } | null>(null);
  const [decisaoMotivo, setDecisaoMotivo] = useState('');
  const decisaoMutation = useMutation({
    mutationFn: async () => {
      if (!decisaoItem) return;
      await fetchJSON(`/api/solicitacoes/${decisaoItem.item.id}/aprovacao-manual`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao: decisaoItem.item.versao, decisao: decisaoItem.decisao, motivo: decisaoMotivo.trim() }),
      });
    },
    onSuccess: () => {
      toast.success(decisaoItem?.decisao === 'aprovada' ? 'Aprovada: entrou na fila de atendimento.' : 'Rejeitada.');
      setDecisaoItem(null);
      setDecisaoMotivo('');
      queryClient.invalidateQueries({ queryKey: ['fila-aguardando-aprovacao'] });
      queryClient.invalidateQueries({ queryKey: ['fila-aberta'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const minhasQuery = useQuery({
    queryKey: ['fila-minhas'],
    queryFn: () => fetchTudoPaginado<ItemFila>('/api/fila/minhas'),
  });

  // Pendentes: devolvidas ao solicitante, esperando a resposta dele.
  const pendentesQuery = useQuery({
    queryKey: ['fila-pendentes'],
    queryFn: () =>
      fetchJSON<{ items: (ItemFila & { solicitante_nome?: string; atendente_nome?: string })[] }>('/api/solicitacoes/busca?status=pendente&tamanho=100').then((r) => r.items),
  });

  function invalidarFila() {
    queryClient.invalidateQueries({ queryKey: ['fila-aberta'] });
    queryClient.invalidateQueries({ queryKey: ['fila-minhas'] });
    queryClient.invalidateQueries({ queryKey: ['fila-pendentes'] });
  }

  const assumirMutation = useMutation({
    mutationFn: ({ id, versao }: { id: string; versao: number }) =>
      fetchJSON(`/api/solicitacoes/${id}/assumir`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao }),
      }),
    onSuccess: () => {
      toast.success('Solicitação assumida');
      invalidarFila();
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const pendenciaMutation = useMutation({
    mutationFn: ({ id, versao, comentario }: { id: string; versao: number; comentario: string }) =>
      fetchJSON(`/api/solicitacoes/${id}/pendencia`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao, comentario }),
      }),
    onSuccess: () => {
      toast.success('Solicitação devolvida ao solicitante');
      setPendenciaItem(null);
      setPendenciaTexto('');
      invalidarFila();
    },
    onError: (err: Error) => toast.error(err.message),
  });

  async function gerarLote(tipo: 'despesa' | 'obra') {
    const minhas = minhasQuery.data ?? [];
    const ids = minhas
      .filter((i) => selecionados.has(i.id) && (tipo === 'obra' ? i.tipo_solicitacao === 'obras' : i.tipo_solicitacao !== 'obras'))
      .map((i) => i.id);
    if (ids.length === 0) {
      toast.error(`Nenhuma solicitação de ${tipo === 'obra' ? 'Obras' : 'Despesa'} selecionada`);
      return;
    }
    const chave = crypto.randomUUID();
    try {
      const resp = await fetchJSON<{
        aviso?: { codigo: string; solicitacoes_em_risco: string[] };
        lote_id?: string;
        arquivo_nome?: string;
        arquivo_base64?: string;
        solicitacoes_incluidas?: string[];
        bloqueados?: { solicitacao_id: string; motivo: string }[];
      }>(`/api/solicitacoes/lote-${tipo}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ solicitacao_ids: ids, chave_idempotencia: chave }),
      });

      if (resp.aviso) {
        toast.warning(
          `Aviso de verba duplicada (${resp.aviso.solicitacoes_em_risco.length} solicitação(ões) em risco) — gere de novo se quiser ignorar.`,
        );
        return;
      }
      if (resp.lote_id && resp.arquivo_base64 && resp.arquivo_nome) {
        baixarArquivoBase64(resp.arquivo_nome, resp.arquivo_base64);
      }
      const bloqueadosMsg = resp.bloqueados?.length
        ? ` (${resp.bloqueados.length} bloqueada(s): ${resp.bloqueados.map((b) => b.motivo).join('; ')})`
        : '';
      toast.success(`Lote gerado: ${resp.solicitacoes_incluidas?.length ?? 0} solicitação(ões)${bloqueadosMsg}`);
      setSelecionados(new Set());
      invalidarFila();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao gerar lote');
    }
  }

  // Exportar CSV de demonstração (15/10) — mesma elegibilidade de
  // "Gerar lote", mas sem gravar nada e sem precisar do .xlsm real (ainda
  // não configurado no servidor). Útil para mostrar a estrutura/dados do
  // lote antes do arquivo real estar disponível.
  async function baixarCSV(tipo: 'despesa' | 'obra') {
    const minhas = minhasQuery.data ?? [];
    const ids = minhas
      .filter((i) => selecionados.has(i.id) && (tipo === 'obra' ? i.tipo_solicitacao === 'obras' : i.tipo_solicitacao !== 'obras'))
      .map((i) => i.id);
    if (ids.length === 0) {
      toast.error(`Nenhuma solicitação de ${tipo === 'obra' ? 'Obras' : 'Despesa'} selecionada`);
      return;
    }
    try {
      const resp = await fetch(`/api/solicitacoes/lote-${tipo}/csv`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ solicitacao_ids: ids }),
      });
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data?.error || `Erro HTTP ${resp.status}`);
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `preview_lote_${tipo}.csv`;
      a.click();
      URL.revokeObjectURL(url);
      toast.success('CSV de demonstração baixado');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao gerar CSV');
    }
  }

  function toggleSelecionado(id: string) {
    setSelecionados((atual) => {
      const novo = new Set(atual);
      if (novo.has(id)) novo.delete(id);
      else novo.add(id);
      return novo;
    });
  }

  const filtrar = useMemo(() => {
    const termos = busca
      .normalize('NFD')
      .replace(/[\u0300-\u036f]/g, '')
      .toLowerCase()
      .split(/\s+/)
      .filter(Boolean);
    return <T extends ItemFila>(itens: T[] | undefined): T[] =>
      (itens ?? []).filter((i) => {
        if (filtroTipo !== 'todos' && i.tipo_solicitacao !== filtroTipo) return false;
        if (soAtrasadas && !i.atrasada) return false;
        if (termos.length === 0) return true;
        const alvo = `${i.codigo ?? ''} ${i.centro_custo_codigo ?? ''} ${i.centro_custo_nome ?? ''} ${i.local_obra ?? ''} ${rotuloTipo(i.tipo_solicitacao)}`
          .normalize('NFD')
          .replace(/[\u0300-\u036f]/g, '')
          .toLowerCase();
        return termos.every((t) => alvo.includes(t));
      });
  }, [busca, filtroTipo, soAtrasadas]);
  const atrasadasTotal = [...(filaQuery.data ?? []), ...(minhasQuery.data ?? [])].filter((i) => i.atrasada).length;
  const cartoes: { chave: typeof aba | 'atrasadas'; rotulo: string; n?: number; alerta?: boolean }[] = [
    { chave: 'fila', rotulo: 'Aguardando atendimento', n: filaQuery.data?.length },
    { chave: 'comigo', rotulo: 'Comigo (em atendimento)', n: minhasQuery.data?.length },
    { chave: 'pendentes', rotulo: 'Pendentes com o solicitante', n: pendentesQuery.data?.length },
    { chave: 'aprovacao', rotulo: 'Aguardando aprovação', n: aguardandoQuery.data?.length },
    { chave: 'atrasadas', rotulo: 'Atrasadas (fila + comigo)', n: atrasadasTotal, alerta: atrasadasTotal > 0 },
  ];

  const minhas = filtrar(minhasQuery.data);
  const todasSelecionadas = minhas.length > 0 && minhas.every((i) => selecionados.has(i.id));
  function alternarTodas() {
    setSelecionados(todasSelecionadas ? new Set() : new Set(minhas.map((i) => i.id)));
  }

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-6">
      <h1 className="text-lg font-semibold text-foreground">Fila de atendimento</h1>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-5" role="tablist" aria-label="Situação dos chamados">
        {cartoes.map((c) => {
          const ativo = c.chave === 'atrasadas' ? soAtrasadas : aba === c.chave && !soAtrasadas;
          return (
            <button
              key={c.chave}
              type="button"
              role="tab"
              aria-selected={ativo}
              onClick={() => {
                if (c.chave === 'atrasadas') {
                  setSoAtrasadas((v) => !v);
                  if (aba !== 'fila' && aba !== 'comigo') setAba('fila');
                } else {
                  setAba(c.chave);
                  setSoAtrasadas(false);
                }
              }}
              className={`flex min-w-0 flex-col gap-1 rounded-lg border px-4 py-3 text-left transition-colors ${ativo ? 'border-ring bg-muted' : 'border-border bg-card hover:bg-muted/60'}`}
            >
              <span className="truncate text-xs text-fg-helper">{c.rotulo}</span>
              <span className={`text-xl font-semibold tabular-nums ${c.alerta ? 'text-danger-text' : 'text-foreground'}`}>{c.n ?? '…'}</span>
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1">
          <Label htmlFor="fila-busca" className="text-xs">Buscar</Label>
          <Input id="fila-busca" className="h-9 w-64" placeholder="Código (GO-12), CC ou local da obra" value={busca} onChange={(e) => setBusca(e.target.value)} />
        </div>
        <div className="grid gap-1">
          <Label className="text-xs">Tipo</Label>
          <Select value={filtroTipo} onValueChange={setFiltroTipo}>
            <SelectTrigger className="h-9 w-44"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="todos">Todos os tipos</SelectItem>
              {Object.entries(TIPO_ROTULO).map(([v, r]) => <SelectItem key={v} value={v}>{r}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <label className="flex h-9 cursor-pointer items-center gap-2 text-sm text-fg-secondary">
          <input type="checkbox" className="size-4 accent-[var(--blue-d50)]" checked={soAtrasadas} onChange={(e) => setSoAtrasadas(e.target.checked)} />
          Só atrasadas
        </label>
        {(busca || filtroTipo !== 'todos' || soAtrasadas) && (
          <Button size="sm" variant="ghost" onClick={() => { setBusca(''); setFiltroTipo('todos'); setSoAtrasadas(false); }}>Limpar filtros</Button>
        )}
        <Button size="sm" variant="ghost" className="ml-auto" onClick={() => navigate('/solicitacoes')}>Todas as solicitações →</Button>
      </div>

      {aba === 'pendentes' && (
        <SecaoFila titulo="Pendentes com o solicitante" quantidade={filtrar(pendentesQuery.data).length}>
          <EstadoLista carregando={pendentesQuery.isLoading} erro={pendentesQuery.error} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tipo</TableHead>
                <TableHead>Centro de custo</TableHead>
                <TableHead>Solicitante</TableHead>
                <TableHead>Atendente</TableHead>
                <TableHead>Aberta em</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtrar(pendentesQuery.data).map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="font-medium"><span className="block text-xs tabular-nums text-fg-helper">{item.codigo}</span>{rotuloTipo(item.tipo_solicitacao)}</TableCell>
                  <TableCell><CentroCustoCell item={item} /></TableCell>
                  <TableCell>{item.solicitante_nome ?? '—'}</TableCell>
                  <TableCell>{item.atendente_nome ?? '—'}</TableCell>
                  <TableCell className="tabular-nums text-fg-secondary">{dataHora(item.created_at)}</TableCell>
                  <TableCell><Button size="sm" variant="outline" onClick={() => navigate(`/fila/${item.id}`)}>Ver</Button></TableCell>
                </TableRow>
              ))}
              {filtrar(pendentesQuery.data).length === 0 && !pendentesQuery.isLoading && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={6} className="py-6 text-center text-sm text-fg-helper">Nenhuma solicitação pendente com o solicitante.</TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </SecaoFila>
      )}

      {aba === 'aprovacao' && (
        <SecaoFila titulo="Aguardando aprovação da diretoria" quantidade={filtrar(aguardandoQuery.data).length}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tipo</TableHead>
                <TableHead>Centro de custo</TableHead>
                <TableHead>Aberta em</TableHead>
                <TableHead className="w-px" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtrar(aguardandoQuery.data).map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="font-medium"><span className="block text-xs tabular-nums text-fg-helper">{item.codigo}</span>{rotuloTipo(item.tipo_solicitacao)}</TableCell>
                  <TableCell><CentroCustoCell item={item} /></TableCell>
                  <TableCell className="tabular-nums text-fg-secondary">{dataHora(item.created_at)}</TableCell>
                  <TableCell className="flex gap-2">
                    <Button size="sm" variant="outline" onClick={() => navigate(`/fila/${item.id}`)}>Ver</Button>
                    <Button size="sm" onClick={() => setDecisaoItem({ item, decisao: 'aprovada' })}>Registrar aprovação</Button>
                    <Button size="sm" variant="outline" onClick={() => setDecisaoItem({ item, decisao: 'rejeitada' })}>Registrar rejeição</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </SecaoFila>
      )}

      <Dialog open={!!decisaoItem} onOpenChange={(o) => { if (!o) { setDecisaoItem(null); setDecisaoMotivo(''); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{decisaoItem?.decisao === 'aprovada' ? 'Registrar aprovação' : 'Registrar rejeição'}</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-fg-secondary">
            {decisaoItem?.decisao === 'aprovada'
              ? 'Use quando a diretoria já aprovou por fora do sistema. A solicitação entra na fila de atendimento.'
              : 'A solicitação é encerrada e o solicitante é avisado por e-mail.'}
          </p>
          <div className="grid gap-1.5">
            <Label htmlFor="decisao-motivo">{decisaoItem?.decisao === 'rejeitada' ? 'Motivo (obrigatório)' : 'Observação (opcional)'}</Label>
            <Textarea id="decisao-motivo" value={decisaoMotivo} onChange={(e) => setDecisaoMotivo(e.target.value)} />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDecisaoItem(null)}>Cancelar</Button>
            <Button
              onClick={() => decisaoMutation.mutate()}
              disabled={decisaoMutation.isPending || (decisaoItem?.decisao === 'rejeitada' && decisaoMotivo.trim().length < 3)}
            >
              Confirmar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {aba === 'fila' && (
      <SecaoFila titulo="Aguardando atendimento" quantidade={filtrar(filaQuery.data).length}>
        <EstadoLista carregando={filaQuery.isLoading} erro={filaQuery.error} />
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tipo</TableHead>
              <TableHead>Centro de custo</TableHead>
              <TableHead>Aberta em</TableHead>
              <TableHead>Prazo</TableHead>
              <TableHead className="w-px" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtrar(filaQuery.data).map((item) => (
              <TableRow key={item.id}>
                <TableCell className="font-medium"><span className="block text-xs tabular-nums text-fg-helper">{item.codigo}</span>{rotuloTipo(item.tipo_solicitacao)}</TableCell>
                <TableCell><CentroCustoCell item={item} /></TableCell>
                <TableCell className="tabular-nums text-fg-secondary">{dataHora(item.created_at)}</TableCell>
                <TableCell><PrazoCell atrasada={item.atrasada} prazo={item.prazo_limite} /></TableCell>
                <TableCell>
                  <Button
                    size="sm"
                    onClick={() => assumirMutation.mutate({ id: item.id, versao: item.versao })}
                    disabled={assumirMutation.isPending}
                  >
                    Assumir
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {filtrar(filaQuery.data).length === 0 && !filaQuery.isLoading && (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={5} className="py-6 text-center text-sm text-fg-helper">
                  Nenhuma solicitação aguardando atendimento{busca || filtroTipo !== 'todos' || soAtrasadas ? ' com estes filtros' : ''}.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </SecaoFila>
      )}

      {aba === 'comigo' && (
      <SecaoFila
        titulo="Minhas solicitações em atendimento"
        quantidade={minhas.length}
        acoes={
          <div className="flex flex-wrap items-center gap-2">
            {selecionados.size > 0 && (
              <span className="text-xs text-fg-helper">
                {selecionados.size === 1 ? '1 selecionada' : `${selecionados.size} selecionadas`}
              </span>
            )}
            <Button size="sm" onClick={() => gerarLote('despesa')}>
              Gerar lote de despesa
            </Button>
            <Button size="sm" variant="outline" onClick={() => gerarLote('obra')}>
              Gerar lote de obra
            </Button>
            <Button size="sm" variant="ghost" onClick={() => baixarCSV('despesa')} title="Prévia em CSV, sem o .xlsm do robô SAP">
              CSV despesa (prévia)
            </Button>
            <Button size="sm" variant="ghost" onClick={() => baixarCSV('obra')} title="Prévia em CSV, sem o .xlsm do robô SAP">
              CSV obra (prévia)
            </Button>
          </div>
        }
      >
        <EstadoLista carregando={minhasQuery.isLoading} erro={minhasQuery.error} />
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-px">
                <input
                  type="checkbox"
                  aria-label="Selecionar todas"
                  className="size-4 cursor-pointer accent-[var(--blue-d50)]"
                  checked={todasSelecionadas}
                  disabled={minhas.length === 0}
                  onChange={alternarTodas}
                />
              </TableHead>
              <TableHead>Tipo</TableHead>
              <TableHead>Centro de custo</TableHead>
              <TableHead>Aberta em</TableHead>
              <TableHead>Prazo</TableHead>
              <TableHead>Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {minhas.map((item) => (
              <TableRow key={item.id} data-state={selecionados.has(item.id) ? 'selected' : undefined}>
                <TableCell>
                  <input
                    type="checkbox"
                    aria-label={`Selecionar ${rotuloTipo(item.tipo_solicitacao)} de ${dataHora(item.created_at)}`}
                    className="size-4 cursor-pointer accent-[var(--blue-d50)]"
                    checked={selecionados.has(item.id)}
                    onChange={() => toggleSelecionado(item.id)}
                  />
                </TableCell>
                <TableCell className="font-medium"><span className="block text-xs tabular-nums text-fg-helper">{item.codigo}</span>{rotuloTipo(item.tipo_solicitacao)}</TableCell>
                <TableCell><CentroCustoCell item={item} /></TableCell>
                <TableCell className="tabular-nums text-fg-secondary">{dataHora(item.created_at)}</TableCell>
                <TableCell><PrazoCell atrasada={item.atrasada} prazo={item.prazo_limite} /></TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" onClick={() => navigate(`/fila/${item.id}`)}>
                      Ver e comentar
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setPendenciaItem(item)}>
                      Pendência
                    </Button>
                    <Button size="sm" onClick={() => navigate(`/fila/${item.id}`)} title="Abre o chamado: resultado, categoria do erro, ordens de Obras e confirmação">
                      Finalizar
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {minhas.length === 0 && !minhasQuery.isLoading && (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="py-6 text-center text-sm text-fg-helper">
                  Nenhuma solicitação em atendimento{busca || filtroTipo !== 'todos' || soAtrasadas ? ' com estes filtros' : '. Assuma uma em "Aguardando atendimento"'}.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </SecaoFila>
      )}

      <Dialog open={Boolean(pendenciaItem)} onOpenChange={(o) => !o && setPendenciaItem(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Marcar pendência</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label>Comentário (devolvido ao solicitante)</Label>
            <Textarea value={pendenciaTexto} onChange={(e) => setPendenciaTexto(e.target.value)} />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendenciaItem(null)}>
              Cancelar
            </Button>
            <Button
              disabled={!pendenciaTexto.trim() || pendenciaMutation.isPending}
              onClick={() =>
                pendenciaItem &&
                pendenciaMutation.mutate({ id: pendenciaItem.id, versao: pendenciaItem.versao, comentario: pendenciaTexto })
              }
            >
              Devolver
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </div>
  );
};

export default FilaAtendimento;
