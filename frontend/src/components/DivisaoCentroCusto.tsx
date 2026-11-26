import { useMemo, useState } from 'react';
import { Label } from '@/components/ui/label';
import { BuscaCombobox, type OpcaoBusca } from '@/components/BuscaCombobox';

export interface CentroCustoOpcao extends OpcaoBusca {
  divisao_id: string;
}

// B3 (26/11): divisão e centro de custo em campos separados. O código do CC no SAP tem 8 dígitos: os 4 da esquerda
// são a divisão (filial) e os 4 da direita, o centro de custo. Os seletores não mostram os 8 dígitos juntos; a lista
// de CC respeita a divisão escolhida. O valor continua sendo o id do CC (o que o servidor espera).
const parteCC = (codigo: string) => (/^\d{8}$/.test(codigo) ? codigo.slice(4) : codigo);

export function DivisaoCentroCusto({
  value,
  onChange,
  centros,
  divisoes,
  rotuloDivisao = 'Filial / Divisão',
  rotuloCC = 'Centro de custo',
  limparRotulo,
  divisaoPermitida,
}: {
  value: string;
  onChange: (id: string) => void;
  centros: CentroCustoOpcao[];
  divisoes: OpcaoBusca[];
  rotuloDivisao?: string;
  rotuloCC?: string;
  // destino da Transferência: "Mesmo centro de custo da origem" (value '')
  limparRotulo?: string;
  // F4 (26/11): só as divisões que passam no filtro (ex.: Tipo FL no destino da Transferência)
  divisaoPermitida?: (divisaoId: string) => boolean;
}) {
  const ccAtual = centros.find((c) => c.id === value);
  const [divisaoEscolhida, setDivisaoEscolhida] = useState('');
  const divisaoId = ccAtual?.divisao_id ?? divisaoEscolhida;

  // só as divisões que têm algum CC disponível para este campo
  const opcoesDivisao = useMemo(() => {
    const comCC = new Set(centros.map((c) => c.divisao_id).filter((d) => !divisaoPermitida || divisaoPermitida(d)));
    const porId = new Map(divisoes.map((d) => [d.id, d]));
    return [...comCC]
      .map((id) => porId.get(id) ?? { id, codigo: '', nome: 'Divisão sem cadastro' })
      .sort((a, b) => a.codigo.localeCompare(b.codigo));
  }, [centros, divisoes, divisaoPermitida]);

  const opcoesCC = useMemo(
    () =>
      centros
        .filter((c) => (!divisaoId || c.divisao_id === divisaoId) && (!divisaoPermitida || divisaoPermitida(c.divisao_id)))
        .map((c) => ({ id: c.id, codigo: parteCC(c.codigo), nome: c.nome })),
    [centros, divisaoId, divisaoPermitida],
  );

  return (
    <div className="grid min-w-0 gap-3 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
      <div className="grid min-w-0 gap-1.5">
        <Label>{rotuloDivisao}</Label>
        <BuscaCombobox
          ariaLabel={rotuloDivisao}
          value={divisaoId}
          onChange={(id) => {
            setDivisaoEscolhida(id);
            if (ccAtual && ccAtual.divisao_id !== id) onChange('');
          }}
          opcoes={opcoesDivisao}
          placeholder={limparRotulo && !value ? 'Mesma da origem' : 'Escolha a divisão'}
          vazio="Nenhuma divisão encontrada."
        />
      </div>
      <div className="grid min-w-0 gap-1.5">
        <Label>{rotuloCC}</Label>
        <BuscaCombobox
          ariaLabel={rotuloCC}
          value={value}
          onChange={onChange}
          opcoes={opcoesCC}
          placeholder={limparRotulo ?? (divisaoId ? 'Escolha o centro de custo' : 'Escolha a divisão primeiro ou busque aqui')}
          vazio="Nenhum centro de custo encontrado."
          limparRotulo={limparRotulo}
        />
      </div>
    </div>
  );
}
