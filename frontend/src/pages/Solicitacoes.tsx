import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useAuth } from '@/contexts/AuthContext';
import { buscaCC, formatarCC, partesCC } from '@/lib/centroCusto';
import { BuscaCombobox } from '@/components/BuscaCombobox';

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

interface Opcao { codigo: string; nome: string }

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
  // F11 (26/11): filial/divisão, CC e conta, no lado escolhido (origem, destino ou qualquer)
  const [divisao, setDivisao] = useState('');
  const [cc, setCc] = useState('');
  const [conta, setConta] = useState('');
  const [lado, setLado] = useState('qualquer');
  const filtros = useQuery({
    queryKey: ['solicitacoes-filtros'],
    queryFn: async () => {
      const resp = await fetch('/api/solicitacoes/filtros');
      if (!resp.ok) throw new Error(`Erro HTTP ${resp.status}`);
      return (await resp.json()) as { divisoes: Opcao[]; centros_custo: Opcao[]; contas: Opcao[] };
    },
  });
  const opcoesCC = useMemo(
    () =>
      (filtros.data?.centros_custo ?? [])
        .filter((c) => !divisao || partesCC(c.codigo).divisao === divisao)
        .map((c) => ({ id: c.codigo, codigo: formatarCC(c.codigo), nome: c.nome, busca: buscaCC(c.codigo) })),
    [filtros.data, divisao],
  );

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
  if (divisao) params.set('divisao', divisao);
  if (cc) params.set('cc', cc);
  if (conta) params.set('conta', conta);
  if ((divisao || cc || conta) && lado !== 'qualquer') params.set('lado', lado);
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

  const limpar = () => {
    setBusca(''); setTermo(''); setStatus(TODOS); setTipo(TODOS); setDe(''); setAte('');
    setDivisao(''); setCc(''); setConta(''); setLado('qualquer'); setPagina(1);
  };
  const filtrando = !!(termo || status !== TODOS || tipo !== TODOS || de || ate || divisao || cc || conta);
  // etiquetas removíveis dos filtros aplicados
  const etiquetas: { rotulo: string; tirar: () => void }[] = [];
  if (status !== TODOS) etiquetas.push({ rotulo: `Status: ${STATUS[status] ?? status}`, tirar: () => setStatus(TODOS) });
  if (tipo !== TODOS) etiquetas.push({ rotulo: `Tipo: ${TIPO[tipo] ?? tipo}`, tirar: () => setTipo(TODOS) });
  if (de || ate) etiquetas.push({ rotulo: `Abertura: ${de || '…'} a ${ate || '…'}`, tirar: () => { setDe(''); setAte(''); } });
  if (divisao) etiquetas.push({ rotulo: `Filial: ${divisao}`, tirar: () => { setDivisao(''); setCc(''); } });
  if (cc) etiquetas.push({ rotulo: `CC: ${formatarCC(cc)}`, tirar: () => setCc('') });
  if (conta) etiquetas.push({ rotulo: `Conta: ${conta}`, tirar: () => setConta('') });
  if ((divisao || cc || conta) && lado !== 'qualquer') etiquetas.push({ rotulo: `Lado: ${lado === 'origem' ? 'Origem' : 'Destino'}`, tirar: () => setLado('qualquer') });
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
        <div className="grid min-w-0 gap-1.5">
          <Label>Filial / divisão</Label>
          <BuscaCombobox
            value={divisao}
            onChange={(v) => { setDivisao(v); setCc(''); setPagina(1); }}
            opcoes={(filtros.data?.divisoes ?? []).map((d) => ({ id: d.codigo, codigo: d.codigo, nome: d.nome || 'Filial' }))}
            placeholder="Todas"
            vazio="Nenhuma filial nos seus chamados."
            limparRotulo="Todas"
            ariaLabel="Filial ou divisão"
          />
        </div>
        <div className="grid min-w-0 gap-1.5">
          <Label>Centro de custo</Label>
          <BuscaCombobox
            value={cc}
            onChange={(v) => { setCc(v); setPagina(1); }}
            opcoes={opcoesCC}
            placeholder="Todos"
            vazio="Nenhum centro de custo nos seus chamados."
            limparRotulo="Todos"
            ariaLabel="Centro de custo"
          />
        </div>
        <div className="grid min-w-0 gap-1.5">
          <Label>Conta</Label>
          <BuscaCombobox
            value={conta}
            onChange={(v) => { setConta(v); setPagina(1); }}
            opcoes={(filtros.data?.contas ?? []).map((c) => ({ id: c.codigo, codigo: c.codigo, nome: c.nome }))}
            placeholder="Todas"
            vazio="Nenhuma conta nos seus chamados."
            limparRotulo="Todas"
            ariaLabel="Conta"
          />
        </div>
        <div className="grid gap-1.5">
          <Label>Lado (Transferência)</Label>
          <Select value={lado} onValueChange={(v) => { setLado(v); setPagina(1); }} disabled={!divisao && !cc && !conta}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="qualquer">Qualquer lado</SelectItem>
              <SelectItem value="origem">Origem</SelectItem>
              <SelectItem value="destino">Destino</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-end">
          {filtrando && <Button variant="outline" size="sm" onClick={limpar}>Limpar filtros</Button>}
        </div>
      </section>
      {etiquetas.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {etiquetas.map((e) => (
            <span key={e.rotulo} className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-xs">
              {e.rotulo}
              <button type="button" aria-label={`Tirar o filtro ${e.rotulo}`} className="text-fg-helper hover:text-foreground" onClick={() => { e.tirar(); setPagina(1); }}>×</button>
            </span>
          ))}
        </div>
      )}

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
                      <span className="flex flex-col"><span className="tabular-nums">{formatarCC(i.centro_custo_codigo)}</span><span className="truncate text-xs text-fg-helper">{i.centro_custo_nome}</span></span>
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
