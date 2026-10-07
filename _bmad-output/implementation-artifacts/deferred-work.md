- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Runner de migrations continua para a próxima migration mesmo quando uma falha, sem parar — padrão herdado deliberadamente do FB_APU02; revisitar quando a Story 2.1 trouxer migrations reais com dependência de ordem.
  evidence: `backend/main.go` (`onDBConnected`) loga e segue em erro de `Exec`; hoje inofensivo porque `migrations/` está vazia, mas relevante assim que houver sequência real.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Runner de migrations e a stack `docker-compose` completa nunca foram exercitados de ponta a ponta nesta story.
  evidence: Docker não está instalado no sandbox de desenvolvimento; a spec proíbe explicitamente montar CI/CD nesta story. Settles when a future deploy/CI story runs `docker compose up --wait` + `curl /api/health` against this exact compose file.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Ambiente de teste do frontend (`vite.config.ts`, `test.environment: 'node'`) não está pronto para testes de componente React (sem `jsdom`/`@testing-library/react`).
  evidence: Nenhum teste existe ainda no frontend; só vira problema quando a primeira story escrever um teste de componente.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-2-login-via-sso-corporativo.md`
  summary: Logout é hoje só client-side — `tokenBlacklist` é gravado mas nunca lido, porque nenhuma rota ainda exige nosso próprio JWT (só o middleware do Keycloak existe). Revisitar quando a primeira rota autenticada por JWT próprio for construída (precisa de um middleware de "auth required" que consulte a blacklist).
  evidence: `backend/handlers/auth.go` — `tokenBlacklist.Store` em `LogoutHandler`, nenhum `Load`/checagem em nenhum middleware.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-2-login-via-sso-corporativo.md`
  summary: `iam.GetKey` (JWKS) dispara fetch síncrono segurando lock exclusivo para todo `kid` desconhecido, sem cache negativo nem rate limit — vetor de DoS barato. Herdado byte a byte do FB_APU02 (já em produção lá); corrigir só no FB_APU05 criaria inconsistência entre os sistemas irmãos — avaliar nos dois juntos.
  evidence: `backend/iam/iam_jwks_client.go` (`GetKey`), confirmado idêntico ao FB_APU02 via diff.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-2-login-via-sso-corporativo.md`
  summary: Coluna `usuarios.updated_at` nunca é escrita por nenhum caminho atual (sempre igual a `created_at`) — sem efeito até que algo atualize o perfil do usuário.
  evidence: `backend/migrations/001_create_usuarios.sql` — nenhum trigger nem UPDATE statement no diff.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-3-bootstrap-e-protecao-do-administrador.md`
  summary: Quando um admin e rebaixado/desativado via PATCH, o JWT que essa pessoa ja tem em maos continua valido ate expirar (ate 30min) - nao existe hoje um jeito de invalidar "todos os tokens do usuario X" de uma vez (blacklist e por token individual).
  evidence: `backend/handlers/middleware.go` (RequireAuth so confere blacklist por token, nao por user_id); mesmo trade-off ja aceito desde a Story 1.2 (token de acesso curto por design).
