import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';

// Divisões: FL e Externo (F4, segunda leva de 26/11). Cada divisão tem duas marcas independentes, mantidas pela
// Controladoria: faz FL e faz compra externa. Elas filtram as divisões do DESTINO da Transferência pelo Tipo escolhido
// (a origem não tem Tipo). Mudar aqui muda a lista do formulário na hora, sem publicar versão nova. Cada mudança fica
// no histórico do cadastro, com quem e quando.
interface Divisao { id: string; codigo: string; nome: string; faz_fl: boolean; faz_externo: boolean }

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const dados = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(dados?.error || `Erro HTTP ${resp.status}`);
  return dados as T;
}

export default function DivisoesMarcas() {
  const qc = useQueryClient();
  const [busca, setBusca] = useState('');
  const { data, isLoading, error } = useQuery({ queryKey: ['divisoes-marcas'], queryFn: () => api<{ items: Divisao[] }>('/api/divisoes/marcas') });
  const salvar = useMutation({
    mutationFn: (p: { id: string; faz_fl?: boolean; faz_externo?: boolean }) =>
      api<Divisao>(`/api/divisoes/${p.id}/marcas`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p) }),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ['divisoes-marcas'] });
      qc.invalidateQueries({ queryKey: ['formulario-opcoes'] });
      toast.success(`Divisão ${d.codigo}: FL ${d.faz_fl ? 'sim' : 'não'} · Externo ${d.faz_externo ? 'sim' : 'não'}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const itens = useMemo(() => {
    const t = busca.trim().toLowerCase();
    return (data?.items ?? []).filter((d) => !t || `${d.codigo} ${d.nome}`.toLowerCase().includes(t));
  }, [data, busca]);
  const total = data?.items ?? [];

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-6">
      <div>
        <h1 className="text-lg font-semibold">Divisões: FL e Externo</h1>
        <p className="text-xs text-fg-helper">
          Marcas independentes de cada divisão. No destino da Transferência, o Tipo FL lista as divisões que fazem FL e o Tipo Externo, as que fazem
          compra externa. Hoje: {total.filter((d) => d.faz_fl).length} fazem FL, {total.filter((d) => d.faz_externo).length} fazem compra externa,
          {' '}{total.filter((d) => d.faz_fl && d.faz_externo).length} fazem as duas.
        </p>
      </div>
      <Input className="max-w-xs" placeholder="Buscar por código ou nome" value={busca} onChange={(e) => setBusca(e.target.value)} />
      {isLoading && <p className="text-sm text-fg-helper">Carregando…</p>}
      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}
      {data && (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-xs text-fg-helper">
              <tr>
                <th className="p-2 text-left font-medium">Divisão</th>
                <th className="p-2 text-left font-medium">Filial</th>
                <th className="p-2 text-center font-medium">Faz FL</th>
                <th className="p-2 text-center font-medium">Faz compra externa</th>
              </tr>
            </thead>
            <tbody>
              {itens.map((d) => (
                <tr key={d.id} className="border-t border-border">
                  <td className="p-2 font-medium tabular-nums">{d.codigo}</td>
                  <td className="p-2 text-fg-secondary">{d.nome !== d.codigo ? d.nome : '—'}</td>
                  <td className="p-2 text-center">
                    <Switch aria-label={`Divisão ${d.codigo} faz FL`} checked={d.faz_fl} disabled={salvar.isPending} onCheckedChange={(v) => salvar.mutate({ id: d.id, faz_fl: v })} />
                  </td>
                  <td className="p-2 text-center">
                    <Switch aria-label={`Divisão ${d.codigo} faz compra externa`} checked={d.faz_externo} disabled={salvar.isPending} onCheckedChange={(v) => salvar.mutate({ id: d.id, faz_externo: v })} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
