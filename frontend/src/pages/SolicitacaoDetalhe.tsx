import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { useAuth } from '@/contexts/AuthContext';

// Página do chamado (substitui o pop-up "Ver e comentar"): quem abriu, como foi preenchido, aprovador,
// anexos, histórico e as ações do atendimento (assumir, pendência, comentar, finalizar).
interface Lanc { lado: string; centro_custo: string; conta: string; conta_nome: string; classe_imobilizado: string; mes: string; valor: number }
interface LinhaObra { lado: string; local: string; subgrupo: string; ordem: string; valor: number }
interface Extras {
  codigo?: string;
  classificacao?: string | null;
  ano_orcamento?: number;
  obras_linhas?: LinhaObra[];
  solicitante_nome?: string | null;
  solicitante_email?: string | null;
  atendente_nome?: string | null;
  centro_custo_codigo?: string | null;
  centro_custo_nome?: string | null;
  created_at?: string;
  aprovador?: { tipo?: string; nome?: string; motivo?: string };
  lancamentos?: Lanc[];
}
interface Detalhe {
  id: string;
  tipo_solicitacao: string;
  status: string;
  versao: number;
  solicitante_id: string;
  administrador_id: string | null;
  fornecedor: string | null;
  fornecedor_cnpj?: string | null;
  fornecedor_nao_cadastrado?: boolean;
  quantidade_material?: number | null;
  descricao: string | null;
  anexos: { id: string; nome_arquivo: string; tamanho_bytes: number }[];
  comentarios: { id: string; autor_id: string; autor_nome?: string; tipo: string; texto: string; created_at: string }[];
  extras?: Extras;
}

const CATEGORIAS_ERRO: [string, string][] = [
  ['saldo_insuficiente', 'Saldo insuficiente na origem'],
  ['conta_cc_invalido', 'Conta/CC inválido no SAP'],
  ['divergencia_valores', 'Divergência de valores'],
  ['fora_periodo', 'Fora do período orçamentário'],
  ['documentacao_incompleta', 'Documentação incompleta'],
  ['outro', 'Outro'],
];
const TIPO_HISTORICO: Record<string, string> = { pendencia: 'Pendência', comentario: 'Comentário', encerramento: 'Encerramento' };

const TIPO: Record<string, string> = { transferencia: 'Transferência', inclusao: 'Inclusão', inclusao_sfc: 'Inclusão SFC', imobilizado: 'Imobilizado', obras: 'Obras' };
const STATUS: Record<string, string> = {
  aguardando_aprovacao: 'Aguardando aprovação', aberta: 'Aguardando atendimento', em_atendimento: 'Em atendimento',
  pendente: 'Pendente (com o solicitante)', finalizada_sucesso: 'Finalizada com sucesso', finalizada_erro: 'Finalizada com erro',
  finalizada: 'Finalizada', rejeitada: 'Rejeitada',
};
const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });
const MESES_ABREV = ['Jan', 'Fev', 'Mar', 'Abr', 'Mai', 'Jun', 'Jul', 'Ago', 'Set', 'Out', 'Nov', 'Dez'];
// "2026-11" ou "2026-11-01" → "Nov/2026"
const mesAno = (m?: string) => {
  const x = /^(\d{4})-(\d{2})/.exec(m ?? '');
  return x ? `${MESES_ABREV[Number(x[2]) - 1]}/${x[1]}` : m || '—';
};
const dataHora = (iso?: string) => (iso ? new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' }) : '—');

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data?.erro?.mensagem || data?.error || `Erro HTTP ${resp.status}`);
  return data as T;
}

async function baixarAnexo(a: { id: string; nome_arquivo: string }) {
  try {
    const resp = await fetch(`/api/solicitacoes/anexos/${a.id}`);
    if (!resp.ok) throw new Error(`Erro HTTP ${resp.status}`);
    const url = URL.createObjectURL(await resp.blob());
    const el = document.createElement('a');
    el.href = url;
    el.download = a.nome_arquivo;
    el.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'Erro ao baixar o anexo');
  }
}

function Campo({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-0.5">
      <dt className="text-xs text-muted-foreground">{rotulo}</dt>
      <dd className="text-sm text-foreground">{children}</dd>
    </div>
  );
}

export default function SolicitacaoDetalhe() {
  const { id = '' } = useParams();
  const { user } = useAuth();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [comentario, setComentario] = useState('');
  const [pendencia, setPendencia] = useState('');
  const [resultado, setResultado] = useState<'sucesso' | 'erro'>('sucesso');
  const [categoriaErro, setCategoriaErro] = useState('');
  const [obsFinal, setObsFinal] = useState('');
  const [confirmando, setConfirmando] = useState(false);

  const q = useQuery({ queryKey: ['solicitacao', id], queryFn: () => api<Detalhe>(`/api/solicitacoes/${id}`) });
  const d = q.data;
  const x = d?.extras;
  const atende = user?.perfil === 'administrador' || user?.perfil === 'atendente';
  const meu = !!d && d.administrador_id === user?.id;

  function refrescar() {
    qc.invalidateQueries({ queryKey: ['solicitacao', id] });
    qc.invalidateQueries({ queryKey: ['fila-aberta'] });
    qc.invalidateQueries({ queryKey: ['fila-minhas'] });
    qc.invalidateQueries({ queryKey: ['fila-aguardando-aprovacao'] });
  }
  const post = (acao: string, corpo: object, ok: string, depois?: () => void) =>
    api(`/api/solicitacoes/${id}/${acao}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(corpo) })
      .then(() => { toast.success(ok); depois?.(); refrescar(); })
      .catch((e: Error) => toast.error(e.message));
  const mut = useMutation({ mutationFn: (f: () => Promise<unknown>) => f() });

  if (q.isLoading) return <p className="p-6 text-sm text-muted-foreground">Carregando…</p>;
  if (q.error || !d) return <p className="p-6 text-sm text-destructive">{(q.error as Error)?.message ?? 'Solicitação não encontrada.'}</p>;

  const origem = (x?.lancamentos ?? []).filter((l) => l.lado === 'origem');
  const destino = (x?.lancamentos ?? []).filter((l) => l.lado === 'destino');
  const mesmoCC = origem.length > 0 && destino.length > 0 && destino.every((l) => l.centro_custo === origem[0].centro_custo);
  const tabela = (linhas: Lanc[], titulo: string) =>
    linhas.length === 0 ? null : (
      <div className="grid gap-1">
        <h3 className="text-sm font-semibold">{titulo}</h3>
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr><th className="p-2">Centro de custo</th><th className="p-2">Conta / classe</th><th className="p-2">Mês</th><th className="p-2 text-right">Valor</th></tr>
            </thead>
            <tbody>
              {linhas.map((l, i) => (
                <tr key={i} className="border-t border-border">
                  <td className="p-2">{l.centro_custo}</td>
                  <td className="p-2">{l.conta ? `${l.conta} — ${l.conta_nome}` : l.classe_imobilizado || '—'}</td>
                  <td className="p-2 tabular-nums">{mesAno(l.mes)}</td>
                  <td className="p-2 text-right tabular-nums">{moeda.format(l.valor)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <Link to={atende ? '/fila' : '/solicitacoes'} className="text-xs text-link underline underline-offset-2">{atende ? '← Fila de atendimento' : '← Minhas solicitações'}</Link>
          {atende && <Link to="/solicitacoes" className="ml-3 text-xs text-link underline underline-offset-2">Todas as solicitações</Link>}
          <h1 className="text-lg font-semibold text-foreground">{x?.codigo && <span className="mr-2 tabular-nums text-fg-helper">{x.codigo}</span>}{TIPO[d.tipo_solicitacao] ?? d.tipo_solicitacao}</h1>
        </div>
        <Badge variant="default">{STATUS[d.status] ?? d.status}</Badge>
      </div>

      <section className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2">
        <Campo rotulo="Aberta por">{x?.solicitante_nome ?? '—'}{x?.solicitante_email && <span className="text-muted-foreground"> · {x.solicitante_email}</span>}</Campo>
        <Campo rotulo="Aberta em">{dataHora(x?.created_at)}</Campo>
        <Campo rotulo="Centro de custo">{x?.centro_custo_codigo ? `${x.centro_custo_codigo} — ${x.centro_custo_nome}` : '—'}</Campo>
        <Campo rotulo="Atendente">{x?.atendente_nome ?? 'Ainda não assumida'}</Campo>
        <Campo rotulo="Aprovador">{x?.aprovador?.nome || '—'}{x?.aprovador?.motivo && <span className="text-muted-foreground"> · {x.aprovador.motivo}</span>}</Campo>
        {d.fornecedor && (
          <Campo rotulo="Fornecedor">
            {d.fornecedor}{d.fornecedor_cnpj && <span className="text-muted-foreground"> · {d.fornecedor_cnpj}</span>}
            {d.fornecedor_nao_cadastrado && <Badge variant="warning" className="ml-2">Não cadastrado</Badge>}
          </Campo>
        )}
        {d.quantidade_material ? <Campo rotulo="Quantidade do material">{d.quantidade_material.toLocaleString('pt-BR')}</Campo> : null}
        {d.descricao && <div className="sm:col-span-2"><Campo rotulo="Descrição">{d.descricao}</Campo></div>}
      </section>

      <section className="grid gap-4 rounded-lg border border-border bg-card p-4">
        <h2 className="text-sm font-semibold">Como foi preenchido</h2>
        {tabela(origem, 'Origem')}
        {tabela(destino, mesmoCC ? 'Destino (mesmo centro de custo da origem)' : 'Destino')}
        {tabela((x?.lancamentos ?? []).filter((l) => l.lado !== 'origem' && l.lado !== 'destino'), 'Lançamentos')}
        {(x?.obras_linhas ?? []).length > 0 && (
          <div className="grid gap-1">
            <h3 className="text-sm font-semibold">Linhas da obra{x?.classificacao ? ` · ${x.classificacao.replace(/_/g, ' ')}` : ''}{x?.ano_orcamento ? ` · ano ${x.ano_orcamento}` : ''}</h3>
            <div className="overflow-x-auto rounded-lg border border-border">
              <table className="w-full text-left text-sm">
                <thead className="bg-muted/50 text-xs text-muted-foreground">
                  <tr><th className="p-2">Lado</th><th className="p-2">Local</th><th className="p-2">Subgrupo</th><th className="p-2">Ordem</th><th className="p-2 text-right">Valor</th></tr>
                </thead>
                <tbody>
                  {(x?.obras_linhas ?? []).map((l, i) => (
                    <tr key={i} className="border-t border-border">
                      <td className="p-2">{l.lado === 'retirada' ? 'Retirada' : l.lado === 'inclusao' ? 'Inclusão' : '—'}</td>
                      <td className="p-2">{l.local}</td>
                      <td className="p-2">{l.subgrupo}</td>
                      <td className="p-2 tabular-nums">{l.ordem}</td>
                      <td className="p-2 text-right tabular-nums">{moeda.format(l.valor)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
        {(x?.lancamentos ?? []).length === 0 && (x?.obras_linhas ?? []).length === 0 && <p className="text-sm text-muted-foreground">Sem lançamentos para exibir.</p>}
      </section>

      <section className="grid gap-2 rounded-lg border border-border bg-card p-4">
        <h2 className="text-sm font-semibold">Anexos</h2>
        {d.anexos.length === 0 && <p className="text-sm text-muted-foreground">Nenhum anexo.</p>}
        {d.anexos.map((a) => (
          <button key={a.id} type="button" onClick={() => baixarAnexo(a)} className="w-fit text-left text-sm text-link underline underline-offset-2">
            {a.nome_arquivo} ({Math.ceil(a.tamanho_bytes / 1024)} KB)
          </button>
        ))}
      </section>

      <section className="grid gap-3 rounded-lg border border-border bg-card p-4">
        <h2 className="text-sm font-semibold">Histórico e comentários</h2>
        {d.comentarios.length === 0 && <p className="text-sm text-muted-foreground">Nenhum comentário ainda.</p>}
        {d.comentarios.map((c) => (
          <div key={c.id} className="rounded-lg border border-border p-2 text-sm">
            <p className="text-xs text-muted-foreground">{c.autor_nome || 'Usuário'} · {TIPO_HISTORICO[c.tipo] ?? c.tipo} · {dataHora(c.created_at)}</p>
            <p className="whitespace-pre-wrap">{c.texto}</p>
          </div>
        ))}
        <div className="grid gap-1.5">
          <Label htmlFor="coment">Novo comentário</Label>
          <Textarea id="coment" value={comentario} onChange={(e) => setComentario(e.target.value)} />
          <div>
            <Button size="sm" disabled={!comentario.trim() || mut.isPending}
              onClick={() => mut.mutate(() => post('comentarios', { versao: d.versao, comentario: comentario.trim() }, 'Comentário adicionado', () => setComentario('')))}>
              Comentar
            </Button>
          </div>
        </div>
      </section>

      {atende && (
        <section className="grid gap-4 rounded-lg border border-border bg-card p-4">
          <h2 className="text-sm font-semibold">Ações do atendimento</h2>
          {d.status === 'aberta' && (
            <div><Button onClick={() => mut.mutate(() => post('assumir', { versao: d.versao }, 'Solicitação assumida'))}>Assumir esta solicitação</Button></div>
          )}
          {d.status === 'em_atendimento' && !meu && <p className="text-sm text-muted-foreground">Esta solicitação está com {x?.atendente_nome ?? 'outro atendente'}.</p>}
          {d.status === 'em_atendimento' && meu && (
            <>
              <div className="grid gap-1.5">
                <Label htmlFor="pend">Pendência (devolve ao solicitante com este texto)</Label>
                <Textarea id="pend" value={pendencia} onChange={(e) => setPendencia(e.target.value)} />
                <div>
                  <Button size="sm" variant="outline" disabled={!pendencia.trim() || mut.isPending}
                    onClick={() => mut.mutate(() => post('pendencia', { versao: d.versao, comentario: pendencia.trim() }, 'Solicitação devolvida ao solicitante', () => setPendencia('')))}>
                    Marcar pendência
                  </Button>
                </div>
              </div>
              <div className="grid max-w-xs gap-1.5">
                <Label>Finalizar com resultado</Label>
                <Select value={resultado} onValueChange={(v) => setResultado(v as 'sucesso' | 'erro')}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sucesso">Sucesso</SelectItem>
                    <SelectItem value="erro">Erro</SelectItem>
                  </SelectContent>
                </Select>
                {resultado === 'erro' && (
                  <>
                    <Label>Categoria do erro (obrigatória)</Label>
                    <Select value={categoriaErro} onValueChange={setCategoriaErro}>
                      <SelectTrigger><SelectValue placeholder="Escolha a categoria" /></SelectTrigger>
                      <SelectContent>
                        {CATEGORIAS_ERRO.map(([v, r]) => <SelectItem key={v} value={v}>{r}</SelectItem>)}
                      </SelectContent>
                    </Select>
                    <Label htmlFor="obsfin">Observação{categoriaErro === 'outro' ? ' (obrigatória em "Outro")' : ' (opcional)'}</Label>
                    <Textarea id="obsfin" maxLength={1000} value={obsFinal} onChange={(e) => setObsFinal(e.target.value)} />
                  </>
                )}
                <div>
                  <Button size="sm" disabled={mut.isPending || (resultado === 'erro' && (!categoriaErro || (categoriaErro === 'outro' && !obsFinal.trim())))}
                    onClick={() => setConfirmando(true)}>
                    Finalizar
                  </Button>
                  {resultado === 'erro' && !categoriaErro && <p className="mt-1 text-xs text-fg-helper">Escolha a categoria do erro para finalizar.</p>}
                </div>
              </div>
            </>
          )}
          {d.status === 'aguardando_aprovacao' && <p className="text-sm text-muted-foreground">Aguardando aprovação. A decisão manual fica na fila de atendimento.</p>}
          {['finalizada', 'finalizada_sucesso', 'finalizada_erro', 'rejeitada', 'pendente'].includes(d.status) && <p className="text-sm text-muted-foreground">Sem ações disponíveis neste status.</p>}
        </section>
      )}
      <Dialog open={confirmando} onOpenChange={setConfirmando}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Finalizar o chamado como {resultado === 'sucesso' ? 'Sucesso' : 'Erro'}?</DialogTitle>
            <DialogDescription>
              Esta ação encerra o atendimento.
              {resultado === 'erro' && categoriaErro && <> Categoria: {CATEGORIAS_ERRO.find(([v]) => v === categoriaErro)?.[1]}.</>}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmando(false)}>Cancelar</Button>
            <Button disabled={mut.isPending}
              onClick={() => {
                setConfirmando(false);
                const corpo = resultado === 'erro'
                  ? { versao: d.versao, resultado, ordens_criadas: [], categoria_erro: categoriaErro, observacao: obsFinal.trim() }
                  : { versao: d.versao, resultado, ordens_criadas: [] };
                mut.mutate(() => post('finalizar', corpo, 'Solicitação finalizada', () => navigate('/fila')));
              }}>
              Confirmar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
