import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';

export interface Permissoes {
  controle_ti: boolean;
}

// Permissões lidas do servidor (não do token): ligar/desligar um interruptor em
// Gestão de Usuários vale no próximo carregamento, sem sair e entrar de novo.
export function usePermissoes() {
  const { isAuthenticated } = useAuth();
  return useQuery<Permissoes>({
    queryKey: ['permissoes'],
    queryFn: async () => {
      const resp = await fetch('/api/me/permissoes');
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      return resp.json();
    },
    enabled: isAuthenticated,
    staleTime: 30_000,
  });
}
