import React, { createContext, useContext, useState, useEffect, useRef } from 'react';
import { SESSION_KEY_AUTH_VIA_SSO } from '@/lib/keycloak/buildLoginUrl';

// Diferente do AuthContext de referência no FB_APU02 (multi-empresa): o
// FB_APU05 é single-company (AD-7) — sem environment/group/company/
// switchCompany. Perfil (`solicitante`/`administrador`) vem sempre do nosso
// próprio banco (tabela `usuarios`), nunca de claim do Keycloak.
interface User {
  id: string;
  email: string;
  nome: string;
  perfil: 'solicitante' | 'administrador';
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  login: (data: { token: string; user: User }, opts?: { viaSSO?: boolean }) => void;
  logout: () => void;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

// Restaura sessão de sessionStorage de forma síncrona (lazy initializer) —
// sem revalidar contra o servidor, já que não há endpoint /api/auth/me nesta
// story. Lido uma única vez, na primeira renderização do provider.
function readStoredUser(): User | null {
  const storedUser = sessionStorage.getItem('user');
  if (!storedUser) return null;
  try {
    return JSON.parse(storedUser) as User;
  } catch {
    sessionStorage.removeItem('user');
    sessionStorage.removeItem('token');
    return null;
  }
}

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  const [user, setUser] = useState<User | null>(() => readStoredUser());
  const [token, setToken] = useState<string | null>(() => sessionStorage.getItem('token'));

  // Ref para o interceptor de fetch (sem stale closure).
  const tokenRef = useRef<string | null>(null);
  // Guarda contra dupla-invocação de logout() (duplo clique, 2 componentes de navegação).
  const loggingOutRef = useRef(false);

  useEffect(() => {
    tokenRef.current = token;
  }, [token]);

  // Interceptor global de fetch: injeta Authorization só nas chamadas à nossa
  // própria API (/api/*) — nunca em chamadas a outra origem, como a troca de
  // code→token que handleCallback.ts faz direto contra o Keycloak. Sem esse
  // escopo, um token nosso já em memória vazaria como Bearer para o IdP numa
  // requisição que não é dirigida a nós.
  // Em 401 numa chamada autenticada, encerra a sessão local e manda pro login
  // — esta story não implementa renovação via refresh cookie (/api/auth/refresh
  // não existe ainda; ver Implementation Notes da spec), então uma sessão
  // expira de fato ao fim dos 30min do access token, exigindo novo login.
  useEffect(() => {
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const url = typeof input === 'string' ? input : (input as Request).url ?? '';
      const isApiCall = url.includes('/api/');
      const isAuthCall = url.includes('/api/auth/');

      const headers = new Headers(init.headers || {});
      if (isApiCall && !headers.has('Authorization') && tokenRef.current) {
        headers.set('Authorization', `Bearer ${tokenRef.current}`);
      }
      const response = await originalFetch(input, { ...init, headers });

      if (response.status === 401 && isApiCall && !isAuthCall && tokenRef.current && !loggingOutRef.current) {
        console.error('[Auth] Token expirado ou inválido — sessão encerrada.');
        sessionStorage.clear();
        sessionStorage.setItem('session_expired', '1');
        window.location.href = '/login';
      }

      return response;
    };
    return () => {
      window.fetch = originalFetch;
    };
  }, []);

  const login = (data: { token: string; user: User }, opts?: { viaSSO?: boolean }) => {
    setToken(data.token);
    setUser(data.user);

    sessionStorage.setItem('token', data.token);
    sessionStorage.setItem('user', JSON.stringify(data.user));
    // Sempre reescrita — nunca deixa resíduo de uma sessão anterior.
    sessionStorage.setItem(SESSION_KEY_AUTH_VIA_SSO, opts?.viaSSO ? '1' : '0');
  };

  // logout cobre só limpeza local (cookie de refresh + sessionStorage) — o
  // end_session completo no Keycloak (SLO) fica fora desta story (ver spec
  // Design Notes): não é testável neste sandbox sem um client Keycloak real
  // provisionado. Isso significa que, se a sessão do Keycloak no navegador
  // ainda estiver ativa, o auto-redirect da tela de login pode reautenticar
  // silenciosamente o usuário — limitação conhecida, documentada no relato
  // desta story.
  const logout = () => {
    if (loggingOutRef.current) return;
    loggingOutRef.current = true;

    // POST para o backend invalidar (blacklist) o access token atual e apagar
    // o refresh token do store + limpar o cookie HttpOnly — fire-and-forget,
    // a limpeza local abaixo não espera a rede.
    fetch('/api/auth/logout', {
      method: 'POST',
      headers: tokenRef.current ? { Authorization: `Bearer ${tokenRef.current}` } : {},
    }).catch(() => {});

    sessionStorage.clear();
    setUser(null);
    setToken(null);
    window.location.href = '/login';
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        login,
        logout,
        isAuthenticated: !!user,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};
