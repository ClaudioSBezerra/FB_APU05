import { useEffect, useMemo, useState } from 'react';

// Saldo orçamentário (API de Trava orçamentária do SAP) para as linhas do formulário. É AVISO: nunca impede o envio.
// Se o SAP não responde, a linha volta "nao_identificado" e a tela mostra "Saldo Não Identificado".
export interface LinhaSaldoEntrada {
  lado: 'origem' | 'destino';
  centro_custo_id: string;
  conta_id: string;
  mes: string; // AAAA-MM-01
  valor: number;
}

export interface SaldoLinha {
  indice: number;
  lado: 'origem' | 'destino';
  cc: string;
  conta: string;
  mes: string;
  status: 'ok' | 'sem_lancamentos' | 'nao_identificado';
  motivo?: string;
  planejado?: number;
  realizado?: number;
  comprometido?: number;
  disposto?: number;
  disponivel?: number;
  disponivel_acumulado?: number;
  retirada_acumulada?: number;
  excede: boolean;
  falta?: number;
}

type Estado = { carregando: boolean; linhas: SaldoLinha[]; ativo: boolean };

export function useSaldoOrcamento(linhas: LinhaSaldoEntrada[], habilitado: boolean): Estado {
  // só linhas completas entram na consulta; a chave do efeito muda só quando algo relevante muda
  const completas = useMemo(
    () => linhas.map((l, i) => ({ ...l, i })).filter((l) => l.centro_custo_id && l.conta_id && /^\d{4}-\d{2}/.test(l.mes)),
    [linhas],
  );
  const chave = JSON.stringify(completas.map((l) => [l.lado, l.centro_custo_id, l.conta_id, l.mes, l.lado === 'origem' ? l.valor : 0]));
  const [estado, setEstado] = useState<Estado>({ carregando: false, linhas: [], ativo: true });

  useEffect(() => {
    if (!habilitado || completas.length === 0) {
      setEstado((e) => ({ ...e, carregando: false, linhas: [] }));
      return;
    }
    const ctrl = new AbortController();
    setEstado((e) => ({ ...e, carregando: true }));
    // espera o usuário parar de digitar (valor/mês) antes de consultar o SAP
    const t = setTimeout(async () => {
      try {
        const resp = await fetch('/api/solicitacoes/saldo', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ lancamentos: completas.map(({ i: _i, ...l }) => l) }),
          signal: ctrl.signal,
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const dados = (await resp.json()) as { ativo?: boolean; linhas: SaldoLinha[] };
        // volta para o índice original da linha do formulário
        setEstado({ carregando: false, ativo: dados.ativo !== false, linhas: dados.linhas.map((s, k) => ({ ...s, indice: completas[k].i })) });
      } catch (e) {
        if ((e as Error).name === 'AbortError') return;
        setEstado({
          carregando: false,
          ativo: true,
          linhas: completas.map((l) => ({
            indice: l.i, lado: l.lado, cc: '', conta: '', mes: l.mes.slice(0, 7), status: 'nao_identificado', motivo: 'servidor indisponível', excede: false,
          })),
        });
      }
    }, 600);
    return () => {
      clearTimeout(t);
      ctrl.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chave, habilitado]);

  return estado;
}
