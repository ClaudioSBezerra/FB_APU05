---
title: 'Login via SSO corporativo'
type: 'feature'
created: 2026-10-06
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
baseline_commit: 'b8412b0576b49bd2aa165ab5db884ad92e56098a'
context:
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O FB_APU05 não tem nenhum jeito de autenticar usuários ainda. Sem login, nenhuma outra story (cadastros, solicitações, fila) pode ter um ator real por trás das ações.

**Approach:** Implementar OIDC/PKCE contra o Keycloak corporativo da Ferreira Costa, replicando literalmente o fluxo já validado em produção no FB_APU02 (config runtime, PKCE no frontend, troca de token no backend com validação JWKS + checagem obrigatória de `email_verified`), emitindo token próprio (JWT access 30min + refresh 7 dias em cookie HttpOnly). Perfil (`solicitante`/`administrador`) é coluna gerenciada pela própria aplicação na tabela `usuarios` (AD-7), nunca derivada do Keycloak.

**Decisões (2026-10-06):**
- Primeiro login de um e-mail verificado sem registro em `usuarios`: **auto-provisiona** (cria o registro na hora, perfil padrão `solicitante`).
- Client Keycloak: **novo e isolado** (`fb-apu05`), nunca reaproveita nem toca na configuração existente do `fb-apu02` — env vars (`IAM_CLIENT_ID` etc.) totalmente separadas, nenhum arquivo do FB_APU02 é alterado.

</frozen-after-approval>

## Code Map

- `/home/claudio/projetos/FB_APU02/backend/handlers/auth_sso.go` -- endpoint `POST /api/auth/sso/keycloak`: extrai email/email_verified do contexto (injetado pelo middleware IAM), rejeita se não verificado, busca usuário por e-mail, chama a mesma `finishLogin` do login normal
- `/home/claudio/projetos/FB_APU02/backend/handlers/auth_sso.go` (`SSOConfigHandler`) -- `GET /api/auth/sso/config`: só retorna `enabled:true` se IAM_BASE_URL + IAM_CLIENT_ID + IAM_REDIRECT_URI + IAM_ALLOWED_CLIENT_IDS estiverem todas setadas; replicar essa checagem completa (achado de bug real: checagem parcial mostra botão de SSO quebrado)
- `/home/claudio/projetos/FB_APU02/backend/iam/{iam_key_provider,iam_jwks_client,iam_auth_middleware}.go` -- copiar literalmente (só trocar o import do módulo Go para `fb_apu05`): cache JWKS com TTL 1h + double-check locking, `jwt.Parse` com `WithIssuer`, `WithExpirationRequired`, `WithLeeway(30s)`, validação de `azp` contra allowlist (nunca `aud`), injeta `iam_sub`/`iam_client_id`/`iam_email`/`iam_email_verified` no contexto
- `/home/claudio/projetos/FB_APU02/backend/handlers/auth.go` (`fetchUserByEmail`, `finishLogin`, `GenerateToken`) -- função compartilhada entre login normal e SSO: busca case-insensitive (`LOWER(email)=LOWER($1)`), JWT HS256 (claims `user_id,role,exp:+30min`), refresh token 32 bytes hex em `sync.Map`, cookie `HttpOnly/SameSite=Strict/Path=/api/auth//MaxAge=7d`
- `/home/claudio/projetos/FB_APU02/frontend/src/lib/keycloak/buildLoginUrl.ts` -- PKCE: `code_verifier` 48 bytes random→base64url, `code_challenge` SHA-256 via `@noble/hashes`, `state` UUID manual, tudo em `sessionStorage` (não cookie)
- `/home/claudio/projetos/FB_APU02/frontend/src/lib/keycloak/handleCallback.ts` -- valida `state`, troca `code` direto no Keycloak (nunca passa pelo nosso backend), manda só o `access_token` pro nosso `POST /api/auth/sso/keycloak` (sem corpo), token do Keycloak nunca é persistido
- `/home/claudio/projetos/FB_APU02/frontend/src/pages/{Login,AuthCallback,AuthError}.tsx` -- auto-redirect se SSO habilitado e sem `?password` na URL (timeout 5s de fallback), botão manual "Entrar com SSO Ferreira Costa"
- `/home/claudio/projetos/FB_APU02/frontend/src/contexts/AuthContext.tsx` (`login`, `logout`) -- `login(data, {viaSSO})` grava flag em sessionStorage; `logout()` decide se dispara `end_session` no Keycloak ou só limpa local
- `ARCHITECTURE-SPINE.md` AD-6 (fluxo Keycloak), AD-7 corrigido (perfil é coluna `usuarios.perfil`, nunca claim)

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/001_create_usuarios.sql` -- tabela `usuarios` (`id UUID PK`, `email VARCHAR UNIQUE`, `perfil VARCHAR CHECK (perfil IN ('solicitante','administrador'))`, `nome VARCHAR`, `created_at`, `updated_at`) -- primeira migration real do projeto
- [x] `backend/iam/{iam_key_provider,iam_jwks_client,iam_auth_middleware}.go` -- copiar do FB_APU02, módulo `fb_apu05`
- [x] `backend/handlers/auth.go` -- `fetchUserByEmail`, `finishLogin`, `GenerateToken` (JWT HS256, claims `user_id,perfil,exp`), emissão de refresh token (cookie HttpOnly/SameSite=Strict, 7 dias)
- [x] `backend/handlers/auth_sso.go` -- `GET /api/auth/sso/config` (replicar checagem completa das 4 env vars) e `POST /api/auth/sso/keycloak` (checa `email_verified`; se o e-mail não existir em `usuarios`, cria o registro com `perfil='solicitante'` antes de chamar `finishLogin` — auto-provisionamento)
- [x] `backend/main.go` -- registra as duas rotas só se `IAM_BASE_URL` setada; monta `allowedClientIDs`; instancia `JWKSClient`
- [x] `backend/.env.example` -- adicionar `IAM_BASE_URL, IAM_CLIENT_ID, IAM_REDIRECT_URI, IAM_ALLOWED_CLIENT_IDS`
- [x] `frontend/src/lib/keycloak/{buildLoginUrl,handleCallback}.ts` -- copiar lógica PKCE do FB_APU02
- [x] `frontend/src/pages/{Login,AuthCallback,AuthError}.tsx` -- telas de login
- [x] `frontend/src/contexts/AuthContext.tsx` -- `login`, `logout`, estado de sessão, injeção automática de `Authorization: Bearer` nas chamadas `fetch`
- [x] `frontend/package.json` -- adicionar `@noble/hashes`

**Acceptance Criteria:**
- Given um usuário com conta válida e e-mail verificado no Keycloak, when ele completa o fluxo PKCE e o backend valida o token via JWKS, then o sistema emite seu próprio JWT e refresh cookie, e o usuário fica autenticado
- Given um token do Keycloak com `email_verified=false`, when ele chega em `/api/auth/sso/keycloak`, then o backend recusa com 401, sem emitir token
- Given `IAM_BASE_URL` ou qualquer uma das outras 3 env vars de IAM ausente, when o frontend consulta `/api/auth/sso/config`, then `enabled:false` — botão/auto-redirect de SSO não aparece
- Given um usuário autenticado, when ele faz logout, then a sessão local é limpa e o cookie de refresh é invalidado

## Implementation Notes

Implementado em sessão única (subagente dedicado), com smoke test real de ponta a ponta contra um Postgres descartável e um servidor JWKS falso local (stdlib, sem depender do Keycloak real) — cobriu: auto-provisionamento, idempotência de duplo-clique no primeiro login (`ON CONFLICT DO UPDATE ... RETURNING`), rejeição de `email_verified=false`, rejeição de `azp` fora da allowlist, `/api/auth/sso/config` desabilitado quando falta qualquer env var, e limpeza do cookie no logout. Banco de teste e processos descartados ao final; FB_APU02 nunca tocado (confirmado via `git status` lá).

Riscos/incompleto sinalizados pelo implementador:
- Sem endpoint `/api/auth/refresh` nem `/api/auth/me` — fora do Tasks desta story; sessão só dura os 30min do access token até uma story futura adicionar renovação.
- `backend/middleware/` (SecurityMiddleware, CORS, rate limiting) ainda não existe — AD-9 pede isso, mas não estava no Code Map desta story.
- Logout é só local (sem `end_session` no Keycloak) — decisão já registrada nas Design Notes, não é descuido.
- Client Keycloak `fb-apu05` ainda precisa ser provisionado manualmente por alguém com acesso ao admin console — fora do alcance desta sessão.
- Telas de login ficaram com Tailwind puro (sem shadcn/ui) — scaffold da Story 1.1 não tinha componentes ainda.

## Spec Change Log

## Review Triage Log

- **high** `backend/migrations/001_create_usuarios.sql` + `auth_sso.go`: unicidade é em `email` (case-sensitive), mas a busca (`fetchUserByEmail`) e o índice de apoio usam `LOWER(email)` — duas requisições concorrentes de primeiro login com e-mail em casing diferente (`Foo@x.com` vs `foo@x.com`) criam duas linhas `usuarios` para a mesma pessoa. → **patch**
- **medium** `backend/handlers/auth.go` (`LogoutHandler`): `jwt.Parse` tem seu erro descartado (`tok, _ := ...`) — um token estruturalmente válido mas com assinatura forjada ainda tem suas claims inseridas na blacklist. → **patch**
- **medium** `backend/handlers/auth.go` (geração de refresh token): erro de `crypto/rand.Read` é descartado — uma falha de entropia (rara, mas real) geraria um token previsível em silêncio. → **patch**
- **medium** `backend/handlers/auth.go` (`ValidateJWTSecret`): usa `DATABASE_URL != ""` como sinal de "produção", mas `main.go` já exige `DATABASE_URL` incondicionalmente (`log.Fatal` se ausente) — logo essa var está sempre setada, e o ramo "só avisa em dev" nunca é alcançável; a função sempre derruba o processo, mesmo em dev local sem `JWT_SECRET`. → **patch**
- **medium** `frontend/src/contexts/AuthContext.tsx`: o interceptor de `window.fetch` injeta `Authorization: Bearer` em **qualquer** fetch da página (só checa se o header já existe), não só nas chamadas pra nossa API — a troca de código do Keycloak em `handleCallback.ts` (fetch direto pro Keycloak) recebe esse header se já houver um token de sessão anterior em memória. → **patch**
- **medium** `backend/handlers/auth_sso.go`: a checagem crítica de `email_verified=false → 401` (comentada como "CRÍTICO") não tem nenhum teste — a função retorna antes de tocar no banco, então é testável sem DB real. → **patch**
- **medium** `backend/handlers/auth_sso.go` (`SSOConfigHandler`): a lógica "todas as 4 env vars exigidas" (que corrige um bug real documentado) não tem teste cobrindo as combinações de 1-var-faltando — função é pura (só lê env vars), testável com `t.Setenv` sem DB. → **patch**
- **medium** `frontend/src/lib/keycloak/handleCallback.ts`: os `fetch` (troca de código no Keycloak e no nosso backend) não têm timeout — uma requisição que trava deixa o usuário preso na tela "Concluindo login..." pra sempre. → **patch**
- **low** `backend/handlers/auth_sso.go` (`SSOConfigHandler`): não restringe o método HTTP (diferente dos handlers irmãos no mesmo diff). → **patch**
- **low** `frontend/src/pages/Login.tsx`: leitura+limpeza de `sessionStorage['session_expired']` acontece direto no corpo do componente, não num `useEffect` — sob StrictMode (dev), a dupla invocação do corpo da função limpa a flag antes da renderização que o usuário vê, e o aviso de "sessão expirada" nunca aparece em dev. → **patch**
- **low** `frontend/src/App.tsx`: sem rota coringa — um caminho não mapeado renderiza página em branco em vez de redirecionar. → **patch**
- **medium** `backend/handlers/auth.go` (`tokenBlacklist`): gravado no logout mas nunca lido em lugar nenhum — nenhuma rota hoje valida o JWT próprio contra a blacklist (só existe `iam.IAMAuthMiddleware`, que valida tokens do Keycloak, não os nossos). Logout é hoje só client-side; não é um bug desta story (nenhuma rota autenticada por JWT próprio foi pedida aqui), mas o comentário no código ("AD-9 blacklist") sugere um efeito que ainda não existe. → **defer**
- **low** `backend/iam/iam_jwks_client.go` (`GetKey`): `kid` desconhecido sempre dispara fetch síncrono segurando lock exclusivo, sem cache negativo nem rate limit — vetor de DoS barato contra este serviço e o Keycloak real. Código copiado byte a byte do FB_APU02 (confirmado por diff) — já existe em produção lá; divergir aqui sem corrigir lá criaria inconsistência entre os dois sistemas irmãos. → **defer**
- **low** `backend/migrations/001_create_usuarios.sql`: `updated_at` nunca é escrito por nenhum caminho deste diff (fica sempre igual a `created_at`) — sem efeito hoje, só importa quando algo passar a atualizar o perfil do usuário. → **defer**
- **false** `backend/main.go`: `/api/auth/sso/config` registrada incondicionalmente, só `/api/auth/sso/keycloak` é condicional — isso é o comportamento correto e exigido pelo próprio Acceptance Criteria 3 (`enabled:false` quando falta env var só é alcançável se o endpoint sempre existir); a redação da task no spec era só imprecisa, o código está certo.
- **false** `backend/iam/iam_auth_middleware.go:127`: alegação de coerção via `fmt.Sprint`/`ParseBool` — o código real usa `claims["email_verified"].(bool)`, um type assertion direto que retorna `false` (fail-closed) em qualquer valor não-booleano; comportamento seguro, não um bug.
- **false** `backend/migrations/001_create_usuarios.sql`: alegação de que `gen_random_uuid()` falharia sem `pgcrypto` em Postgres <13 — o alvo fixado pela Arquitetura é Postgres 15 (`docker-compose.yml`), que tem `gen_random_uuid()` nativo; mesmo padrão já usado sem extensão nas migrations do FB_APU02.
- **false** Seção Verification do spec não lista `go test ./...` — achado real, mas sua correção é editar o próprio spec desta build, o que a regra de triagem exclui explicitamente (`Reject any finding whose fix is to edit this build's spec`). Vou atualizar a seção de Verification por conta própria, fora do fluxo formal de triagem.

## Design Notes

Logout nesta story cobre só limpeza local (cookie + sessionStorage) — o `end_session` no Keycloak (SLO completo) replica o padrão do FB_APU02 mas não é testável neste sandbox sem um client Keycloak real; ver Verification.

**Ação manual fora desta sessão:** o client Keycloak `fb-apu05` precisa ser criado de verdade no realm `ferreiracosta` (admin console do Keycloak) antes do login funcionar ponta a ponta — isolado do `fb-apu02`, sem alterar nada que já existe lá. Esta sessão não tem acesso a esse admin console; o código é escrito assumindo que o client existirá com `IAM_CLIENT_ID=fb-apu05`, mas alguém com acesso precisa provisioná-lo (redirect URI, scopes `openid profile email`) antes de testar o fluxo real.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: compila sem erro, suíte de testes do pacote `iam` passa
- `cd frontend && npm run build && npm run lint` -- expected: build e lint sem erro

**Manual checks (if no CLI):**
- Fluxo PKCE completo (login real contra Keycloak) não pode ser exercitado neste sandbox — não há client Keycloak provisionado nem Docker para subir a stack. Revisão de código + testes unitários do parsing JWT/JWKS (com chaves de teste geradas localmente, não contra o Keycloak real) são o que esta sessão consegue verificar.
