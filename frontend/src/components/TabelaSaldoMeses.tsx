import type { SaldoMeses } from '@/hooks/useSaldoMeses';

const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });
const MESES = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];
export const rotuloMes = (aaaamm: string) => `${MESES[Number(aaaamm.slice(5, 7)) - 1] ?? aaaamm}/${aaaamm.slice(0, 4)}`;

// Item C (26/11): tabela da ORIGEM da Transferência, de janeiro até o mês escolhido (com dezembro, os 12 meses).
// Mostra todos os meses do intervalo, sem esconder os sem saldo: os que não comportam o valor pedido ficam esmaecidos,
// os que comportam ficam destacados, e o mês escolhido aparece marcado com quanto dele pode sair. Clicar num mês troca
// o mês da linha de origem que está sendo editada.
export function TabelaSaldoMeses({
  saldo,
  carregando,
  retiradas,
  mesAtivo,
  valorAtivo,
  mesesOcupados,
  onEscolherMes,
}: {
  saldo?: SaldoMeses;
  carregando: boolean;
  // AAAA-MM -> valor retirado naquele mês (todas as linhas de origem)
  retiradas: Record<string, number>;
  mesAtivo: string; // AAAA-MM da linha em edição ('' = nenhuma)
  valorAtivo: number;
  mesesOcupados: Set<string>; // meses já usados por OUTRAS linhas de origem
  onEscolherMes: (mes: string) => void;
}) {
  if (carregando) return <p className="text-xs text-fg-helper">Consultando o saldo no SAP…</p>;
  if (!saldo || !saldo.ativo) return null;
  if (saldo.status === 'nao_identificado') {
    return (
      <p className="text-xs text-fg-helper" title={saldo.motivo}>
        Saldo Não Identificado{saldo.motivo ? ` (${saldo.motivo})` : ''}. O envio não é bloqueado pelo saldo; quem atende confere no SAP.
      </p>
    );
  }
  const escolhidos = Object.keys(retiradas).filter((m) => retiradas[m] > 0);
  const ultimo = [mesAtivo, ...escolhidos].filter(Boolean).sort().pop();
  const ate = ultimo ? Number(ultimo.slice(5, 7)) : 12;
  const linhas = saldo.meses.slice(0, ate);

  return (
    <div className="grid gap-1.5">
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full text-xs tabular-nums">
          <caption className="sr-only">Saldo da origem de janeiro até o mês escolhido</caption>
          <thead className="bg-muted/60 text-fg-secondary">
            <tr>
              <th className="px-2 py-1.5 text-left font-medium">Mês</th>
              <th className="px-2 py-1.5 text-right font-medium">Disponível no mês</th>
              <th className="px-2 py-1.5 text-right font-medium">Acumulado até o mês</th>
              <th className="px-2 py-1.5 text-right font-medium">Reservado em chamados abertos</th>
              <th className="px-2 py-1.5 text-right font-medium">Pode remanejar</th>
            </tr>
          </thead>
          <tbody>
            {linhas.map((m) => {
              const ativo = m.mes === mesAtivo;
              const ocupado = mesesOcupados.has(m.mes);
              const comporta = m.pode_remanejar > 0 && m.pode_remanejar + 0.005 >= (ativo ? valorAtivo : Math.max(valorAtivo, 0.01));
              const retirado = retiradas[m.mes] ?? 0;
              const estoura = retirado > m.pode_remanejar + 0.005;
              return (
                <tr
                  key={m.mes}
                  className={[
                    'border-t border-border',
                    ativo ? 'bg-[var(--blue-l90,#e8f0fe)] font-semibold' : '',
                    !ativo && !comporta ? 'opacity-50' : '',
                    ocupado ? '' : 'cursor-pointer hover:bg-muted/60',
                  ].join(' ')}
                  onClick={() => !ocupado && onEscolherMes(m.mes)}
                  title={ocupado ? 'Mês já usado em outra linha de origem' : 'Usar este mês na linha de origem em edição'}
                >
                  <td className="px-2 py-1">
                    {rotuloMes(m.mes)}
                    {ativo && <span className="ml-1 text-[10px] font-normal text-fg-helper">(escolhido)</span>}
                    {!ativo && ocupado && <span className="ml-1 text-[10px] font-normal text-fg-helper">(em outra linha)</span>}
                  </td>
                  <td className="px-2 py-1 text-right">{moeda.format(m.disponivel)}</td>
                  <td className="px-2 py-1 text-right">{moeda.format(m.disponivel_acumulado)}</td>
                  <td className="px-2 py-1 text-right" title={(m.chamados_reserva ?? []).join(', ')}>
                    {m.reservado ? (
                      <>
                        {moeda.format(m.reservado)}
                        <span className="block text-[10px] text-fg-helper">{(m.chamados_reserva ?? []).join(', ')}</span>
                      </>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className={`px-2 py-1 text-right ${estoura ? 'text-danger-text' : comporta ? 'font-semibold text-foreground' : ''}`}>
                    {moeda.format(m.pode_remanejar)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-fg-helper">
        Disponível e acumulado já descontam o que está reservado em chamados abertos. Pode remanejar = o menor entre os dois; nenhum mês pode ficar negativo. Clique num mês para usá-lo na linha em edição.
        {saldo.status === 'sem_lancamentos' && ' Sem orçamento nem lançamentos no SAP para esta conta.'}
      </p>
    </div>
  );
}
