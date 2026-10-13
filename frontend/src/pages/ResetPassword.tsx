import { useState } from 'react';
import { useNavigate } from 'react-router-dom';

// Tela acessada pelo link do e-mail de redefinição (?token=...), enviado por
// ForgotPasswordHandler. Token é lido da URL só na hora de montar o corpo da
// requisição — nunca guardado em estado além disso.
const ResetPassword = () => {
  const navigate = useNavigate();
  const token = new URLSearchParams(window.location.search).get('token') || '';

  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setErrorMsg(null);

    if (!token) {
      setErrorMsg('Link inválido — token ausente. Solicite uma nova redefinição.');
      return;
    }

    setSubmitting(true);
    try {
      const resp = await fetch('/api/auth/reset-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, password, confirm_password: confirmPassword }),
      });
      const data = (await resp.json()) as { message?: string; error?: string };
      if (!resp.ok) {
        setErrorMsg(data.error || 'Falha ao redefinir a senha.');
        return;
      }
      navigate('/login', { replace: true });
    } catch {
      setErrorMsg('Falha de conexão com o servidor. Tente novamente.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-6 shadow-sm">
        <h1 className="text-base font-semibold text-card-foreground">Redefinir senha</h1>
        <p className="mt-1 text-xs text-muted-foreground">Escolha uma nova senha para sua conta.</p>

        {errorMsg && (
          <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {errorMsg}
          </div>
        )}

        <form onSubmit={handleSubmit} className="mt-5 flex flex-col gap-3">
          <div>
            <label htmlFor="password" className="text-xs font-medium text-card-foreground">
              Nova senha
            </label>
            <input
              id="password"
              type="password"
              required
              minLength={8}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label htmlFor="confirmPassword" className="text-xs font-medium text-card-foreground">
              Confirme a nova senha
            </label>
            <input
              id="confirmPassword"
              type="password"
              required
              minLength={8}
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="mt-1 w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-60"
          >
            {submitting ? 'Salvando...' : 'Redefinir senha'}
          </button>
        </form>
      </div>
    </div>
  );
};

export default ResetPassword;
