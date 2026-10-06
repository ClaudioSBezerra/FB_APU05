import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/contexts/AuthContext';
import { handleCallback, CallbackError } from '@/lib/keycloak/handleCallback';

// Tela intermediária do fluxo OIDC/PKCE — troca o code do Keycloak por uma
// sessão própria do FB_APU05 e navega pra dentro do app.
export default function AuthCallback() {
  const navigate = useNavigate();
  const { login } = useAuth();
  // Guarda contra o duplo-invoke de efeitos do React.StrictMode em dev
  // (mount→cleanup→mount): sem isso, a 1ª invocação (descartada) já teria
  // consumido o `code` de uso único do Keycloak antes da 2ª (a que vale) rodar.
  const handled = useRef(false);

  useEffect(() => {
    if (handled.current) return;
    handled.current = true;

    const run = async () => {
      try {
        const data = await handleCallback(new URLSearchParams(window.location.search));
        login(data, { viaSSO: true });
        navigate('/', { replace: true });
      } catch (err) {
        const message = err instanceof CallbackError ? err.message : 'Erro ao concluir login via Keycloak.';
        navigate(`/auth/error?message=${encodeURIComponent(message)}`, { replace: true });
      }
    };
    run();
  }, [login, navigate]);

  return (
    <div className="min-h-screen flex flex-col items-center justify-center gap-3 bg-background">
      <div className="h-10 w-10 rounded-full border-2 border-muted border-t-foreground animate-spin" />
      <p className="text-sm text-muted-foreground">Concluindo login via Keycloak...</p>
    </div>
  );
}
