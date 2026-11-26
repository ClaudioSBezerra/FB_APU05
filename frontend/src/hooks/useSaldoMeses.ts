import { useQuery } from '@tanstack/react-query';

// Saldo dos 12 meses de um CC + conta + ano numa só consulta (GET /api/solicitacoes/saldo-meses, item C de 26/11).
// "pode_remanejar" = menor entre o disponível do mês e o acumulado até o mês, nunca abaixo de zero — é o mesmo valor
// que o servidor usa para recusar a abertura.
export interface MesSaldo {
  mes: string; // AAAA-MM
  disponivel: number;
  disponivel_acumulado: number;
  pode_remanejar: number;
}

export interface SaldoMeses {
  ativo: boolean;
  status?: 'ok' | 'sem_lancamentos' | 'nao_identificado';
  motivo?: string;
  meses: MesSaldo[];
}

export function useSaldoMeses(centroCustoId: string, contaId: string, ano: string) {
  const habilitado = Boolean(centroCustoId && contaId && /^\d{4}$/.test(ano));
  return useQuery({
    queryKey: ['saldo-meses', centroCustoId, contaId, ano],
    enabled: habilitado,
    staleTime: 15_000,
    retry: false,
    queryFn: async (): Promise<SaldoMeses> => {
      const p = new URLSearchParams({ centro_custo_id: centroCustoId, conta_id: contaId, ano });
      const resp = await fetch(`/api/solicitacoes/saldo-meses?${p}`);
      const dados = await resp.json().catch(() => ({}));
      if (!resp.ok) {
        return { ativo: true, status: 'nao_identificado', motivo: dados?.error ?? `HTTP ${resp.status}`, meses: [] };
      }
      return dados as SaldoMeses;
    },
  });
}
