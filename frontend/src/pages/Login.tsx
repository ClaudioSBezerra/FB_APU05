import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/contexts/AuthContext';
import { buildLoginUrl, fetchIAMConfig } from '@/lib/keycloak/buildLoginUrl';

// Payload de POST /api/auth/login em caso de sucesso — mesmo formato que
// AuthContext.login() espera (ver backend/handlers/auth.go, AuthResponse),
// igual SSOLoginResponse em handleCallback.ts.
interface LocalLoginResponse {
  token: string;
  user: {
    id: string;
    email: string;
    nome: string;
    perfil: 'solicitante' | 'administrador';
  };
}

// Tela de login — único fluxo é SSO Keycloak (AD-6/Epic 1: "sem cadastro de
// usuário separado"). Sem shadcn/ui ou sonner aqui: o scaffold (Story 1.1)
// ainda não inclui esses componentes/dependência — usa só Tailwind (já
// configurado) e elementos nativos, para não introduzir dependências fora do
// escopo desta story.
const Login = () => {
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [ssoEnabled, setSsoEnabled] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();
  const { login, isAuthenticated } = useAuth();

  // `?password` força a tela manual mesmo com SSO habilitado — útil pra
  // depurar o fluxo sem cair direto no redirect automático.
  const hasPasswordFallback = new URLSearchParams(window.location.search).has('password');
  const [autoRedirecting, setAutoRedirecting] = useState(!hasPasswordFallback);

  // A LEITURA é pura (lazy initializer do useState — React.StrictMode invoca
  // isto 2x em dev, mas ler sem mutar dá o mesmo resultado nas duas vezes). A
  // LIMPEZA fica separada, num useEffect: mutar sessionStorage ali não conta
  // como setState (não dispara o lint de "setState dentro de efeito") e
  // sobrevive ao duplo-invoke de efeitos do StrictMode (remover uma chave já
  // removida é no-op) — só existe pra a flag não reaparecer num reload futuro.
  const [sessionExpired] = useState(() => sessionStorage.getItem('session_expired') === '1');
  useEffect(() => {
    if (sessionExpired) sessionStorage.removeItem('session_expired');
  }, [sessionExpired]);

  useEffect(() => {
    if (isAuthenticated) {
      navigate('/', { replace: true });
    }
  }, [isAuthenticated, navigate]);

  useEffect(() => {
    let mounted = true;
    let settled = false;

    const timeoutId = window.setTimeout(() => {
      if (!settled && mounted) {
        settled = true;
        setAutoRedirecting(false);
      }
    }, 5000);

    fetchIAMConfig().then(async (config) => {
      if (!mounted) return;
      setSsoEnabled(config.enabled);

      // O timeout já pode ter revelado o formulário (settled=true) enquanto
      // esperávamos a config chegar — não força mais um redirect por cima.
      if (settled) return;

      if (hasPasswordFallback || !config.enabled) {
        settled = true;
        setAutoRedirecting(false);
        return;
      }

      try {
        const url = await buildLoginUrl();
        if (settled) return;
        settled = true;
        if (mounted) window.location.href = url;
      } catch (error) {
        console.error('[Login] Falha ao montar URL do Keycloak:', error);
        settled = true;
        if (mounted) setAutoRedirecting(false);
      }
    });

    return () => {
      mounted = false;
      window.clearTimeout(timeoutId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Login local (e-mail+senha) — só aparece quando SSO não está configurado
  // (ssoEnabled=false). Contas locais são criadas por um administrador via
  // POST /api/admin/usuarios; não existe autocadastro aqui.
  const handleLocalLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setErrorMsg(null);
    setSubmitting(true);
    try {
      const resp = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      const data = (await resp.json()) as LocalLoginResponse | { error?: string };
      if (!resp.ok) {
        setErrorMsg(('error' in data && data.error) || 'E-mail ou senha inválidos');
        return;
      }
      login(data as LocalLoginResponse);
      navigate('/', { replace: true });
    } catch {
      setErrorMsg('Falha de conexão com o servidor. Tente novamente.');
    } finally {
      setSubmitting(false);
    }
  };

  const handleSSOLogin = async () => {
    try {
      window.location.href = await buildLoginUrl();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'SSO não configurado neste servidor.';
      setErrorMsg(message);
    }
  };

  if (autoRedirecting) {
    return (
      <div
        className="min-h-screen flex flex-col items-center justify-center gap-3 bg-background"
        role="status"
        aria-live="polite"
      >
        <div className="h-8 w-8 rounded-full border-2 border-muted border-t-foreground animate-spin" />
        <p className="text-sm text-muted-foreground">Redirecionando para login corporativo...</p>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-6 shadow-sm">
        <h1 className="text-base font-semibold text-card-foreground">FB_APU05 — Módulo Controladoria</h1>
        <p className="mt-1 text-xs text-muted-foreground">Entre com sua conta corporativa Ferreira Costa.</p>

        {sessionExpired && (
          <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            Sua sessão expirou. Faça login novamente para continuar.
          </div>
        )}
        {errorMsg && (
          <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {errorMsg}
          </div>
        )}

        {ssoEnabled ? (
          <button
            type="button"
            onClick={handleSSOLogin}
            className="mt-5 w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90"
          >
            Entrar com SSO Ferreira Costa
          </button>
        ) : (
          <form onSubmit={handleLocalLogin} className="mt-5 flex flex-col gap-3">
            <div>
              <label htmlFor="email" className="text-xs font-medium text-card-foreground">
                E-mail
              </label>
              <input
                id="email"
                type="email"
                required
                autoComplete="username"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label htmlFor="password" className="text-xs font-medium text-card-foreground">
                Senha
              </label>
              <input
                id="password"
                type="password"
                required
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              />
            </div>
            <button
              type="submit"
              disabled={submitting}
              className="mt-1 w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-60"
            >
              {submitting ? 'Entrando...' : 'Entrar'}
            </button>
          </form>
        )}
      </div>
    </div>
  );
};

export default Login;
