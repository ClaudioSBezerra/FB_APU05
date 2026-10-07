---
title: 'Bootstrap e proteção do administrador'
type: 'feature'
created: 2026-10-06
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
followup_pass: true
baseline_revision: 'dce0b84f0ae961ab161ae678a02d09dbdcf9bd53'
context:
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
deferred:
  - summary: >-
      Nenhum teste verifica que PATCH /api/admin/usuarios/{id} está de fato
      registrado atrás de RequireAuth(..., "administrador") em main.go.
    evidence: |-
      Busca por TestMain/httptest.NewServer em backend só encontra uso em
      iam/iam_auth_middleware_test.go; nada cobre o roteamento de main.go.
      Uma regressão que remova o wrapper RequireAuth ou troque o
      perfilExigido não derrubaria nenhum teste existente. O menor fix
      exigiria extrair o registro de rotas de main() para uma função
      testável — refactor estrutural sem precedente hoje no repo (nem a
      rota de logout da Story 1.2 tem teste de fiação).
    location: >-
      backend/main.go:202
    severity: medium
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Hoje não existe nenhum jeito de o sistema ter um primeiro administrador, nem de conceder esse perfil a outra pessoa, nem nada que impeça o sistema de ficar sem nenhum admin ativo (FR-2). Também não existe ainda nenhuma rota protegida pelo JWT próprio do FB_APU05 — a `tokenBlacklist` criada na Story 1.2 nunca é lida em lugar nenhum.

**Approach:** Criar o primeiro middleware de "exige login" (JWT próprio + checagem da blacklist, fechando a lacuna deixada pela Story 1.2), um endpoint administrativo para conceder/revogar perfil `administrador` e ativar/desativar um usuário (com trilha de auditoria), e um procedimento de bootstrap documentado e fora da API (script SQL com placeholder, nunca um e-mail real hardcoded) para o primeiro administrador. Toda mudança que reduziria o número de administradores ativos para zero é recusada.

Não existe, no FB_APU02, um padrão formal de bootstrap do primeiro admin (confirmado por investigação: só migrations ad-hoc com e-mail real hardcoded) nem o conceito de "proteger o último admin" — esta é a primeira vez que esse requisito é desenhado, não uma replicação literal.

</frozen-after-approval>

## Code Map

- `/home/claudio/projetos/FB_APU02/backend/handlers/auth.go:214-266` (`AuthMiddleware`) -- forma geral a seguir: um middleware parametrizado por perfil exigido, extrai claims do JWT próprio, injeta no contexto; adaptar para também checar a `tokenBlacklist` (gap que a Story 1.2 deixou) e usar a claim `perfil` (não `role`)
- `/home/claudio/projetos/FB_APU02/backend/handlers/admin.go:539-595` (`PromoteUserHandler`) -- modelo mais próximo pro endpoint de conceder perfil (mas lá não há proteção de "último admin" — isso é novo)
- `/home/claudio/projetos/FB_APU02/backend/migrations/021b_ensure_admin_user.sql` -- **anti-padrão a NÃO replicar**: e-mail real e hash hardcoded direto numa migration versionada. FB_APU05 usa um script separado (fora de `migrations/`) com placeholder, nunca um e-mail real no repositório (dado sensível, repositório público)
- `backend/handlers/auth.go` (desta sessão, Story 1.2) -- `GenerateToken`, `tokenBlacklist`, `jsonErr`, claims `user_id`/`perfil`/`exp`
- `backend/migrations/001_create_usuarios.sql` -- tabela `usuarios` atual (sem coluna `ativo` ainda)

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/002_add_ativo_and_auditoria.sql` -- adiciona `usuarios.ativo BOOLEAN NOT NULL DEFAULT true`; cria `usuarios_auditoria` (id, ator_id, usuario_alvo_id, acao, perfil_anterior, perfil_novo, ativo_anterior, ativo_novo, created_at)
- [x] `backend/handlers/middleware.go` -- `RequireAuth(next http.HandlerFunc, perfilExigido string) http.HandlerFunc`: valida JWT próprio (mesmo `JWT_SECRET`), checa `tokenBlacklist`, injeta claims no contexto; 401 se inválido/revogado, 403 se `perfilExigido` não bate
- [x] `backend/handlers/admin.go` -- `PATCH /api/admin/usuarios/{id}` (protegido por `RequireAuth(..., "administrador")`): body `{perfil?: "administrador"|"solicitante", ativo?: bool}`; antes de aplicar uma mudança que reduziria para zero o número de `usuarios` com `perfil='administrador' AND ativo=true`, recusa com 409; toda mudança aplicada grava uma linha em `usuarios_auditoria`
- [x] `backend/scripts/bootstrap_admin.sql` -- script manual (fora de `migrations/`, nunca roda automaticamente): `UPDATE usuarios SET perfil='administrador' WHERE email = '<SUBSTITUA_PELO_EMAIL_REAL>'` com comentário de instrução; documentar no README a execução via `psql` após o primeiro login SSO da pessoa (ela precisa existir em `usuarios` antes — nasce via auto-provisionamento da Story 1.2)
- [x] `backend/main.go` -- registrar a rota `PATCH /api/admin/usuarios/{id}` atrás de `RequireAuth`
- [x] `README.md` -- seção "Primeiro acesso administrativo": login via SSO cria o usuário como `solicitante`; rodar `bootstrap_admin.sql` substituindo o placeholder para promover a pessoa a `administrador`

**Acceptance Criteria:**
- Given um banco recém-criado sem nenhum administrador, when o procedimento de bootstrap (script + instrução do README) é executado após o primeiro login da pessoa, then existe exatamente um administrador ativo
- Given um administrador autenticado, when ele concede perfil `administrador` a outro usuário via `PATCH /api/admin/usuarios/{id}`, then o perfil muda e uma linha é gravada em `usuarios_auditoria`
- Given exatamente um administrador ativo, when alguém tenta rebaixá-lo (`perfil: "solicitante"`) ou desativá-lo (`ativo: false`) via essa rota, then o sistema recusa com 409, sem aplicar a mudança
- Given um usuário com perfil `solicitante` autenticado, when ele chama `PATCH /api/admin/usuarios/{id}`, then recebe 403
- Given um token revogado (blacklist da Story 1.2), when ele é usado em qualquer rota protegida por `RequireAuth`, then recebe 401

## Implementation Notes

Implementado em sessão única. Resumo das decisões de implementação:

- `RequireAuth` (`backend/handlers/middleware.go`) adaptado de `AuthMiddleware` do FB_APU02 com duas diferenças deliberadas: (1) checa `tokenBlacklist` antes de validar a assinatura, fechando o gap documentado no Problem desta spec; (2) exige igualdade exata da claim `perfil` (não `role`, AD-7) com `perfilExigido`, sem o bypass implícito "admin sempre passa" do modelo original — evita um bug sutil se um perfil intermediário surgir no futuro.
- `AdminUpdateUsuarioHandler` (`backend/handlers/admin.go`) roda inteiro dentro de uma transação: `SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE` trava a linha-alvo e lê o estado anterior; quando a mudança tiraria o usuário-alvo da condição "administrador ativo", uma segunda query (`SELECT id FROM usuarios WHERE perfil='administrador' AND ativo=true FOR UPDATE`) trava **todo o conjunto** de administradores ativos antes de recontar — não um `COUNT(*)` solto. Isso importa porque o Postgres recusa `FOR UPDATE` combinado com função agregada, e porque travar só a linha-alvo não fecharia a corrida entre duas requisições mirando **dois administradores diferentes** ao mesmo tempo (cada uma travaria sua própria linha sem bloquear a outra, e ambas poderiam ler "ainda há 2 admins" antes de qualquer commit). Com o conjunto inteiro travado, a segunda requisição concorrente bloqueia até a primeira commitar ou abortar (e, no caso raro de deadlock entre duas linhas-alvo diferentes, o Postgres aborta uma delas — o cliente recebe 500 e pode reenviar; o reenvio então enxerga o estado já consistente e é corretamente aceito ou recusado com 409). PATCH sem nenhum campo alterado (no-op) não grava auditoria.
- `backend/scripts/bootstrap_admin.sql` segue exatamente o padrão pedido: fora de `migrations/`, só placeholder (`<SUBSTITUA_PELO_EMAIL_REAL>`), nunca um e-mail real — confirmado por `grep -n "@"` no arquivo (zero ocorrências).
- `PATCH /api/admin/usuarios/{id}` registrado em `main.go` usando o roteamento por método+wildcard do `net/http` nativo do Go 1.22 (`"PATCH /api/admin/usuarios/{id}"`), sem introduzir nenhuma dependência de router externo.

Verificação real executada (sem Docker/credenciais de Postgres disponíveis neste sandbox — ver Verification):
- `go build ./... && go vet ./... && go test ./...` — compila sem erro, suíte completa (incluindo a nova) passa.
- Testes novos adicionados cobrindo os trechos que não dependem de banco: `backend/handlers/middleware_test.go` (401 sem token, 403 perfil incorreto, 401 token revogado via blacklist, caminho feliz injeta `user_id` no contexto) e `backend/handlers/admin_test.go` (400 corpo vazio, 400 perfil inválido, 400 id ausente, 405 método errado, mapeamento de `auditAction`).
- A lógica transacional (proteção do último admin, auditoria, FOR UPDATE) foi verificada por leitura cuidadosa do código e por um traçado manual do cenário de corrida de duas requisições concorrentes mirando dois administradores diferentes (documentado acima) — **não foi exercitada contra um Postgres real** neste sandbox (sem Docker disponível e sem credenciais para o Postgres local já em execução na máquina). Esse é o principal risco residual desta implementação — ver seção de riscos abaixo.

Riscos/incompleto sinalizados pelo implementador:
- **Não testado ponta a ponta contra Postgres real**: as 5 Acceptance Criteria desta story (bootstrap, concessão de perfil, proteção do último admin, 403 para solicitante, 401 para token revogado) foram verificadas por unit test (para as partes sem DB) e por leitura cuidadosa do SQL/transação (para as partes com DB), mas não por um teste de integração rodando de fato contra Postgres 15 — recomendo rodar manualmente via `docker-compose up` antes de considerar esta story pronta para produção.
- Cenário de deadlock entre duas requisições concorrentes mirando dois administradores-alvo diferentes ao mesmo tempo resulta em 500 (erro genérico) para uma delas, não em 409 — tecnicamente correto (o cliente pode reenviar e recebe a resposta certa), mas um 409 explícito seria uma experiência melhor; não implementado por ser uma janela de corrida extremamente estreita e por manter o tratamento de erro consistente com o resto do arquivo.
- `backend/handlers/middleware.go` não tem endpoint de "whoami"/`/api/auth/me` para o frontend consumir claims — fora do escopo desta story (Code Map não pede isso).
- Nenhuma tela de frontend foi criada para a rota administrativa (gestão de usuários) — a spec só pede o endpoint backend + script + README; UI fica para uma story futura.

## Spec Change Log

## Review Triage Log

> Nota: o dev-pass original (sessão automática do bmad-loop) travou por limite de uso da conta antes de concluir o review gate. Retomado manualmente em 2026-10-07 — rodei os 3 reviewers do zero contra o diff já produzido.

- **high** `backend/handlers/admin.go`: a proteção do último administrador (409) — a garantia central desta story — não tem nenhum teste que a exercite de verdade; os 5 testes existentes passam `db=nil` e só cobrem os ramos anteriores ao acesso ao banco. Uma inversão/off-by-one em `totalAdminsAtivos <= 1` ou nas flags `eraAdminAtivo`/`seraAdminAtivo` passaria despercebida. → **patch**
- **medium** `backend/handlers/admin.go`: a escrita em `usuarios_auditoria` (e o skip no caminho no-op) também não tem nenhum teste que a exercite — mesma causa raiz (precisa de banco real ou mock). → **patch**
- **medium** `backend/handlers/admin.go`: `{id}` do path vai direto pra `WHERE id = $1` contra coluna `UUID` sem validar formato antes — um id malformado vira erro de sintaxe do Postgres, caindo no `err != nil` genérico → 500 em vez de 400. → **patch**
- **low** `backend/handlers/admin.go`: a trava `FOR UPDATE` do conjunto de admins ativos não usa `ORDER BY` — duas requisições concorrentes mirando dois admins-alvo diferentes podem travar linhas em ordens diferentes (risco de deadlock que o próprio implementador já documentou como resultando em 500, retentável). `ORDER BY id` reduz a janela. → **patch**
- **low** `backend/scripts/bootstrap_admin.sql`: `UPDATE` sem `RETURNING` — um e-mail digitado errado no placeholder vira "UPDATE 0" silencioso, sem sinal de que o bootstrap falhou. → **patch**
- **low** `backend/migrations/002_add_ativo_and_auditoria.sql`: falta índice em `ator_id` (só `usuario_alvo_id` tem) — "o que o admin X fez" é uma consulta de auditoria tão natural quanto "o que aconteceu com Y". → **patch**
- **low** `backend/migrations/002_add_ativo_and_auditoria.sql`: `acao VARCHAR(50)` sem `CHECK` amarrado aos 3 valores que `auditAction()` realmente produz — `usuarios.perfil` já tem esse padrão (migration 001), ficou inconsistente aqui. → **patch**
- **low** `backend/handlers/admin.go`: `json.NewDecoder(r.Body)` sem limite de tamanho (`http.MaxBytesReader`) — hygiene padrão, sem downside. → **patch**
- **low** `backend/handlers/middleware.go`: prefixo `"Bearer "` checado case-sensitive — como só nosso próprio frontend chama essa rota (sempre manda `Bearer` com B maiúsculo), impacto prático é baixo, mas o fix é trivial e sem downside. → **patch**
- **false** `backend/handlers/middleware.go` + `auth.go`: alegação de "algorithm confusion" por `jwt.Parse` não restringir `WithValidMethods`. Verificado: o sistema só assina/verifica com um único segredo simétrico (HMAC) em toda a aplicação — não existe nenhuma chave assimétrica (RSA/EC) nossa para um atacante "confundir" (a verificação RS256 do Keycloak é um caminho de código totalmente separado, em `iam/`, nunca usado para os tokens próprios). `alg: none` já é rejeitado pelo golang-jwt v5 por padrão (exige o marcador `UnsafeAllowNoneSignatureType`, que o keyfunc nunca retorna). Sem chave pública para abusar e sem bypass de `none`, não há escalonamento real possível aqui. Vou adicionar `jwt.WithValidMethods(["HS256"])` mesmo assim, como higiene — mas fora do fluxo formal de triagem (fix de graça, não é correção de um achado real).
- **defer** `backend/handlers/middleware.go`: quando um admin é rebaixado/desativado, o JWT que essa pessoa já tem continua válido até expirar (até 30min) — não existe hoje um jeito de invalidar "todos os tokens do usuário X" (a blacklist é por token individual, não por `user_id`), e fazer `RequireAuth` reconsultar o banco a cada requisição anula o propósito de um JWT sem estado. Troca-off já existente desde a Story 1.2 (mesmo motivo do token de acesso durar só 30min); não é escopo desta story resolver.

### 2026-10-07 — Review pass

> Pass de follow-up (`followup_pass: true`) sobre o spec já `done`: 4 camadas (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) rodadas do zero contra o diff completo desde `baseline_revision`.

- verdicts: 21 findings — high 0, medium 5, low 5, false 11, maybe-false 0
- findings:
  - `medium` `patch` (blind-hunter) `TestAdminUpdateUsuarioHandler_RejectsEmptyBody` usa id `"abc"`, que já é recusado pelo guard de formato UUID antes do corpo ser decodificado — o teste nunca exercita a validação "informe ao menos 'perfil' ou 'ativo'" que seu nome promete cobrir. — Confirmado lendo `admin.go:60-63` (guard de UUID roda antes da decodificação do corpo) e o teste original (id `"abc"`, só checava `rec.Code`). Corrigido: teste agora usa `testAlvoID` (UUID válido) e também assere a mensagem de erro específica.
  - `medium` `patch` (blind-hunter) `TestAdminUpdateUsuarioHandler_RejectsInvalidPerfil` tem a mesma falha — id `"abc"` trava no guard de UUID antes de alcançar a validação de `perfil`. — Mesma causa raiz do item anterior. Corrigido junto: teste agora usa `testAlvoID` e assere a mensagem "perfil deve ser...".
  - `medium` `patch` (blind-hunter) Nenhum teste dedicado cobria o próprio guard de "id inválido" (UUID malformado) com uma asserção que de fato prove que é esse o motivo do 400. — Os dois testes acima eram os únicos call-sites a passar por ali, e nenhum verificava a mensagem. Corrigido: novo teste `TestAdminUpdateUsuarioHandler_RejectsInvalidID` (id `"abc"`, corpo válido) assere especificamente a mensagem "id inválido".
  - `medium` `patch` (verification-gap) `Broken-verification gap`: os mesmos dois testes de validação do admin handler nunca alcançam o código que dizem cobrir (id `"abc"` satisfaz o 400 antes da checagem de corpo/perfil) — uma remoção das duas validações de entrada do FR-2 não derrubaria nenhum teste. — Verificado lendo `admin_test.go` original e `admin.go:60-78`; mesma causa raiz já confirmada acima. Corrigido pelas mesmas três mudanças.
  - `low` `patch` (blind-hunter) Corrida de deadlock entre duas requisições concorrentes mirando dois administradores-alvo DIFERENTES: cada uma trava sua própria linha-alvo primeiro (ordem não relacionada entre as duas) e só depois tenta travar o conjunto de admins ativos com `ORDER BY id` — o `ORDER BY` do conjunto não cobre a ordem das travas de linha-alvo já feitas antes dele, então as duas transações podem se esperar mutuamente. — Confirmado lendo `admin.go`: o lock da linha-alvo (`SELECT ... WHERE id = $1 FOR UPDATE`) roda antes do lock do conjunto, fora de qualquer ordenação global. Já era um risco documentado (Implementation Notes/triage anterior) como aceitável por ser raro e retentável (500); patch trivial aplicado mesmo assim: `SELECT pg_advisory_xact_lock($1)` com chave fixa como primeira instrução da transação, serializando toda mutação administrativa e fechando a corrida por completo (não só reduzindo a janela).
  - `low` `patch` (blind-hunter) O desenho de concorrência (travar o conjunto inteiro de admins) nunca é exercitado por uma concorrência real — os testes `sqlmock` só simulam uma transação sequencial. — Confirmado: `admin_test.go` só tem três cenários sqlmock, todos sequenciais, nenhum com duas goroutines/transações simultâneas. Mitigado pelo mesmo advisory lock acima, que torna o comportamento correto por construção (serialização) em vez de depender de um teste de concorrência real contra Postgres (fora do alcance deste sandbox, mesmo risco residual já registrado na seção de Verification).
  - `low` `patch` (intent-alignment) Divergência D1 (lido no código/comentários) vs D2 (exercitado por execução concorrente real): a garantia central de "nunca zero admins" é argumentada em prosa e codificada na forma das queries, mas os testes verificam só a sequência de SQL emitida, não a ausência real de corrida. — Mesma causa raiz dos dois itens acima; mesma mitigação (advisory lock + risco residual já documentado).
  - `low` `patch` (verification-gap) `LogoutHandler`'s `jwt.WithValidMethods(["HS256"])` (hardening adicionado nesta story) não tem nenhum teste em todo o repositório — busca por `LogoutHandler`/`/api/auth/logout` em todos os `*_test.go` não encontrou nenhuma referência. — Confirmado pela mesma busca. Corrigido: novo arquivo `backend/handlers/auth_logout_test.go` com `TestLogoutHandler_BlacklistsValidToken` (caminho feliz HS256) e `TestLogoutHandler_RejectsNonHS256Token` (token HMAC válido mas assinado com HS384 não deve ser blacklistado).
  - `low` `patch` (edge-case-hunter) `backend/scripts/bootstrap_admin.sql` só define `perfil = 'administrador'`, nunca `ativo` — se o alvo já estiver com `ativo=false` quando o script roda, o resultado é um "administrador" inativo, e o `RETURNING` original (sem a coluna `ativo`) não deixava isso visível ao operador. — Inatingível no cenário documentado de bootstrap do primeiro admin (nenhum admin existe ainda para ter desativado alguém), mas o script é reutilizável para promoções posteriores onde isso é possível. Corrigido: `SET ativo = true` adicionado e `ativo` incluído no `RETURNING`.
  - `false` (blind-hunter) README diz que admins podem ativar/desativar "outros" usuários, mas o handler não impede um admin de se auto-alvejar. — Refutado: "outros" no README descreve o caso de uso típico (agir sobre outra pessoa), não uma restrição técnica; nem o Intent nem as Acceptance Criteria da spec pedem bloqueio de auto-alvo, e a proteção do último admin e as demais checagens se aplicam igualmente independente de `alvoID == atorID` — nenhum comportamento incorreto resulta disso.
  - `false` (blind-hunter) `bootstrap_admin.sql` não verifica se alguma linha foi de fato afetada pelo `UPDATE`. — Refutado: o `RETURNING` (já adicionado num pass de review anterior) mais o comportamento padrão do `psql -f` (que imprime `UPDATE 0`/conjunto vazio no terminal) já sinalizam visivelmente ao operador humano que executa este procedimento manual e interativo que nada foi alterado; não há falha silenciosa no caminho de uso documentado.
  - `medium` `defer` (blind-hunter) Nenhum teste verifica a fiação real em `main.go` — que `PATCH /api/admin/usuarios/{id}` está de fato atrás de `RequireAuth(..., "administrador")`; uma regressão que remova esse wrapper ou troque o `perfilExigido` não derrubaria nenhum teste. — Confirmado: busca por `TestMain`/`httptest.NewServer` em `backend` só encontra uso em `iam`, nada cobrindo o roteamento de `main.go`. Real e de impacto potencialmente alto (endpoint administrativo ficaria sem autenticação), mas o menor fix exigiria extrair o registro de rotas de `main()` para uma função testável — um refactor estrutural sem nenhum precedente hoje no repo (nem a rota de logout da Story 1.2 tem teste de fiação). Registrado em `deferred` por ser um gap de testabilidade transversal, não específico desta rota.
  - `false` (blind-hunter) `GetUserIDFromContext` pode retornar string vazia quando as claims não têm `user_id`, e esse valor vai sem checagem para o INSERT em `usuarios_auditoria.ator_id` (coluna UUID). — Refutado: `GenerateToken` (`auth.go`) sempre grava o `user_id` real do usuário autenticado como claim; `RequireAuth` só aceita tokens assinados com o `JWT_SECRET` do próprio servidor (HS256 fixado via `WithValidMethods`). Não existe caminho legítimo para um token validamente assinado sem `user_id` sem antes comprometer o segredo de assinatura — fora do escopo desta story.
  - `false` (edge-case-hunter) Mesma alegação de `GetUserIDFromContext` retornando vazio e causando 500 opaco no INSERT de auditoria. — Mesma refutação do item anterior.
  - `false` (blind-hunter) `002_add_ativo_and_auditoria.sql` não traz migration de rollback/down, "diferente do cuidado tomado em outras partes do PR". — Refutado: a premissa é falsa — `001_create_usuarios.sql` (Story 1.2) também não tem rollback; este repositório não tem nenhuma convenção de down-migration, então 002 é consistente com a prática existente, não um desvio dela.
  - `false` (blind-hunter) Remoção de `golang.org/x/crypto` de `go.mod`/`go.sum` sem o diff provar que nenhum outro arquivo ainda depende dela. — Refutado pela própria camada verification-gap: `grep -rn "x/crypto|bcrypt" backend --include=*.go` não encontrou nenhuma referência remanescente — limpeza de dependência morta confirmada como segura.
  - `false` (intent-alignment) "Unremarked scope": a mesma remoção de `golang.org/x/crypto` não é rastreável ao Intent citado. — Mesma refutação do item anterior (confirmado seguro pela verification-gap); não é uma regressão, é limpeza correta.
  - `false` `reject` (intent-alignment) Divergência A (garantia de "nunca zero admins" como invariante de todo o sistema) vs B (só este handler aplica a checagem) — nenhuma trava a nível de banco (trigger/constraint). — Refutado: nenhum outro caminho de código nesta diff ou no restante do repositório escreve `usuarios.perfil`/`usuarios.ativo` de um usuário existente; o script de bootstrap só promove (nunca rebaixa/desativa), então não existe hoje nenhuma forma de violar a invariante fora deste handler.
  - `false` `reject` (intent-alignment) Divergência C1 (trilha de auditoria como registro persistido) vs C2 (trilha de auditoria como algo consultável por um admin via produto) — não existe endpoint de leitura de `usuarios_auditoria`. — Rejeitado: Tasks & Acceptance só pedem gravar a trilha ("com trilha de auditoria"), nunca um endpoint de consulta; Implementation Notes já registra explicitamente que a UI/consumo fica para uma story futura. Não é um intent_gap — simplesmente nunca foi pedido, não há comportamento incorreto a corrigir.
  - `false` (intent-alignment) Divergência E1 (procedimento documentado, baseado em disciplina humana) vs E2 (mecanismo estrutural que impediria tecnicamente um e-mail real commitado) para o bootstrap. — Refutado: o Intent pede literalmente "script SQL com placeholder" + procedimento documentado (leitura A), não um mecanismo de prevenção estrutural; o diff implementa exatamente a leitura aprovada do Intent.
  - `false` (intent-alignment) Hardening de `jwt.WithValidMethods` e comentários que referenciam Design Notes/FB_APU02 fora do texto literal do Intent — escopo não solicitado. — Refutado: o hardening de JWT já tinha sido explicitamente marcado no pass de review anterior como "fix de graça" (higiene voluntária, fora da triagem formal); inofensivo, sem efeito colateral negativo.

## Design Notes

A proteção do "último admin" é avaliada em uma única query antes do UPDATE, dentro da mesma transação, para evitar corrida entre duas requisições concorrentes de desativação (duas requisições lendo "ainda há 2 admins" ao mesmo tempo e ambas desativando, deixando zero) — usar `SELECT ... FOR UPDATE` ou equivalente na linha-alvo e recontagem dentro da transação.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: compila sem erro, testes existentes (iam, handlers) continuam passando -- **executado, passou** (build OK, vet OK, `ok fb_apu05/handlers`, `ok fb_apu05/iam`, incluindo os testes novos de `RequireAuth` e `AdminUpdateUsuarioHandler` que não dependem de banco)

**Manual checks (if no CLI):**
- Revisar `bootstrap_admin.sql` para confirmar que não contém nenhum e-mail real, só o placeholder. -- **feito**: `grep -n "@" backend/scripts/bootstrap_admin.sql` não retornou nenhuma ocorrência.
- **Pendente**: teste de integração ponta a ponta contra Postgres real (`docker-compose up` + exercitar as 5 Acceptance Criteria com `curl`/`psql`) — este sandbox não tinha Docker nem credenciais para o Postgres local já em execução na máquina; a lógica transacional (FOR UPDATE no conjunto de admins ativos, proteção do último admin, trilha de auditoria) foi verificada por leitura de código e traçado manual de cenário de corrida, não por execução real. Recomendo rodar isso antes do merge.

## Auto Run Result

**Resumo da mudança:** Pass de follow-up review (`followup_pass: true`) sobre a Story 1.3, já `done` de uma sessão anterior. Rodei as 4 camadas de review (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) do zero contra o diff completo desde `baseline_revision`, triei 21 achados e apliquei 9 patches triviais — nenhuma mudança de comportamento de produto, só hardening de concorrência e fechamento de lacunas de teste.

**Arquivos alterados nesta pass:**
- `backend/handlers/admin.go` -- adiciona `SELECT pg_advisory_xact_lock($1)` como primeira instrução da transação, serializando toda mutação administrativa e eliminando por completo (não só reduzindo a janela) o risco de deadlock entre duas requisições concorrentes mirando dois admins-alvo diferentes
- `backend/handlers/admin_test.go` -- corrige dois testes que usavam um id (`"abc"`) que já falhava no guard de formato antes de alcançar a validação que diziam cobrir; adiciona `TestAdminUpdateUsuarioHandler_RejectsInvalidID` dedicado; atualiza as 3 sequências `sqlmock` para o novo advisory lock
- `backend/handlers/auth_logout_test.go` (novo) -- cobre o hardening `jwt.WithValidMethods(["HS256"])` do `LogoutHandler` (Story 1.3), até então sem nenhum teste: caminho feliz (HS256 válido é blacklistado) e rejeição de um token HMAC válido mas assinado com HS384
- `backend/scripts/bootstrap_admin.sql` -- adiciona `ativo = true` ao `SET` e `ativo` ao `RETURNING`, fechando um edge case em que o script promoveria `perfil` sem reativar um alvo já desativado, sem sinalizar isso ao operador

**Achados do review — detalhamento:**
- **high:** 0
- **medium patched (4):** dois testes de validação de `admin.go` que nunca alcançavam o código que diziam cobrir (causa raiz compartilhada, reportada por blind-hunter em 3 ângulos e por verification-gap em 1) -- corrigidos trocando o id de teste por um UUID válido e asserindo a mensagem específica de cada validação
- **low patched (5):** corrida de deadlock na proteção do último admin (3 achados convergentes de blind-hunter e intent-alignment sobre a mesma causa raiz) -- fechada com advisory lock; `LogoutHandler` sem teste do hardening JWT -- coberto; `bootstrap_admin.sql` não reativava um alvo desativado -- corrigido
- **deferred (1, medium):** nenhum teste verifica a fiação real de `RequireAuth` sobre a rota administrativa em `main.go` -- real e de impacto potencialmente alto se regredir, mas o menor fix exige extrair o registro de rotas de `main()` para uma função testável (refactor estrutural sem nenhum precedente hoje no repo, nem para as rotas das stories anteriores) -- registrado em `deferred` na frontmatter
- **rejeitados como `false` (11):** auto-alvo de admin via README (comportamento correto, não é uma restrição real); `bootstrap_admin.sql` sem checagem de linha afetada (já mitigado por `RETURNING` + saída padrão do `psql`); `GetUserIDFromContext` vazio (inatingível -- `GenerateToken` sempre grava `user_id` real, reportado 2x por camadas diferentes); falta de down-migration (premissa falsa -- nenhuma migration do repo tem rollback); remoção de `golang.org/x/crypto` (confirmada segura, zero referências remanescentes, reportada 2x); 3 divergências de leitura do Intent levantadas por intent-alignment (invariante só no handler, trilha de auditoria só gravada, bootstrap baseado em disciplina humana) -- todas correspondem à leitura literal e aprovada do Intent, nenhuma é um gap real; hardening de JWT fora do texto do Intent -- já era "fix de graça" explícito do pass anterior

**Recomendação de review de follow-up:** `false`. Só achados `medium` e `low` foram patcheados nesta pass (nenhum `high`); pela regra de pass de follow-up, isso não justifica outra rodada -- o trabalho convergiu.

**Verificação realizada:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- **executado, passou** (build OK, vet OK, `ok fb_apu05/handlers`, `ok fb_apu05/iam`, incluindo os 2 testes novos de `LogoutHandler`, o teste novo `RejectsInvalidID`, e as 3 sequências `sqlmock` atualizadas para o advisory lock)
- `go test ./... -v -run "TestAdminUpdateUsuarioHandler|TestLogoutHandler"` -- **executado, passou** (9 subtestes, incluindo os 2 corrigidos e os 2 novos)

**Riscos residuais:**
- Fiação de `RequireAuth` sobre a rota administrativa em `main.go` não tem teste (ver `deferred` acima) -- mitigação prática atual: revisão de código, já que o registro é uma única linha legível.
- Teste de integração ponta a ponta contra Postgres real segue pendente (já documentado nas sessões anteriores desta spec) -- este sandbox não tem Docker nem credenciais para o Postgres local da máquina; a lógica transacional (incluindo o novo advisory lock) foi verificada por leitura de código, não por execução real contra um banco de verdade. Recomendo rodar isso antes do merge.
