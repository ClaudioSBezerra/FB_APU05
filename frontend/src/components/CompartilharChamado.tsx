import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// Compartilhar o chamado (somente leitura) com usuários já cadastrados. Só aparece para quem abriu o chamado.
interface Pessoa { id: string; nome: string; email: string }

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const dados = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(dados?.error || `Erro HTTP ${resp.status}`);
  return dados as T;
}

export function CompartilharChamado({ solicitacaoId }: { solicitacaoId: string }) {
  const qc = useQueryClient();
  const [busca, setBusca] = useState('');
  const [termo, setTermo] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTermo(busca.trim()), 300);
    return () => clearTimeout(t);
  }, [busca]);

  const lista = useQuery({
    queryKey: ['compartilhamentos', solicitacaoId],
    queryFn: () => api<{ items: Pessoa[] }>(`/api/solicitacoes/${solicitacaoId}/compartilhamentos`),
  });
  const sugestoes = useQuery({
    queryKey: ['usuarios-busca', termo],
    queryFn: () => api<{ items: Pessoa[] }>(`/api/usuarios/busca?q=${encodeURIComponent(termo)}`),
    enabled: termo.length >= 3,
  });

  const atualizar = (dados: { items: Pessoa[] }) => qc.setQueryData(['compartilhamentos', solicitacaoId], dados);
  const compartilhar = useMutation({
    mutationFn: (p: Pessoa) =>
      api<{ items: Pessoa[] }>(`/api/solicitacoes/${solicitacaoId}/compartilhamentos`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ usuario_id: p.id }),
      }).then((d) => ({ d, p })),
    onSuccess: ({ d, p }) => { atualizar(d); setBusca(''); toast.success(`Chamado compartilhado com ${p.nome}. A pessoa recebe um e-mail com o link.`); qc.invalidateQueries({ queryKey: ['solicitacao', solicitacaoId] }); },
    onError: (e: Error) => toast.error(e.message),
  });
  const retirar = useMutation({
    mutationFn: (p: Pessoa) => api<{ items: Pessoa[] }>(`/api/solicitacoes/${solicitacaoId}/compartilhamentos/${p.id}`, { method: 'DELETE' }),
    onSuccess: (d) => { atualizar(d); toast.success('Acesso retirado'); qc.invalidateQueries({ queryKey: ['solicitacao', solicitacaoId] }); },
    onError: (e: Error) => toast.error(e.message),
  });

  const jaTem = new Set((lista.data?.items ?? []).map((p) => p.id));
  const opcoes = (sugestoes.data?.items ?? []).filter((p) => !jaTem.has(p.id));

  return (
    <section className="grid gap-3 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-sm font-semibold">Compartilhar este chamado</h2>
        <p className="text-xs text-muted-foreground">As pessoas escolhidas acompanham o andamento (somente leitura) e recebem um e-mail com o link.</p>
      </div>
      <div className="relative grid gap-1.5">
        <Label htmlFor="compartilhar-busca">Nome ou e-mail do usuário</Label>
        <Input id="compartilhar-busca" value={busca} onChange={(e) => setBusca(e.target.value)} placeholder="Digite ao menos 3 letras" autoComplete="off" />
        {termo.length >= 3 && busca.trim() === termo && (
          <div className="rounded-md border border-border bg-card shadow-popover" role="listbox">
            {sugestoes.isLoading && <p className="p-3 text-xs text-fg-helper">Procurando…</p>}
            {!sugestoes.isLoading && opcoes.length === 0 && <p className="p-3 text-xs text-fg-helper">Nenhum usuário encontrado.</p>}
            {opcoes.map((p) => (
              <button key={p.id} type="button" role="option" aria-selected={false} disabled={compartilhar.isPending}
                className="flex w-full flex-col px-3 py-2 text-left text-sm hover:bg-muted/60"
                onClick={() => compartilhar.mutate(p)}>
                <span>{p.nome}</span><span className="text-xs text-fg-helper">{p.email}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      {(lista.data?.items ?? []).length > 0 ? (
        <ul className="grid gap-1.5">
          {(lista.data?.items ?? []).map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-1.5 text-sm">
              <span className="min-w-0"><span className="block truncate">{p.nome}</span><span className="block truncate text-xs text-fg-helper">{p.email}</span></span>
              <Button size="sm" variant="outline" disabled={retirar.isPending} onClick={() => retirar.mutate(p)}>Retirar acesso</Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-xs text-muted-foreground">Ainda não compartilhado com ninguém.</p>
      )}
    </section>
  );
}
