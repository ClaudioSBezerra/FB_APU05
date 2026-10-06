import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/contexts/AuthContext';
import { buildLoginUrl, fetchIAMConfig } from '@/lib/keycloak/buildLoginUrl';

// Tela de login — único fluxo é SSO Keycloak (AD-6/Epic 1: "sem cadastro de
// usuário separado"). Sem shadcn/ui ou sonner aqui: o scaffold (Story 1.1)
// ainda não inclui esses componentes/dependência — usa só Tailwind (já
// configurado) e elementos nativos, para não introduzir dependências fora do
// escopo desta story.
const Login = () => {
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [ssoEnabled, setSsoEnabled] = useState(false);
  const navigate = useNavigate();
  const { isAuthenticated } = useAuth();

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
          <div className="mt-5 rounded-md border border-border bg-muted px-3 py-2 text-xs text-muted-foreground">
            SSO Keycloak não está configurado neste servidor. Contate o administrador.
          </div>
        )}
      </div>
    </div>
  );
};

export default Login;
