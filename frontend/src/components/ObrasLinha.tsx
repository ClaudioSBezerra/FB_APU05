import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

// Linha de Obras (F7, F8, F8b e F16 de 26/11): ordem resolvida pelo grão, tipo da ordem e nome do subgrupo.
// Usado no formulário de abertura e na edição pela Controladoria (F15).

async function fetchJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url);
  const data = await resp.json();
  if (!resp.ok) throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  return data as T;
}

export type TipoOrdem = '' | 'FL' | 'Externa';

// F16: o tipo que vale na linha — Inclusão é sempre Externa, FL é sempre FL; nas demais, o escolhido.
export function tipoDaLinha(classificacao: string, l: { tipo: TipoOrdem }): TipoOrdem {
  if (classificacao === 'inclusao') return 'Externa';
  if (classificacao === 'fl') return 'FL';
  return l.tipo;
}

// F8 (26/11): em Fortaleza, Loja e ZPA são subgrupos diferentes — o sufixo fica visível ("Terraplanagem — Loja").
export function nomeSubgrupo(codigo: string, nome: string): string {
  const no = codigo.split('.')[0].toUpperCase();
  const suf = no.endsWith('LOJA') ? 'Loja' : no.endsWith('ZPA') ? 'ZPA' : '';
  if (!suf) return nome;
  let base = nome.replace(/\s*\(?(LOJA|ZPA)\)?\s*$/i, '').trim();
  if (base === base.toUpperCase()) base = base.charAt(0) + base.slice(1).toLowerCase();
  return `${base} — ${suf}`;
}


// OrdemDaLinha (F7/F8, 26/11) — a ordem é resolvida pelo grão (local, subgrupo e tipo de despesa, que sai da
// classificação), como na referência: existe → preenchida e travada; não existe e a classificação permite → "CRIAR"
// travado; retirada/origem sem ordem → bloqueia; obra sem estrutura → digitada; duas ordens no grão → escolhe pela
// coluna de faturamento. A linha de ajuda sob o campo diz de onde veio a ordem.
interface OrdemCandidata { ordem: string; tipo_faturamento: string; colunas: string; iea: string; bloqueada?: boolean; observacao?: string }
interface RespostaOrdem {
  situacao: 'existe' | 'criar' | 'exige_existente' | 'sem_estrutura' | 'ambigua' | 'fora_da_familia' | 'bloqueada';
  ordem: string;
  dica: string;
  candidatas: OrdemCandidata[];
}

export function OrdemDaLinha({
  localObraId,
  subgrupoDespesaId,
  classificacao,
  lado,
  tipo,
  value,
  onChange,
}: {
  localObraId: string;
  subgrupoDespesaId: string;
  classificacao: string;
  lado: string;
  tipo: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const habilitado = Boolean(localObraId && subgrupoDespesaId && tipo);
  const ladoEfetivo = classificacao === 'transferencia_saldo' ? lado : '';
  const { data } = useQuery({
    queryKey: ['obra-ordem', localObraId, subgrupoDespesaId, classificacao, ladoEfetivo, tipo],
    queryFn: () =>
      fetchJSON<RespostaOrdem>(
        `/api/formulario/obra-ordens?local_obra_id=${localObraId}&subgrupo_despesa_id=${subgrupoDespesaId}&classificacao=${classificacao}&lado=${ladoEfetivo}&tipo=${tipo}`,
      ),
    enabled: habilitado,
  });
  // o sistema preenche o que sabe; nos demais casos limpa o que não vale mais
  useEffect(() => {
    if (!data) return;
    if (data.situacao === 'existe' || data.situacao === 'criar') {
      if (value !== data.ordem) onChange(data.ordem);
    } else if (data.situacao === 'ambigua') {
      if (value && !data.candidatas.some((c) => c.ordem === value && !c.bloqueada)) onChange('');
    } else if (data.situacao !== 'sem_estrutura' && value) {
      onChange('');
    } else if (data.situacao === 'sem_estrutura' && value === 'CRIAR') {
      onChange('');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  if (!habilitado) {
    return (
      <div className="flex h-9 items-center rounded-md border border-input bg-[var(--bg-input-disabled)] px-3 text-sm text-fg-muted">
        {!localObraId ? 'Escolha o local de obra' : !subgrupoDespesaId ? 'Escolha o subgrupo' : 'Escolha o tipo (FL ou Externa)'}
      </div>
    );
  }
  const travado = data?.situacao === 'existe' || data?.situacao === 'criar';
  const erro = data?.situacao === 'exige_existente' || data?.situacao === 'fora_da_familia' || data?.situacao === 'bloqueada';
  // F8b: ordem bloqueada aparece, cinza/vermelha e não selecionável, com o motivo
  const rotuloFat = (c: OrdemCandidata) =>
    c.tipo_faturamento === 'direto' ? 'Direto' : c.tipo_faturamento === 'sub_contratada' ? 'Sub-contratada' : c.colunas || c.tipo_faturamento;
  return (
    <>
      {data?.situacao === 'ambigua' ? (
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger className="h-9">
            <SelectValue placeholder="Escolha a ordem" />
          </SelectTrigger>
          <SelectContent>
            {data.candidatas.map((c) => (
              <SelectItem key={c.ordem} value={c.ordem} disabled={c.bloqueada} title={c.observacao}>
                {c.bloqueada ? `${c.ordem} - bloqueada` : `${rotuloFat(c)} (${c.ordem})`}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : data?.situacao === 'bloqueada' ? (
        <div className="grid gap-1">
          {data.candidatas.map((c) => (
            <div key={c.ordem} className="flex h-9 cursor-not-allowed items-center rounded-md border border-danger-border bg-[var(--bg-input-disabled)] px-3 text-sm text-danger-text" title={c.observacao || 'Bloqueada para lançamento no SAP'}>
              {c.ordem} - bloqueada
            </div>
          ))}
        </div>
      ) : data?.situacao === 'sem_estrutura' ? (
        <Input className="h-9" inputMode="numeric" placeholder="Ordem (4 a 20 dígitos)" value={value} onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 20))} />
      ) : (
        <div
          className={`flex h-9 items-center rounded-md border px-3 text-sm tabular-nums ${travado ? 'border-input bg-[var(--bg-input-disabled)] text-foreground' : 'border-danger-border text-fg-muted'}`}
          title={travado ? 'Definida pela estrutura da obra' : undefined}
        >
          {data ? (travado ? data.ordem : '—') : 'Consultando…'}
        </div>
      )}
      {data && <p className={`text-xs ${erro ? 'text-danger-text' : 'text-fg-helper'}`}>{data.dica}</p>}
      {data?.candidatas.some((c) => c.bloqueada) && data.situacao !== 'bloqueada' && (
        <p className="text-xs text-fg-helper">Ordens bloqueadas para lançamento no SAP aparecem na lista, mas não podem ser escolhidas.</p>
      )}
    </>
  );
}

