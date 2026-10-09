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

### DW-1: Nenhum teste verifica que PATCH /api/admin/usuarios/{id} está de fato registrado atrás de RequireAuth(..., "administrador") em main.go.
origin: spec-deferred 2bacedfbdeca
location: backend/main.go:202
source_spec: `spec-1-3-bootstrap-e-protecao-do-administrador.md`
severity: medium
reason: Busca por TestMain/httptest.NewServer em backend só encontra uso em iam/iam_auth_middleware_test.go; nada cobre o roteamento de main.go. Uma regressão que remova o wrapper RequireAuth ou troque o perfilExigido não derrubaria nenhum teste existente. O menor fix exigiria extrair o registro de rotas de main() para uma função testável — refactor estrutural sem precedente hoje no repo (nem a rota de logout da Story 1.2 tem teste de fiação).
status: open

### DW-2: CSV de números (valor_minimo/valor_maximo de alcadas) pode rejeitar formato decimal pt-BR (vírgula) se os dados reais de origem usarem esse formato.
origin: spec-deferred b1f47a0cb794
location: backend/handlers/cadastros_csv.go:91-112
source_spec: `spec-2-1-carga-inicial-e-cadastros-administraveis.md`
reason: parseNumeroObrigatorio/parseNumeroOpcional (cadastros_csv.go) usam strconv.ParseFloat diretamente sobre o texto do CSV, sem normalizar vírgula para ponto; um valor como "100,50" falha a conversão. O formato decimal real usado nos CSVs de origem (ERP) não está neste repositório para confirmar (mesma limitação já registrada em Design Notes sobre os dados reais de origem). Para resolver: obter uma amostra real do CSV de alcadas e confirmar se usa vírgula ou ponto como separador decimal antes da carga real em produção.
status: open

### DW-3: A rejeição de valor<=0 em validarLancamentosEstrutura (compartilhada entre transferencia e inclusao_sfc) não tem nenhum teste no caminho transferencia — só o caminho inclusao_sfc ganhou cobertura
origin: spec-deferred bdc299b3b6a5
location: backend/handlers/solicitacoes.go:325 (validarLancamentosEstrutura)
source_spec: `spec-3-2-abrir-inclusao-sfc-com-aprovacao-calculada.md`
severity: medium
reason: Confirmado por busca no repo: nenhum teste em solicitacoes_test.go (nem os de Story 3.1/transferencia) exercitava valor<=0 antes desta story; a cobertura adicionada aqui (TestAbrirSolicitacaoHandler_InclusaoSFC_ValorInvalido) cobre apenas o caminho inclusao_sfc. Gap pré-existente da Story 3.1, não introduzido por esta mudança. Se a regra compartilhada regredir, nada detecta isso no fluxo principal de Transferência.
status: open

### DW-4: Nenhuma validação impede 2 linhas ativas com faixas de valor sobrepostas para o mesmo centro_custo_codigo+colaborador_id em autorizadores_formulario.
origin: spec-deferred 5e9c69a78334
location: backend/handlers/cadastros.go (decodeJSONAutorizadoresFormulario) / backend/handlers/cadastros_csv.go (decodeCSVAutorizadoresFormulario)
source_spec: `spec-3-3-abrir-inclusao-com-autorizador-nominal.md`
severity: medium
reason: Confirmado por leitura de decodeJSONAutorizadoresFormulario/ decodeCSVAutorizadoresFormulario: nenhuma checagem de sobreposição existe. Mesmo padrão de alcadas (migration 003), que também não valida sobreposição no decode; só regras_aprovacao tem um desempate explícito (precedencia), e mesmo essa não valida sobreposição na carga. Não introduzido por esta story.
status: open

### DW-5: AutorizadorNominal.Resolve nunca checa se o colaborador_id escolhido ainda é um usuarios.ativo=true.
origin: spec-deferred 2433eafbed3b
location: backend/internal/aprovacao/nominal.go:38-56 (AutorizadorNominal.Resolve)
source_spec: `spec-3-3-abrir-inclusao-com-autorizador-nominal.md`
severity: medium
reason: Confirmado: a query em nominal.go só filtra autorizadores_formulario.ativo, nunca usuarios.ativo. Lacuna sistêmica do pacote aprovacao — nenhum resolver existente (alcada, gerente, regraCurada) checa usuarios.ativo hoje. Não introduzido por esta story.
status: open

### DW-6: TestResolverParaTipo só assevera "imobilizado" como não suportado, nunca "obras".
origin: spec-deferred 209614bb742f
location: backend/internal/aprovacao/calculado_test.go (TestResolverParaTipo)
source_spec: `spec-3-3-abrir-inclusao-com-autorizador-nominal.md`
severity: low
reason: Confirmado por leitura de calculado_test.go. A asserção de "imobilizado" é da Story 3.1; esta story só adicionou as asserções de "inclusao" sem tocar essa linha — não introduzido por 3.3. Ambos os valores caem no mesmo branch default/ErrTipoNaoSuportado em ResolverParaTipo, então testar um é representativo do outro.
status: open

### DW-7: TestAbrirSolicitacaoHandler_Inclusao_Success nunca assevera que regra_id/regra_versao/motivo aparecem no corpo da resposta HTTP.
origin: spec-deferred 00f8937bc1f0
location: backend/handlers/solicitacoes_test.go (TestAbrirSolicitacaoHandler_Inclusao_Success)
source_spec: `spec-3-3-abrir-inclusao-com-autorizador-nominal.md`
severity: low
reason: Confirmado por leitura do teste — só tipo/nome do aprovador são checados no body. Mesmo padrão em todos os testes de sucesso do arquivo, para todos os tipos (transferencia/inclusao_sfc/inclusao); não introduzido por esta story.
status: open

### DW-8: Nenhum teste exercita o CHECK `chk_lancamento_conta_xor_classe` (nem qualquer outra constraint da migration 008) contra um Postgres real — toda a cobertura é via sqlmock.
origin: spec-deferred 4d7ca531ea7b
location: backend/migrations/008_solicitacao_anexos_e_classe_imobilizado.sql (chk_lancamento_conta_xor_classe)
source_spec: `spec-3-4-abrir-imobilizado-com-anexo-de-cotacao.md`
severity: low
reason: Confirmado pela camada verification-gap: grep no repositório mostra que nenhuma migration (001-007 inclusive, já existentes antes desta story) é testada contra um Postgres real em nenhum lugar da suite; go test ./... só usa sqlmock. Mesmo padrão sistêmico pré-existente, não introduzido por esta story — só a constraint em si (nova) herda a lacuna já presente em todas as migrations anteriores.
status: open

### DW-9: `locais_obra`/`subgrupos_despesa` resolvem `codigo` case-insensitive (`UPPER(codigo)`) contra uma constraint UNIQUE case-sensitive, então duas linhas cujo código difere só na caixa produziriam
origin: spec-deferred 5ea04dac0952
location: backend/handlers/cadastros_csv.go (resolveLocalObraIDPorCodigo/resolveSubgrupoDespesaIDPorCodigo)
source_spec: `spec-3-5-abrir-obras-multi-linha.md`
severity: low
reason: Confirmado por leitura (`resolveLocalObraIDPorCodigo`/ `resolveSubgrupoDespesaIDPorCodigo`, cadastros_csv.go), mas é o MESMO padrão já existente em `centros_custo`/`resolveCentroCustoIDPorCodigo` (migration 003, Story 2.1), fielmente replicado aqui — não introduzido por esta story.
status: open

### DW-10: Nenhuma checagem de positividade em `aprovadores_obra.teto` no decode CSV/JSON.
origin: spec-deferred 6c061d679c5f
location: backend/handlers/cadastros.go (decodeJSONAprovadoresObra), backend/handlers/cadastros_csv.go (decodeCSVAprovadoresObra)
source_spec: `spec-3-5-abrir-obras-multi-linha.md`
severity: low
reason: Confirmado por leitura, mas nenhum campo numérico em todo o registry de cadastros (`valor_minimo`/`valor_maximo`/`teto` de `gerentes_aprovacao`, etc.) tem essa checagem — convenção sistêmica já estabelecida, não uma lacuna específica desta story.
status: open

### DW-11: Os testes de internal/fila.Comentar/MarcarPendencia usam sqlmock, que nunca executa o SQL real contra um Postgres — o CASE de reabertura (o mecanismo central desta story) nunca é verificado contra um
origin: spec-deferred 089ac1442a6f
location: backend/internal/fila/fila.go (Comentar, MarcarPendencia)
source_spec: `spec-4-2-pendencia-e-reabertura.md`
severity: medium
reason: mock.ExpectQuery(...).WillReturnRows(...) fixa o resultado pós-transição independente do texto SQL executado; um CASE invertido (ex. fechar ativas em vez de reabrir encerradas) passaria TestComentar_ReabreDePendente, TestComentar_StatusJaAtivoPermanece e o teste de handler equivalente sem detecção. Resolver isso exige o primeiro arcabouço de teste de integração com Postgres real do repositório (nenhuma feature/migration tem isso hoje) — mesma categoria já deferida no triage log da Story 4.1 para a ACL real do AD-4. SQL conferido manualmente por leitura nesta passada: válido e correto.
status: open

### DW-12: GerarLoteObra não deduplica solicitacao_ids repetidos no mesmo pedido — uma solicitação repetida é lida/validada 2x e entra 2x em linhas/ solicitacoes_incluidas (duplicando linhas no .xlsm e no array
origin: spec-deferred 53d511f334ed
location: backend/internal/exportacao/obra.go (GerarLoteObra)
source_spec: `spec-4-4-gerar-lote-obra.md`
severity: medium
reason: Mesmo formato de loop sem deduplicação já existe, idêntico, em GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3, já revisada e aceita) — não é um risco novo introduzido por esta story, mesma categoria pré-existente.
status: open

### DW-13: Nenhuma re-checagem de status/dono sob lock entre a validação por solicitação (fora de transação) e a transação de escrita de gravarLoteObra (janela TOCTOU).
origin: spec-deferred da88e5062ea9
location: backend/internal/exportacao/obra.go (GerarLoteObra)
source_spec: `spec-4-4-gerar-lote-obra.md`
severity: medium
reason: GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3) tem exatamente a mesma forma — valida fora da transação, grava em transação separada — não é um risco novo desta story.
status: open

### DW-14: GerarLoteObra faz 2 round-trips sequenciais ao banco por solicitacao_id, sem lote/paginação nem teto explícito (só o limite indireto de 1 MB do corpo da requisição).
origin: spec-deferred ec3c5b5907dc
location: backend/internal/exportacao/obra.go (GerarLoteObra)
source_spec: `spec-4-4-gerar-lote-obra.md`
severity: medium
reason: GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3) tem exatamente o mesmo formato de loop — não é um risco novo desta story.
status: open

### DW-15: lerFinalizarRequest não tem teste para corpo malformado, erro de io.ReadAll, ou overflow do MaxBytesReader de 1 MiB.
origin: spec-deferred 6b48adf9d851
location: backend/handlers/finalizar.go (lerFinalizarRequest)
source_spec: `spec-4-5-finalizar-solicitacao.md`
severity: low
reason: Mesmo padrão já presente, sem teste, em TODOS os demais lerXRequest do pacote handlers (lerPendenciaOuComentarioRequest, lerGerarLoteDespesaRequest, lerGerarLoteObraRequest — confirmado por grep, nenhum testa corpo malformado); não é um risco novo desta story.
status: open

### DW-16: ExportadorVBA.Finalizar nunca propaga o context.Context da requisição HTTP para a transação (sem BeginTx(ctx,...)/...Context) — uma requisição cancelada/com timeout no cliente não cancela a transação
origin: spec-deferred 0cb0d913d23a
location: backend/internal/exportacao/finalizar.go (Finalizar)
source_spec: `spec-4-5-finalizar-solicitacao.md`
severity: medium
reason: Mesmo padrão em TODAS as demais escritas do repositório (fila.Assumir/MarcarPendencia/Comentar, exportacao.GerarLoteDespesa/GerarLoteObra) — nenhuma usa BeginTx(ctx,...)/ExecContext; convenção já estabelecida em todo o backend, não um risco novo desta story.
status: open

### DW-17: Nenhum teste no repositório (antigo ou novo) exercita RequireAuth(next, "") com um perfil diferente de "administrador" — o comportamento "rota aberta a qualquer perfil autenticado" que esta story
origin: spec-deferred 7d011ed179d8
location: backend/handlers/middleware_test.go
source_spec: `spec-5-1-visualizar-painel-consolidado-de-solicitacoes.md`
severity: low
reason: `painel_test.go` chama ObterPainelHandler diretamente, sem passar pelo middleware; `middleware_test.go` só cobre perfilExigido não vazio ("administrador"). A lógica foi verificada por leitura (middleware.go: `if perfilExigido != ""`) e está correta — mas o caminho perfilExigido="" já existia antes desta story (usado por POST /api/solicitacoes) e middleware.go não foi tocado neste diff, então a lacuna de teste é anterior a esta story, não introduzida por ela.
status: open
