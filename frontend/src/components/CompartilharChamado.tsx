import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

// Visualizadores do chamado (resposta da Controladoria de 26/11, §9.1): pessoas e grupos em cópia. Veem dados, anexos,
// andamento e comentários (nunca a nota interna), recebem os avisos por e-mail e podem comentar. Quem abriu o chamado e o
// atendimento incluem e retiram; quem acompanha só vê a lista.
export interface Pessoa { id: string; nome: string; email: string }
export interface GrupoOpcao { id: string; nome: string; descricao?: string }

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const dados = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(dados?.error || `Erro HTTP ${resp.status}`);
  return dados as T;
}

type Lista = { items: Pessoa[]; grupos: { id: string; nome: string }[] };

// Busca de usuário ativo por nome ou e-mail (3 letras), reaproveitada no formulário de abertura e nos grupos.
export function BuscaPessoa({ id, onEscolher, ignorar = [], placeholder = 'Digite ao menos 3 letras do nome ou e-mail' }: {
  id?: string;
  onEscolher: (p: Pessoa) => void;
  ignorar?: string[];
  placeholder?: string;
}) {
  const [busca, setBusca] = useState('');
  const [termo, setTermo] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTermo(busca.trim()), 300);
    return () => clearTimeout(t);
  }, [busca]);
  const sugestoes = useQuery({
    queryKey: ['usuarios-busca', termo],
    queryFn: () => api<{ items: Pessoa[] }>(`/api/usuarios/busca?q=${encodeURIComponent(termo)}`),
    enabled: termo.length >= 3,
  });
  const fora = new Set(ignorar);
  const opcoes = (sugestoes.data?.items ?? []).filter((p) => !fora.has(p.id));
  return (
    <div className="relative grid gap-1">
      <Input id={id} value={busca} onChange={(e) => setBusca(e.target.value)} onBlur={() => setTimeout(() => setBusca(''), 150)} placeholder={placeholder} autoComplete="off" />
      {termo.length >= 3 && busca.trim() === termo && (
        <div className="absolute top-full z-30 mt-1 w-full rounded-md border border-border bg-card shadow-popover" role="listbox">
          {sugestoes.isLoading && <p className="p-3 text-xs text-fg-helper">Procurando…</p>}
          {!sugestoes.isLoading && opcoes.length === 0 && <p className="p-3 text-xs text-fg-helper">Nenhum usuário encontrado.</p>}
          {opcoes.map((p) => (
            <button key={p.id} type="button" role="option" aria-selected={false}
              className="flex w-full flex-col px-3 py-2 text-left text-sm hover:bg-muted/60"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => { onEscolher(p); setBusca(''); }}>
              <span>{p.nome}</span><span className="text-xs text-fg-helper">{p.email}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function CompartilharChamado({ solicitacaoId, podeGerir }: { solicitacaoId: string; podeGerir: boolean }) {
  const qc = useQueryClient();
  const [grupoEscolhido, setGrupoEscolhido] = useState('');

  const lista = useQuery({
    queryKey: ['compartilhamentos', solicitacaoId],
    queryFn: () => api<Lista>(`/api/compartilhamentos/${solicitacaoId}`),
  });
  const grupos = useQuery({
    queryKey: ['grupos-opcoes'],
    queryFn: () => api<{ items: GrupoOpcao[] }>('/api/grupos/opcoes'),
    enabled: podeGerir,
  });

  const depois = (d: Lista, msg: string) => {
    qc.setQueryData(['compartilhamentos', solicitacaoId], d);
    qc.invalidateQueries({ queryKey: ['solicitacao', solicitacaoId] });
    toast.success(msg);
  };
  const incluirPessoa = useMutation({
    mutationFn: (p: Pessoa) =>
      api<Lista>(`/api/compartilhamentos/${solicitacaoId}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ usuario_id: p.id }) }).then((d) => ({ d, p })),
    onSuccess: ({ d, p }) => depois(d, `${p.nome} incluído em cópia. A pessoa recebe um e-mail com o link.`),
    onError: (e: Error) => toast.error(e.message),
  });
  const retirarPessoa = useMutation({
    mutationFn: (p: Pessoa) => api<Lista>(`/api/compartilhamentos/${solicitacaoId}/${p.id}`, { method: 'DELETE' }),
    onSuccess: (d) => depois(d, 'Visualizador retirado'),
    onError: (e: Error) => toast.error(e.message),
  });
  const incluirGrupo = useMutation({
    mutationFn: (gid: string) =>
      api<Lista>(`/api/compartilhamentos/${solicitacaoId}/grupos`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ grupo_id: gid }) }),
    onSuccess: (d) => { setGrupoEscolhido(''); depois(d, 'Grupo incluído em cópia'); },
    onError: (e: Error) => toast.error(e.message),
  });
  const retirarGrupo = useMutation({
    mutationFn: (gid: string) => api<Lista>(`/api/compartilhamentos/${solicitacaoId}/grupos/${gid}`, { method: 'DELETE' }),
    onSuccess: (d) => depois(d, 'Grupo retirado da cópia'),
    onError: (e: Error) => toast.error(e.message),
  });

  const pessoas = lista.data?.items ?? [];
  const gruposDoChamado = lista.data?.grupos ?? [];
  const gruposLivres = (grupos.data?.items ?? []).filter((g) => !gruposDoChamado.some((x) => x.id === g.id));

  return (
    <section className="grid gap-3 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-sm font-semibold">Em cópia (visualizadores)</h2>
        <p className="text-xs text-muted-foreground">Acompanham o chamado, recebem os avisos por e-mail e podem comentar. Não veem as notas internas nem agem no atendimento.</p>
      </div>
      {podeGerir && (
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <Label htmlFor="visualizador-busca">Incluir pessoa</Label>
            <BuscaPessoa id="visualizador-busca" ignorar={pessoas.map((p) => p.id)} onEscolher={(p) => incluirPessoa.mutate(p)} />
          </div>
          <div className="grid gap-1.5">
            <Label>Incluir grupo</Label>
            <div className="flex gap-2">
              <Select value={grupoEscolhido} onValueChange={setGrupoEscolhido}>
                <SelectTrigger><SelectValue placeholder={gruposLivres.length ? 'Escolha o grupo' : 'Nenhum grupo disponível'} /></SelectTrigger>
                <SelectContent>
                  {gruposLivres.map((g) => <SelectItem key={g.id} value={g.id}>{g.nome}</SelectItem>)}
                </SelectContent>
              </Select>
              <Button size="sm" variant="outline" className="h-10" disabled={!grupoEscolhido || incluirGrupo.isPending} onClick={() => incluirGrupo.mutate(grupoEscolhido)}>Incluir</Button>
            </div>
          </div>
        </div>
      )}
      {pessoas.length === 0 && gruposDoChamado.length === 0 ? (
        <p className="text-xs text-muted-foreground">Ninguém em cópia.</p>
      ) : (
        <ul className="grid gap-1.5">
          {gruposDoChamado.map((g) => (
            <li key={g.id} className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-1.5 text-sm">
              <span className="min-w-0"><span className="block truncate">Grupo {g.nome}</span><span className="block text-xs text-fg-helper">vale quem está no grupo hoje</span></span>
              {podeGerir && <Button size="sm" variant="outline" disabled={retirarGrupo.isPending} onClick={() => retirarGrupo.mutate(g.id)}>Retirar</Button>}
            </li>
          ))}
          {pessoas.map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-1.5 text-sm">
              <span className="min-w-0"><span className="block truncate">{p.nome}</span><span className="block truncate text-xs text-fg-helper">{p.email}</span></span>
              {podeGerir && <Button size="sm" variant="outline" disabled={retirarPessoa.isPending} onClick={() => retirarPessoa.mutate(p)}>Retirar</Button>}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
