/**
 * handleCallback — troca o `code` do callback OIDC por uma sessão própria do
 * FB_APU05.
 *
 * O token do Keycloak só vive o tempo desta chamada: depois de validado pelo
 * backend (POST /api/auth/sso/keycloak, protegido por iam.IAMAuthMiddleware),
 * ele nunca é persistido nem exposto a nenhuma outra camada do frontend — a
 * resposta já vem no formato que AuthContext.login() espera.
 */

import { SESSION_KEY_VERIFIER, SESSION_KEY_STATE, fetchIAMConfig } from './buildLoginUrl';

export class CallbackError extends Error {
  constructor(message: string, public readonly code: string) {
    super(message);
  }
}

// Timeout de rede para as duas trocas desta tela — sem isso, uma requisição
// pendurada (Keycloak ou nosso backend fora do ar) prende o usuário no
// spinner "Concluindo login via Keycloak..." indefinidamente.
const NETWORK_TIMEOUT_MS = 10_000;

/** fetch com timeout — converte o DOMException de timeout/abort num
 * CallbackError com mensagem clara, em vez de deixar o erro cru subir. */
async function fetchWithTimeout(input: RequestInfo | URL, init: RequestInit, timeoutErrorCode: string): Promise<Response> {
  try {
    return await fetch(input, { ...init, signal: AbortSignal.timeout(NETWORK_TIMEOUT_MS) });
  } catch (err) {
    if (err instanceof DOMException && (err.name === 'TimeoutError' || err.name === 'AbortError')) {
      throw new CallbackError('Tempo esgotado ao conectar — tente novamente.', timeoutErrorCode);
    }
    throw err;
  }
}

// Payload devolvido por POST /api/auth/sso/keycloak em caso de sucesso —
// mesmo formato que AuthContext.login() espera (ver backend/handlers/auth.go,
// struct AuthResponse).
export interface SSOLoginResponse {
  token: string;
  user: {
    id: string;
    email: string;
    nome: string;
    perfil: 'solicitante' | 'administrador';
  };
}

export async function handleCallback(searchParams: URLSearchParams): Promise<SSOLoginResponse> {
  const code = searchParams.get('code');
  const state = searchParams.get('state');
  const errorParam = searchParams.get('error');

  if (errorParam) {
    throw new CallbackError(`Keycloak retornou erro: ${errorParam}`, 'keycloak_error');
  }
  if (!code || !state) {
    throw new CallbackError('Callback sem code/state.', 'missing_params');
  }

  const savedState = sessionStorage.getItem(SESSION_KEY_STATE);
  const verifier = sessionStorage.getItem(SESSION_KEY_VERIFIER);
  sessionStorage.removeItem(SESSION_KEY_STATE);
  sessionStorage.removeItem(SESSION_KEY_VERIFIER);

  if (!verifier || !savedState || savedState !== state) {
    throw new CallbackError('State inválido (possível CSRF) ou sessão de login expirada.', 'invalid_state');
  }

  const config = await fetchIAMConfig();
  if (!config.enabled || !config.base_url || !config.client_id || !config.redirect_uri) {
    throw new CallbackError('SSO Keycloak não configurado neste servidor.', 'iam_not_configured');
  }

  // 1. Troca code por access_token do Keycloak (fluxo OIDC padrão, direto no realm —
  // nunca passa pelo nosso backend).
  const tokenResp = await fetchWithTimeout(
    `${config.base_url}/protocol/openid-connect/token`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: config.client_id,
        redirect_uri: config.redirect_uri,
        code,
        code_verifier: verifier,
      }),
    },
    'token_exchange_timeout',
  );
  if (!tokenResp.ok) {
    throw new CallbackError('Falha ao trocar código por token no Keycloak.', 'token_exchange_failed');
  }
  const tokenData = await tokenResp.json();
  const accessToken = tokenData.access_token as string | undefined;
  if (!accessToken) {
    throw new CallbackError('Resposta do Keycloak sem access_token.', 'token_exchange_failed');
  }

  // 2. Troca o access_token do Keycloak pela sessão própria do FB_APU05 — o token
  // do Keycloak não é usado novamente depois deste ponto, nem fica guardado em
  // sessionStorage/localStorage.
  const sessionResp = await fetchWithTimeout(
    '/api/auth/sso/keycloak',
    {
      method: 'POST',
      headers: { Authorization: `Bearer ${accessToken}` },
    },
    'sso_exchange_timeout',
  );
  if (!sessionResp.ok) {
    const text = await sessionResp.text();
    let message = text;
    try {
      const parsed = JSON.parse(text);
      if (typeof parsed?.error === 'string') message = parsed.error;
    } catch {
      // corpo não era JSON — usa o texto cru mesmo
    }
    throw new CallbackError(message || 'Falha ao concluir login via Keycloak.', 'sso_exchange_failed');
  }
  return sessionResp.json();
}
