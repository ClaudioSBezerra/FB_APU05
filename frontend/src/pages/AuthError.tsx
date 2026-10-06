import { useSearchParams, Link } from 'react-router-dom';

// Tela de erro do fluxo de login via Keycloak.
export default function AuthError() {
  const [params] = useSearchParams();
  const message = params.get('message') || 'Não foi possível concluir o login via Keycloak.';

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-6 shadow-sm text-center">
        <h1 className="text-base font-semibold text-card-foreground">Erro no login via SSO</h1>
        <p className="mt-2 text-sm text-muted-foreground">{message}</p>
        <Link
          to="/login?password"
          className="mt-5 inline-block rounded-md border border-border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Voltar para o login
        </Link>
      </div>
    </div>
  );
}
