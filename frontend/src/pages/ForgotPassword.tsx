import { useState } from 'react';
import { Link } from 'react-router-dom';

// Tela de "esqueci minha senha" — só se aplica a contas de login local
// (e-mail+senha); contas SSO não têm senha nossa pra redefinir. Mensagem de
// sucesso é sempre a mesma, exista o e-mail ou não (ForgotPasswordHandler,
// backend/handlers/auth.go) — evita enumeração de contas.
const ForgotPassword = () => {
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setErrorMsg(null);
    setSubmitting(true);
    try {
      const resp = await fetch('/api/auth/forgot-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email }),
      });
      const data = (await resp.json()) as { message?: string; error?: string };
      if (!resp.ok) {
        setErrorMsg(data.error || 'Falha ao solicitar redefinição de senha.');
        return;
      }
      setMessage(data.message || 'Se o e-mail estiver cadastrado, você receberá um link em instantes.');
    } catch {
      setErrorMsg('Falha de conexão com o servidor. Tente novamente.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-6 shadow-sm">
        <h1 className="text-base font-semibold text-card-foreground">Esqueci minha senha</h1>
        <p className="mt-1 text-xs text-muted-foreground">
          Informe o e-mail da sua conta local. Enviaremos um link de redefinição se ele existir.
        </p>

        {message && (
          <div className="mt-4 rounded-md border border-border bg-muted px-3 py-2 text-xs text-muted-foreground">
            {message}
          </div>
        )}
        {errorMsg && (
          <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {errorMsg}
          </div>
        )}

        {!message && (
          <form onSubmit={handleSubmit} className="mt-5 flex flex-col gap-3">
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
            <button
              type="submit"
              disabled={submitting}
              className="mt-1 w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-60"
            >
              {submitting ? 'Enviando...' : 'Enviar link de redefinição'}
            </button>
          </form>
        )}

        <Link to="/login" className="mt-4 block text-center text-xs text-primary underline hover:no-underline">
          Voltar para o login
        </Link>
      </div>
    </div>
  );
};

export default ForgotPassword;
