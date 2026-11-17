import { useQuery } from '@tanstack/react-query';
import { useAuth } from '@/contexts/AuthContext';

export interface PromocaoStatus {
  pode_solicitar: boolean;
  perfil: string;
  ultimo_pedido?: string;
  proximo_pedido?: string;
}

// Situação do "Solicitar promoção" (GET /api/me/promocao). Só consulta quem é solicitante: os
// demais perfis não têm o que solicitar.
export function usePromocao() {
  const { isAuthenticated, user } = useAuth();
  return useQuery<PromocaoStatus>({
    queryKey: ['promocao'],
    queryFn: async () => {
      const resp = await fetch('/api/me/promocao');
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      return resp.json();
    },
    enabled: isAuthenticated && user?.perfil === 'solicitante',
    staleTime: 30_000,
  });
}
