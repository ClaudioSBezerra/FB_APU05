/**
 * buildLoginUrl — monta a URL de autorização OAuth2/OIDC do Keycloak com PKCE.
 *
 * Usa @noble/hashes para SHA-256 (funciona sem HTTPS / contexto seguro, ao
 * contrário de window.crypto.subtle em alguns ambientes).
 *  - code_verifier: 48 bytes aleatórios → base64url
 *  - code_challenge: hash SHA-256 do verifier (método S256)
 *  - state: UUID v4 gerado manualmente (proteção CSRF)
 *
 * O verifier e o state ficam em sessionStorage (não cookie) para
 * handleCallback recuperar depois do redirect.
 *
 * A config do Keycloak (realm URL, client_id, redirect_uri, scopes) é buscada
 * em RUNTIME do backend (/api/auth/sso/config), nunca lida de env var de
 * build — replica literalmente o padrão já validado em produção no FB_APU02
 * (ver spec desta story, Code Map): cravar a config no build arrisca vazar um
 * botão de SSO quebrado para um ambiente sem o client Keycloak configurado.
 */

import { sha256 } from '@noble/hashes/sha2.js';

export const SESSION_KEY_VERIFIER = 'iam_pkce_verifier';
export const SESSION_KEY_STATE = 'iam_oauth_state';
export const SESSION_KEY_AUTH_VIA_SSO = 'auth_via_sso';

export interface IAMConfig {
  enabled: boolean;
  base_url?: string;
  client_id?: string;
  redirect_uri?: string;
  scopes?: string;
}

let cachedConfig: IAMConfig | null = null;

/** Busca (e cacheia em memória) a config de SSO deste servidor. Nunca lança —
 * em qualquer falha de rede/parse, retorna enabled:false (fail-safe: esconde
 * o botão em vez de quebrar a tela de login). */
export async function fetchIAMConfig(): Promise<IAMConfig> {
  if (cachedConfig) return cachedConfig;
  try {
    const res = await fetch('/api/auth/sso/config');
    if (!res.ok) return { enabled: false }; // não cacheia falha — próxima chamada tenta de novo
    const data = (await res.json()) as IAMConfig;
    cachedConfig = data;
    return data;
  } catch {
    return { enabled: false };
  }
}

function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let str = '';
  bytes.forEach((b) => (str += String.fromCharCode(b)));
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

async function generateCodeVerifier(): Promise<string> {
  const random = new Uint8Array(48); // 48 bytes → 64 chars base64url
  crypto.getRandomValues(random);
  return base64UrlEncode(random.buffer as ArrayBuffer);
}

// UUID v4 usando getRandomValues (funciona sem HTTPS, diferente de crypto.randomUUID())
function generateUUID(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0'));
  return `${h.slice(0, 4).join('')}-${h.slice(4, 6).join('')}-${h.slice(6, 8).join('')}-${h.slice(8, 10).join('')}-${h.slice(10).join('')}`;
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const hash = sha256(data);
  return base64UrlEncode(hash.buffer as ArrayBuffer);
}

/**
 * Gera os parâmetros PKCE, persiste verifier + state em sessionStorage, e
 * devolve a URL completa de autorização do Keycloak.
 *
 * Lança se o backend deste servidor não tiver o SSO configurado
 * (`enabled:false` ou campos essenciais ausentes) — o chamador (botão de
 * login) só deve invocar isto depois de já ter confirmado
 * `fetchIAMConfig().enabled === true`.
 */
export async function buildLoginUrl(): Promise<string> {
  const config = await fetchIAMConfig();
  if (!config.enabled || !config.base_url || !config.client_id || !config.redirect_uri) {
    throw new Error('SSO Keycloak não configurado neste servidor.');
  }

  const verifier = await generateCodeVerifier();
  const challenge = await generateCodeChallenge(verifier);
  const state = generateUUID();

  sessionStorage.setItem(SESSION_KEY_VERIFIER, verifier);
  sessionStorage.setItem(SESSION_KEY_STATE, state);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: config.client_id,
    redirect_uri: config.redirect_uri,
    scope: config.scopes || 'openid profile email',
    state,
    code_challenge: challenge,
    code_challenge_method: 'S256',
  });

  return `${config.base_url}/protocol/openid-connect/auth?${params.toString()}`;
}
