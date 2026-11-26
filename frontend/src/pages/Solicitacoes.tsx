import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useAuth } from '@/contexts/AuthContext';

// Tela "Solicitações": quem atende vê todos os chamados (abertos e finalizados); o solicitante vê os próprios.
// A busca procura por código (GO-12), centro de custo, conta, solicitante, fornecedor e local de obra.
interface Item {
  id: string;
  codigo: string;
  tipo_solicitacao: string;
  status: string;
  created_at: string;
  centro_custo_codigo?: string | null;
  centro_custo_nome?: string | null;
  solicitante_nome?: string | null;
  atendente_nome?: string | null;
  local_obra?: string | null;
  valor: number;
  compartilhado_por?: string | null;
}
interface Resposta { items: Item[]; pagina: number; tamanho: number; tem_mais: boolean }

const TIPO: Record<string, string> = { transferencia: 'Transferência', inclusao: 'Inclusão', inclusao_sfc: 'Inclusão SFC', imobilizado: 'Imobilizado', obras: 'Obras' };
const STATUS: Record<string, string> = {
  aguardando_aprovacao: 'Aguardando aprovação', aberta: 'Aguardando atendimento', em_atendimento: 'Em atendimento',
  pendente: 'Pendente (com o solicitante)', finalizada_sucesso: 'Finalizada com sucesso', finalizada_erro: 'Finalizada com erro', rejeitada: 'Rejeitada', cancelada: 'Cancelada',
};
const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });
const dataHora = (iso: string) => new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
const TODOS = '__todos__';

function variante(status: string): 'default' | 'destructive' | 'secondary' {
  if (status === 'finalizada_erro' || status === 'rejeitada') return 'destructive';
  if (status === 'finalizada_sucesso') return 'secondary';
  return 'default';
}

const Solicitacoes = () => {
  const { user } = useAuth();
  const atende = user?.perfil === 'administrador' || user?.perfil === 'atendente';
  const [busca, setBusca] = useState('');
  const [termo, setTermo] = useState(''); // busca aplicada (com atraso, para não consultar a cada tecla)
  const [status, setStatus] = useState(TODOS);
  const [tipo, setTipo] = useState(TODOS);
  const [de, setDe] = useState('');
  const [ate, setAte] = useState('');
  const [pagina, setPagina] = useState(1);

  useEffect(() => {
    const t = setTimeout(() => { setTermo(busca.trim()); setPagina(1); }, 400);
    return () => clearTimeout(t);
  }, [busca]);

  const params = new URLSearchParams();
  if (termo) params.set('q', termo);
  if (status !== TODOS) params.set('status', status);
  if (tipo !== TODOS) params.set('tipo', tipo);
  if (de) params.set('de', de);
  if (ate) params.set('ate', ate);
  params.set('pagina', String(pagina));

  const q = useQuery({
    queryKey: ['solicitacoes-busca', params.toString()],
    queryFn: async () => {
      const resp = await fetch(`/api/solicitacoes/busca?${params.toString()}`);
      const dados = await resp.json().catch(() => ({}));
      if (!resp.ok) throw new Error(dados?.error || `Erro HTTP ${resp.status}`);
      return dados as Resposta;
    },
  });

  const limpar = () => { setBusca(''); setTermo(''); setStatus(TODOS); setTipo(TODOS); setDe(''); setAte(''); setPagina(1); };
  const filtrando = !!(termo || status !== TODOS || tipo !== TODOS || de || ate);
  // quem atende abre o chamado dentro da fila; o solicitante, na própria rota
  const destino = (id: string) => (atende ? `/fila/${id}` : `/solicitacoes/${id}`);

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-6">
      <div>
        <h1 className="text-lg font-semibold text-foreground">{atende ? 'Solicitações' : 'Minhas solicitações'}</h1>
        <p className="text-sm text-muted-foreground">
          {atende ? 'Todos os chamados, abertos e finalizados.' : 'Acompanhe o andamento e o resultado do que você pediu.'}
        </p>
      </div>

      <section className="grid gap-3 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-5">
        <div className="grid gap-1.5 sm:col-span-2">
          <Label htmlFor="busca">Buscar</Label>
          <Input id="busca" value={busca} onChange={(e) => setBusca(e.target.value)}
            placeholder={atende ? 'Código (GO-12), centro de custo, conta, solicitante…' : 'Código (GO-12), centro de custo, conta…'} />
        </div>
        <div className="grid gap-1.5">
          <Label>Status</Label>
          <Select value={status} onValueChange={(v) => { setStatus(v); setPagina(1); }}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={TODOS}>Todos</SelectItem>
              {Object.entries(STATUS).map(([v, r]) => <SelectItem key={v} value={v}>{r}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label>Tipo</Label>
          <Select value={tipo} onValueChange={(v) => { setTipo(v); setPagina(1); }}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={TODOS}>Todos</SelectItem>
              {Object.entries(TIPO).map(([v, r]) => <SelectItem key={v} value={v}>{r}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="de">Aberta de</Label>
          <Input id="de" type="date" value={de} onChange={(e) => { setDe(e.target.value); setPagina(1); }} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="ate">Aberta até</Label>
          <Input id="ate" type="date" value={ate} onChange={(e) => { setAte(e.target.value); setPagina(1); }} />
        </div>
        <div className="flex items-end">
          {filtrando && <Button variant="outline" size="sm" onClick={limpar}>Limpar filtros</Button>}
        </div>
      </section>

      {q.isLoading && <p className="text-sm text-muted-foreground">Carregando…</p>}
      {q.error && <p className="text-sm text-destructive">{(q.error as Error).message}</p>}
      {q.data && q.data.items.length === 0 && (
        <p className="rounded-lg border border-border p-6 text-center text-sm text-muted-foreground">
          {filtrando ? 'Nenhuma solicitação encontrada com esses filtros.' : 'Nenhuma solicitação ainda.'}
        </p>
      )}
      {q.data && q.data.items.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr>
                <th className="p-2">Código</th><th className="p-2">Tipo</th><th className="p-2">Centro de custo / obra</th>
                {atende && <th className="p-2">Solicitante</th>}
                <th className="p-2">Aberta em</th><th className="p-2 text-right">Valor</th><th className="p-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {q.data.items.map((i) => (
                <tr key={i.id} className="border-t border-border hover:bg-muted/40">
                  <td className="p-2 font-medium tabular-nums"><Link className="text-link underline underline-offset-2" to={destino(i.id)}>{i.codigo}</Link></td>
                  <td className="p-2">{TIPO[i.tipo_solicitacao] ?? i.tipo_solicitacao}</td>
                  <td className="max-w-[16rem] p-2">
                    {i.centro_custo_codigo ? (
                      <span className="flex flex-col"><span>{i.centro_custo_codigo}</span><span className="truncate text-xs text-fg-helper">{i.centro_custo_nome}</span></span>
                    ) : (i.local_obra ?? '—')}
                  </td>
                  {atende && <td className="p-2">{i.solicitante_nome ?? '—'}</td>}
                  <td className="p-2 tabular-nums">{dataHora(i.created_at)}</td>
                  <td className="p-2 text-right tabular-nums">{moeda.format(i.valor)}</td>
                  <td className="p-2"><Badge variant={variante(i.status)}>{STATUS[i.status] ?? i.status}</Badge>{i.compartilhado_por && <span className="mt-1 block text-xs text-fg-helper">Em cópia{i.compartilhado_por.startsWith("grupo ") ? ` (${i.compartilhado_por})` : ` · incluído por ${i.compartilhado_por}`}</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {q.data && (pagina > 1 || q.data.tem_mais) && (
        <div className="flex items-center justify-between">
          <Button variant="outline" size="sm" disabled={pagina <= 1} onClick={() => setPagina((p) => p - 1)}>Anterior</Button>
          <span className="text-xs text-muted-foreground">Página {pagina}</span>
          <Button variant="outline" size="sm" disabled={!q.data.tem_mais} onClick={() => setPagina((p) => p + 1)}>Próxima</Button>
        </div>
      )}
    </div>
  );
};

export default Solicitacoes;
