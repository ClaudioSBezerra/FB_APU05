import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Badge } from '@/components/ui/badge';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { BuscaPessoa, type Pessoa } from '@/components/CompartilharChamado';

// Grupos de pessoas em cópia (resposta da Controladoria de 26/11, §9.2). A Controladoria cria o grupo e sempre define o
// gestor da área; depois o gestor edita os membros, e a Controladoria também pode editar tudo e trocar o gestor. Grupo
// sem gestor não pode ser salvo. O histórico do grupo guarda quem mudou o quê e quando.
interface Grupo { id: string; nome: string; descricao: string; gestor: Pessoa; ativo: boolean; membros: Pessoa[] }

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const dados = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(dados?.error || `Erro HTTP ${resp.status}`);
  return dados as T;
}

const dataHora = (iso: string) => new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' });

function Historico({ grupoId }: { grupoId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['grupo-historico', grupoId],
    queryFn: () => api<{ items: { texto: string; autor: string; quando: string }[] }>(`/api/grupos/${grupoId}/historico`),
  });
  if (isLoading) return <p className="text-xs text-fg-helper">Carregando…</p>;
  return (
    <ul className="grid max-h-60 gap-1 overflow-y-auto text-xs">
      {(data?.items ?? []).map((h, i) => (
        <li key={i} className="rounded border border-border px-2 py-1">
          <span className="text-fg-helper">{dataHora(h.quando)} · {h.autor || '—'}:</span> {h.texto}
        </li>
      ))}
      {(data?.items ?? []).length === 0 && <li className="text-fg-helper">Sem registros.</li>}
    </ul>
  );
}

function FormGrupo({ grupo, controladoria, onFechar }: { grupo: Grupo | null; controladoria: boolean; onFechar: () => void }) {
  const qc = useQueryClient();
  const [nome, setNome] = useState(grupo?.nome ?? '');
  const [descricao, setDescricao] = useState(grupo?.descricao ?? '');
  const [gestor, setGestor] = useState<Pessoa | null>(grupo?.gestor ?? null);
  const [ativo, setAtivo] = useState(grupo?.ativo ?? true);
  const [membros, setMembros] = useState<Pessoa[]>(grupo?.membros ?? []);
  const [verHistorico, setVerHistorico] = useState(false);

  const salvar = useMutation({
    mutationFn: () =>
      api(grupo ? `/api/grupos/${grupo.id}` : '/api/grupos', {
        method: grupo ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nome, descricao, gestor_id: gestor?.id ?? '', ativo, membros: membros.map((m) => m.id) }),
      }),
    onSuccess: () => {
      toast.success(grupo ? 'Grupo atualizado' : 'Grupo criado');
      qc.invalidateQueries({ queryKey: ['grupos'] });
      qc.invalidateQueries({ queryKey: ['grupos-opcoes'] });
      onFechar();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const faltaGestor = controladoria && !gestor;
  return (
    <Dialog open onOpenChange={(o) => !o && onFechar()}>
      <DialogContent className="max-h-[90vh] max-w-xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{grupo ? `Grupo ${grupo.nome}` : 'Novo grupo'}</DialogTitle>
          <DialogDescription>{controladoria ? 'Nome, gestor responsável e membros.' : 'Como gestor, você edita os membros deste grupo.'}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          {controladoria && (
            <>
              <div className="grid gap-1.5"><Label htmlFor="g-nome">Nome</Label><Input id="g-nome" maxLength={120} value={nome} onChange={(e) => setNome(e.target.value)} placeholder="Ex.: Grupo Expansão" /></div>
              <div className="grid gap-1.5"><Label htmlFor="g-desc">Descrição</Label><Textarea id="g-desc" maxLength={500} rows={2} value={descricao} onChange={(e) => setDescricao(e.target.value)} /></div>
              <div className="grid gap-1.5">
                <Label>Gestor responsável (obrigatório)</Label>
                {gestor ? (
                  <div className="flex items-center justify-between rounded-md border border-border px-3 py-1.5 text-sm">
                    <span>{gestor.nome} <span className="text-xs text-fg-helper">{gestor.email}</span></span>
                    <Button size="sm" variant="ghost" onClick={() => setGestor(null)}>Trocar</Button>
                  </div>
                ) : (
                  <BuscaPessoa onEscolher={setGestor} placeholder="Nome ou e-mail do gestor da área" />
                )}
              </div>
              <label className="flex w-fit cursor-pointer items-center gap-2 text-sm">
                <input type="checkbox" className="size-4 accent-[var(--blue-d50)]" checked={ativo} onChange={(e) => setAtivo(e.target.checked)} /> Ativo
              </label>
            </>
          )}
          <div className="grid gap-1.5">
            <Label>Membros ({membros.length})</Label>
            <BuscaPessoa ignorar={membros.map((m) => m.id)} onEscolher={(p) => setMembros((a) => [...a, p])} placeholder="Incluir membro: nome ou e-mail" />
            <ul className="grid max-h-56 gap-1 overflow-y-auto">
              {membros.map((m) => (
                <li key={m.id} className="flex items-center justify-between rounded-md border border-border px-3 py-1 text-sm">
                  <span className="min-w-0 truncate">{m.nome} <span className="text-xs text-fg-helper">{m.email}</span></span>
                  <Button size="sm" variant="ghost" onClick={() => setMembros((a) => a.filter((x) => x.id !== m.id))}>Remover</Button>
                </li>
              ))}
              {membros.length === 0 && <li className="text-xs text-fg-helper">Nenhum membro ainda.</li>}
            </ul>
          </div>
          {grupo && (
            <div className="grid gap-1.5">
              <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => setVerHistorico((v) => !v)}>{verHistorico ? 'Esconder histórico' : 'Ver histórico do grupo'}</Button>
              {verHistorico && <Historico grupoId={grupo.id} />}
            </div>
          )}
        </div>
        <DialogFooter>
          {faltaGestor && <p className="mr-auto self-center text-xs text-fg-helper">Grupo sem gestor não pode ser salvo.</p>}
          <Button variant="outline" onClick={onFechar}>Cancelar</Button>
          <Button disabled={salvar.isPending || faltaGestor || (controladoria && !nome.trim())} onClick={() => salvar.mutate()}>{salvar.isPending ? 'Salvando…' : 'Salvar'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function Grupos() {
  const { data, isLoading, error } = useQuery({ queryKey: ['grupos'], queryFn: () => api<{ items: Grupo[]; pode_criar: boolean }>('/api/grupos') });
  const [editando, setEditando] = useState<Grupo | null | 'novo'>(null);
  const controladoria = !!data?.pode_criar;

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">{controladoria ? 'Grupos de pessoas' : 'Meus grupos'}</h1>
          <p className="text-xs text-fg-helper">Grupos que podem ser colocados em cópia nos chamados. Vale quem está no grupo hoje.</p>
        </div>
        {controladoria && <Button size="sm" onClick={() => setEditando('novo')}>Novo grupo</Button>}
      </div>
      {isLoading && <p className="text-sm text-fg-helper">Carregando…</p>}
      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}
      {data && data.items.length === 0 && <p className="text-sm text-fg-helper">{controladoria ? 'Nenhum grupo cadastrado.' : 'Você não é gestor de nenhum grupo.'}</p>}
      <ul className="grid gap-2">
        {(data?.items ?? []).map((g) => (
          <li key={g.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-3">
            <div className="min-w-0">
              <p className="text-sm font-medium">{g.nome} {!g.ativo && <Badge variant="secondary" className="ml-1">Inativo</Badge>}</p>
              <p className="text-xs text-fg-helper">Gestor: {g.gestor.nome} · {g.membros.length} membro(s){g.descricao ? ` · ${g.descricao}` : ''}</p>
            </div>
            <Button size="sm" variant="outline" onClick={() => setEditando(g)}>{controladoria ? 'Editar' : 'Editar membros'}</Button>
          </li>
        ))}
      </ul>
      {editando && <FormGrupo grupo={editando === 'novo' ? null : editando} controladoria={controladoria} onFechar={() => setEditando(null)} />}
    </div>
  );
}
