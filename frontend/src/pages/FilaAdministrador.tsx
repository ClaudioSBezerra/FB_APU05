import { useState } from 'react';
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

// Fila do administrador (Epic 4 — Stories 4.1 a 4.5). Duas listas: a fila
// propriamente dita (status='aberta', GET /api/fila/solicitacoes — Story
// 4.1) e "minhas solicitações" (status='em_atendimento' do admin logado,
// GET /api/fila/minhas — rota de apoio criada para esta tela), de onde
// saem pendência/comentário/finalizar/seleção para lote. Cada ação só
// manda `versao` (lock otimista) — nunca decide status no cliente.

interface ItemFila {
  id: string;
  tipo_solicitacao: string;
  centro_custo_id: string | null;
  status: string;
  versao: number;
  created_at: string;
  prazo_limite: string;
  atrasada: boolean;
}

interface Comentario {
  id: string;
  autor_id: string;
  tipo: string;
  texto: string;
  created_at: string;
}

interface AnexoDetalhe {
  id: string;
  nome_arquivo: string;
  content_type: string;
  tamanho_bytes: number;
  created_at: string;
}

interface DetalheSolicitacao {
  id: string;
  tipo_solicitacao: string;
  status: string;
  versao: number;
  fornecedor: string | null;
  descricao: string | null;
  anexos: AnexoDetalhe[];
  comentarios: Comentario[];
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

// baixarAnexo usa fetch (não um <a href> cru) porque o Authorization é
// injetado só nas chamadas via window.fetch (interceptor de
// AuthContext.tsx) — uma navegação nativa de <a> não carrega esse header e
// voltaria 401.
async function baixarAnexo(anexo: { id: string; nome_arquivo: string }) {
  try {
    const resp = await fetch(`/api/solicitacoes/anexos/${anexo.id}`);
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}));
      throw new Error(data?.error || `Erro HTTP ${resp.status}`);
    }
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = anexo.nome_arquivo;
    a.click();
    URL.revokeObjectURL(url);
  } catch (err) {
    toast.error(err instanceof Error ? err.message : 'Erro ao baixar anexo');
  }
}

function PrazoBadge({ atrasada, prazo }: { atrasada: boolean; prazo: string }) {
  return (
    <Badge variant={atrasada ? 'destructive' : 'secondary'}>
      {atrasada ? 'Atrasada' : 'No prazo'} · {new Date(prazo).toLocaleString('pt-BR')}
    </Badge>
  );
}

const FilaAdministrador = () => {
  const queryClient = useQueryClient();
  const [detalheId, setDetalheId] = useState<string | null>(null);
  const [pendenciaItem, setPendenciaItem] = useState<ItemFila | null>(null);
  const [pendenciaTexto, setPendenciaTexto] = useState('');
  const [comentarioTexto, setComentarioTexto] = useState('');
  const [finalizarItem, setFinalizarItem] = useState<ItemFila | null>(null);
  const [resultadoFinal, setResultadoFinal] = useState<'sucesso' | 'erro'>('sucesso');
  const [selecionados, setSelecionados] = useState<Set<string>>(new Set());

  const filaQuery = useQuery({
    queryKey: ['fila-aberta'],
    queryFn: () => fetchTudoPaginado<ItemFila>('/api/fila/solicitacoes'),
  });

  const minhasQuery = useQuery({
    queryKey: ['fila-minhas'],
    queryFn: () => fetchTudoPaginado<ItemFila>('/api/fila/minhas'),
  });

  const detalheQuery = useQuery({
    queryKey: ['solicitacao', detalheId],
    queryFn: () => fetchJSON<DetalheSolicitacao>(`/api/solicitacoes/${detalheId}`),
    enabled: Boolean(detalheId),
  });

  function invalidarFila() {
    queryClient.invalidateQueries({ queryKey: ['fila-aberta'] });
    queryClient.invalidateQueries({ queryKey: ['fila-minhas'] });
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

  const comentarMutation = useMutation({
    mutationFn: ({ id, versao, comentario }: { id: string; versao: number; comentario: string }) =>
      fetchJSON(`/api/solicitacoes/${id}/comentarios`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao, comentario }),
      }),
    onSuccess: () => {
      toast.success('Comentário adicionado');
      setComentarioTexto('');
      queryClient.invalidateQueries({ queryKey: ['solicitacao', detalheId] });
      invalidarFila();
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const finalizarMutation = useMutation({
    mutationFn: ({ id, versao, resultado }: { id: string; versao: number; resultado: string }) =>
      fetchJSON(`/api/solicitacoes/${id}/finalizar`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao, resultado, ordens_criadas: [] }),
      }),
    onSuccess: () => {
      toast.success('Solicitação finalizada');
      setFinalizarItem(null);
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

  return (
    <div className="flex flex-col gap-8 p-6">
      <div>
        <h1 className="mb-3 text-lg font-semibold text-foreground">
          Fila (aguardando atendimento){filaQuery.data ? ` — ${filaQuery.data.length}` : ''}
        </h1>
        {filaQuery.isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
        {filaQuery.error && <p className="text-sm text-destructive">{(filaQuery.error as Error).message}</p>}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tipo</TableHead>
              <TableHead>Centro de custo</TableHead>
              <TableHead>Prazo</TableHead>
              <TableHead>Versão</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(filaQuery.data ?? []).map((item) => (
              <TableRow key={item.id}>
                <TableCell className="font-medium capitalize">{item.tipo_solicitacao}</TableCell>
                <TableCell className="text-muted-foreground">{item.centro_custo_id ?? '—'}</TableCell>
                <TableCell>
                  <PrazoBadge atrasada={item.atrasada} prazo={item.prazo_limite} />
                </TableCell>
                <TableCell>{item.versao}</TableCell>
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
            {filaQuery.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                  Nenhuma solicitação aberta
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div>
        <div className="mb-3 flex items-center justify-between">
          <h1 className="text-lg font-semibold text-foreground">
            Minhas solicitações (em atendimento){minhasQuery.data ? ` — ${minhasQuery.data.length}` : ''}
          </h1>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => gerarLote('despesa')}>
              Gerar lote Despesa
            </Button>
            <Button size="sm" variant="outline" onClick={() => gerarLote('obra')}>
              Gerar lote Obra
            </Button>
            <Button size="sm" variant="outline" onClick={() => baixarCSV('despesa')}>
              Exportar CSV Despesa (demonstração)
            </Button>
            <Button size="sm" variant="outline" onClick={() => baixarCSV('obra')}>
              Exportar CSV Obra (demonstração)
            </Button>
          </div>
        </div>
        {minhasQuery.isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
        {minhasQuery.error && <p className="text-sm text-destructive">{(minhasQuery.error as Error).message}</p>}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead />
              <TableHead>Tipo</TableHead>
              <TableHead>Centro de custo</TableHead>
              <TableHead>Prazo</TableHead>
              <TableHead>Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(minhasQuery.data ?? []).map((item) => (
              <TableRow key={item.id}>
                <TableCell>
                  <input
                    type="checkbox"
                    checked={selecionados.has(item.id)}
                    onChange={() => toggleSelecionado(item.id)}
                  />
                </TableCell>
                <TableCell className="font-medium capitalize">{item.tipo_solicitacao}</TableCell>
                <TableCell className="text-muted-foreground">{item.centro_custo_id ?? '—'}</TableCell>
                <TableCell>
                  <PrazoBadge atrasada={item.atrasada} prazo={item.prazo_limite} />
                </TableCell>
                <TableCell className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => setDetalheId(item.id)}>
                    Ver / comentar
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setPendenciaItem(item)}>
                    Pendência
                  </Button>
                  <Button size="sm" onClick={() => setFinalizarItem(item)}>
                    Finalizar
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {minhasQuery.data?.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                  Nenhuma solicitação em atendimento
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={Boolean(detalheId)} onOpenChange={(o) => !o && setDetalheId(null)}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Histórico da solicitação</DialogTitle>
          </DialogHeader>
          {(detalheQuery.data?.fornecedor || detalheQuery.data?.descricao || (detalheQuery.data?.anexos.length ?? 0) > 0) && (
            <div className="flex flex-col gap-1.5 rounded-md border border-border p-3 text-sm">
              {detalheQuery.data?.fornecedor && (
                <p>
                  <span className="font-medium">Fornecedor:</span> {detalheQuery.data.fornecedor}
                </p>
              )}
              {detalheQuery.data?.descricao && (
                <p>
                  <span className="font-medium">Descrição:</span> {detalheQuery.data.descricao}
                </p>
              )}
              {(detalheQuery.data?.anexos.length ?? 0) > 0 && (
                <div className="flex flex-col gap-1">
                  <span className="font-medium">Anexos:</span>
                  {detalheQuery.data?.anexos.map((a) => (
                    <button
                      key={a.id}
                      type="button"
                      onClick={() => baixarAnexo(a)}
                      className="text-left text-primary underline underline-offset-2"
                    >
                      {a.nome_arquivo} ({Math.ceil(a.tamanho_bytes / 1024)} KB)
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="flex max-h-72 flex-col gap-2 overflow-y-auto">
            {(detalheQuery.data?.comentarios ?? []).map((c) => (
              <div key={c.id} className="rounded-md border border-border p-2 text-sm">
                <p className="text-xs text-muted-foreground">
                  {c.tipo} · {new Date(c.created_at).toLocaleString('pt-BR')}
                </p>
                <p>{c.texto}</p>
              </div>
            ))}
            {detalheQuery.data?.comentarios.length === 0 && (
              <p className="text-sm text-muted-foreground">Nenhum comentário ainda.</p>
            )}
          </div>
          <div className="grid gap-1.5">
            <Label>Novo comentário</Label>
            <Textarea value={comentarioTexto} onChange={(e) => setComentarioTexto(e.target.value)} />
          </div>
          <DialogFooter>
            <Button
              disabled={!comentarioTexto.trim() || !detalheQuery.data || comentarMutation.isPending}
              onClick={() =>
                detalheQuery.data &&
                detalheId &&
                comentarMutation.mutate({ id: detalheId, versao: detalheQuery.data.versao, comentario: comentarioTexto })
              }
            >
              Comentar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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

      <Dialog open={Boolean(finalizarItem)} onOpenChange={(o) => !o && setFinalizarItem(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Finalizar solicitação</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label>Resultado</Label>
            <Select value={resultadoFinal} onValueChange={(v) => setResultadoFinal(v as 'sucesso' | 'erro')}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="sucesso">Sucesso</SelectItem>
                <SelectItem value="erro">Erro</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Gere a exportação SAP (lote Despesa/Obra) antes de finalizar — finalizar é uma ação separada.
            </p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setFinalizarItem(null)}>
              Cancelar
            </Button>
            <Button
              disabled={finalizarMutation.isPending}
              onClick={() =>
                finalizarItem &&
                finalizarMutation.mutate({ id: finalizarItem.id, versao: finalizarItem.versao, resultado: resultadoFinal })
              }
            >
              Finalizar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default FilaAdministrador;
