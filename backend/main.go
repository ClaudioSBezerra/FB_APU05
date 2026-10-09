package main

// FB_APU05 — Módulo Controladoria
//
// Scaffold inicial (Story 1.1): bootstrap HTTP + runner de migrations.
// Replica literalmente o padrão já validado em produção no projeto irmão
// FB_APU02 (ver backend/main.go de lá, função onDBConnected).

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fb_apu05/handlers"
	"fb_apu05/iam"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var (
	db      *sql.DB
	dbMutex sync.RWMutex
	dbErr   error

	// dbApp (Story 4.1, AD-4) é a conexão geral da aplicação — role
	// fb_apu05_app, não-superuser, sem GRANT UPDATE em
	// aprovador_snapshot/versao/status/administrador_id de `solicitacoes`
	// (ver migration 010). Usada por withDB, o hot-path de TODOS os
	// handlers registrados em main() exceto AssumirSolicitacaoHandler.
	dbApp      *sql.DB
	dbAppMutex sync.RWMutex

	// dbPrivileged (Story 4.1, AD-4) é a conexão privilegiada — role
	// fb_apu05_privilegiado, a ÚNICA com GRANT UPDATE nas colunas
	// protegidas. Usada exclusivamente por withPrivilegedDB, nunca pelo
	// hot-path geral.
	dbPrivileged      *sql.DB
	dbPrivilegedMutex sync.RWMutex
)

func getDB() *sql.DB {
	dbMutex.RLock()
	defer dbMutex.RUnlock()
	return db
}

func getDBApp() *sql.DB {
	dbAppMutex.RLock()
	defer dbAppMutex.RUnlock()
	return dbApp
}

func getDBPrivileged() *sql.DB {
	dbPrivilegedMutex.RLock()
	defer dbPrivilegedMutex.RUnlock()
	return dbPrivileged
}

// connectWithRetry abre uma conexão Postgres com retry (5s) até o Ping()
// funcionar — mesmo padrão de retry de initDBAsync, extraído para as duas
// conexões novas da Story 4.1 (dbApp/dbPrivileged). SetDBError também é
// chamado aqui (mesmo padrão de initDBAsync): /api/health (gate de deploy do
// AD-10) precisa refletir falha de conexão de QUALQUER uma das 3 pools, não
// só da conexão de migrations — uma rota de fila/assumir em 503 permanente
// não pode passar por "status":"ok". maxOpenConns é parametrizado para a
// soma das 3 pools (db + dbApp + dbPrivileged) ficar com folga sob o
// max_connections padrão do Postgres (100, docker-compose.yml).
func connectWithRetry(envVar, rotulo string, maxOpenConns int) *sql.DB {
	connStr := os.Getenv(envVar)
	if connStr == "" {
		log.Fatalf("%s environment variable is required", envVar)
	}

	attempt := 0
	for {
		attempt++
		conn, err := sql.Open("postgres", connStr)
		if err == nil {
			if err = conn.Ping(); err == nil {
				conn.SetMaxOpenConns(maxOpenConns)
				conn.SetMaxIdleConns(15)
				conn.SetConnMaxLifetime(15 * time.Minute)
				fmt.Printf("Successfully connected to the database (%s)!\n", rotulo)
				handlers.SetDBError(rotulo, nil)
				return conn
			}
			_ = conn.Close()
		}
		handlers.SetDBError(rotulo, fmt.Errorf("%s: attempt %d: %v", rotulo, attempt, err))
		fmt.Printf("Failed to connect to database (%s, attempt %d): %v. Retrying in 5s...\n", rotulo, attempt, err)
		time.Sleep(5 * time.Second)
	}
}

// initAppDBAsync conecta DATABASE_URL_APP (role fb_apu05_app) em background
// — mesmo motivo de initDBAsync não bloquear a subida do processo HTTP. É o
// hot-path de todos os handlers exceto AssumirSolicitacaoHandler, por isso
// mantém o mesmo tamanho de pool (50) que a conexão única tinha antes desta
// story.
func initAppDBAsync() {
	go func() {
		conn := connectWithRetry("DATABASE_URL_APP", "app role", 50)
		dbAppMutex.Lock()
		dbApp = conn
		dbAppMutex.Unlock()
	}()
}

// initPrivilegedDBAsync conecta DATABASE_URL_PRIVILEGIADA (role
// fb_apu05_privilegiado) em background — mesmo motivo de initAppDBAsync.
// Tráfego baixo (só a rota de assumir), por isso uma pool bem menor — a
// soma das 3 pools (db=5 + dbApp=50 + dbPrivileged=10) fica com folga sob o
// max_connections padrão do Postgres (100, docker-compose.yml).
func initPrivilegedDBAsync() {
	go func() {
		conn := connectWithRetry("DATABASE_URL_PRIVILEGIADA", "privileged role", 10)
		dbPrivilegedMutex.Lock()
		dbPrivileged = conn
		dbPrivilegedMutex.Unlock()
	}()
}

// initDBAsync conecta ao Postgres em background, com retry, para não travar a
// subida do processo HTTP enquanto o banco (ex. container `db`) ainda não está
// pronto. Ao conectar com sucesso, dispara o runner de migrations. Desde a
// Story 4.1, esta conexão (DATABASE_URL, role owner/superuser) só é usada no
// boot para rodar migrations — o tráfego HTTP migrou para dbApp/dbPrivileged
// — por isso uma pool pequena basta (ver initAppDBAsync/initPrivilegedDBAsync
// para a divisão do orçamento de conexões).
func initDBAsync() {
	go func() {
		var conn *sql.DB
		var err error
		connStr := os.Getenv("DATABASE_URL")
		if connStr == "" {
			log.Fatal("DATABASE_URL environment variable is required")
		}

		attempt := 0
		for {
			attempt++
			conn, err = sql.Open("postgres", connStr)
			if err == nil {
				err = conn.Ping()
				if err == nil {
					conn.SetMaxOpenConns(5)
					conn.SetMaxIdleConns(15)
					conn.SetConnMaxLifetime(15 * time.Minute)

					dbMutex.Lock()
					db = conn
					dbErr = nil
					dbMutex.Unlock()
					handlers.SetDBError("migrations", nil)

					fmt.Println("Successfully connected to the database!")
					onDBConnected()
					return
				}
				// Ping failed — close before retrying so we don't leak a connection
				// (with its own pool/sockets) on every retry during a prolonged outage.
				_ = conn.Close()
			}

			dbMutex.Lock()
			dbErr = fmt.Errorf("attempt %d: %v", attempt, err)
			dbMutex.Unlock()
			handlers.SetDBError("migrations", dbErr)

			fmt.Printf("Failed to connect to database (attempt %d): %v. Retrying in 5s...\n", attempt, err)
			time.Sleep(5 * time.Second)
		}
	}()
}

// onDBConnected é o runner de migrations: garante a tabela schema_migrations,
// lê migrations/*.sql em ordem e executa só as que ainda não constam na
// tabela. Com migrations/ vazia (estado inicial do projeto), roda sem erro e
// sem criar nenhuma tabela de domínio.
func onDBConnected() {
	database := getDB()

	migrationDir := "migrations"
	if _, err := os.Stat(migrationDir); os.IsNotExist(err) {
		if _, err := os.Stat("backend/migrations"); err == nil {
			migrationDir = "backend/migrations"
		}
	}

	fmt.Printf("Looking for migrations in: %s\n", migrationDir)
	files, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	if err != nil {
		log.Printf("Error finding migration files: %v", err)
		return
	}

	var tableExists bool
	_ = database.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='schema_migrations')`).Scan(&tableExists)

	if !tableExists {
		_, err = database.Exec(`CREATE TABLE schema_migrations (
			filename VARCHAR(255) PRIMARY KEY,
			executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`)
		if err != nil {
			log.Printf("Warning: Failed to create schema_migrations table: %v", err)
		}
	}

	if len(files) == 0 {
		log.Println("No migration files found (migrations/ vazia) — nada a executar.")
		return
	}

	for _, file := range files {
		baseName := filepath.Base(file)
		var alreadyExecuted bool
		errCheck := database.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename=$1)", baseName).Scan(&alreadyExecuted)
		if errCheck != nil {
			log.Printf("Warning: Could not check migration status for %s: %v", baseName, errCheck)
			continue
		}
		if alreadyExecuted {
			continue
		}

		fmt.Printf("Executing migration: %s\n", file)
		migration, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Could not read migration file %s: %v", file, err)
			continue
		}

		_, err = database.Exec(string(migration))
		if err != nil {
			log.Printf("Migration %s FAILED: %v", file, err)
			continue
		}

		fmt.Printf("Migration %s executed successfully.\n", file)
		_, insertErr := database.Exec("INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING", baseName)
		if insertErr != nil {
			log.Printf("Warning: Could not record migration %s: %v", baseName, insertErr)
		}
	}
}

func main() {
	_ = godotenv.Load()

	handlers.ValidateJWTSecret()
	initDBAsync()
	// Story 4.1 (AD-4): initDBAsync acima continua exclusivamente a
	// conexão do runner de migrations (DATABASE_URL, role superuser/owner —
	// REVOKE não teria efeito nela). As 2 conexões abaixo são as que os
	// handlers de fato usam.
	initAppDBAsync()
	initPrivilegedDBAsync()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	http.HandleFunc("/api/health", handlers.HealthHandler)

	// withDB adia a resolução do *sql.DB para o momento da requisição — na
	// subida do processo o banco ainda pode estar conectando (initAppDBAsync
	// roda em background com retry), então handlers que precisam de DB não
	// podem capturar um *sql.DB nulo no registro da rota. Resolve sempre a
	// conexão GERAL (DATABASE_URL_APP, role fb_apu05_app) — hot-path de
	// todos os handlers exceto AssumirSolicitacaoHandler (Story 4.1, AD-4).
	withDB := func(handlerFactory func(*sql.DB) http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			database := getDBApp()
			if database == nil {
				http.Error(w, "Database initializing, please wait...", http.StatusServiceUnavailable)
				return
			}
			handlerFactory(database)(w, r)
		}
	}

	// withPrivilegedDB (Story 4.1, AD-4) é o análogo de withDB sobre a
	// conexão PRIVILEGIADA (DATABASE_URL_PRIVILEGIADA, role
	// fb_apu05_privilegiado) — a única com GRANT UPDATE em
	// aprovador_snapshot/versao/status/administrador_id de `solicitacoes`
	// (migration 010). Usada exclusivamente por AssumirSolicitacaoHandler.
	withPrivilegedDB := func(handlerFactory func(*sql.DB) http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			database := getDBPrivileged()
			if database == nil {
				http.Error(w, "Database initializing, please wait...", http.StatusServiceUnavailable)
				return
			}
			handlerFactory(database)(w, r)
		}
	}

	// SSO Keycloak (AD-6) — único fluxo de login do FB_APU05 (sem cadastro de
	// usuário/senha separado). Config exposta em runtime (não build-time) via
	// /api/auth/sso/config, replicando o padrão já validado em produção no
	// FB_APU02 (ver spec Code Map da Story 1.2).
	http.HandleFunc("/api/auth/sso/config", handlers.SSOConfigHandler())
	http.HandleFunc("/api/auth/logout", withDB(handlers.LogoutHandler))

	// Bootstrap e proteção do administrador (Story 1.3, FR-2) — concede/
	// revoga perfil `administrador` e ativa/desativa usuários. Primeira rota
	// do FB_APU05 protegida por RequireAuth (fecha o gap deixado pela Story
	// 1.2: a tokenBlacklist nunca era lida em lugar nenhum até aqui).
	http.HandleFunc("PATCH /api/admin/usuarios/{id}", handlers.RequireAuth(withDB(handlers.AdminUpdateUsuarioHandler), "administrador"))

	// Cadastros administráveis (Story 2.1, FR-16) — 5 rotas genéricas
	// parametrizadas por {tipo} (um dos 7 cadastros mestres do Epic 2, mais
	// "cc-excecao" — exceção de centro de custo, Story 2.3, FR-4 — oitavo
	// {tipo} no mesmo registry, nenhuma rota nova), mesma implementação Go
	// para todos: carga inicial via CSV, listagem paginada, edição,
	// histórico e restauração de versão. Todas atrás de
	// RequireAuth(..., "administrador"), mesmo padrão da linha acima.
	http.HandleFunc("GET /api/admin/cadastros/{tipo}", handlers.RequireAuth(withDB(handlers.ListarCadastroHandler), "administrador"))
	http.HandleFunc("POST /api/admin/cadastros/{tipo}", handlers.RequireAuth(withDB(handlers.ImportarCadastroHandler), "administrador"))
	http.HandleFunc("PUT /api/admin/cadastros/{tipo}/{id}", handlers.RequireAuth(withDB(handlers.AtualizarCadastroHandler), "administrador"))
	http.HandleFunc("GET /api/admin/cadastros/{tipo}/{id}/historico", handlers.RequireAuth(withDB(handlers.HistoricoCadastroHandler), "administrador"))
	http.HandleFunc("POST /api/admin/cadastros/{tipo}/{id}/restaurar", handlers.RequireAuth(withDB(handlers.RestaurarCadastroHandler), "administrador"))

	// Carga de colaborador × centro de custo (Story 2.2, FR-3, AD-8) — upload
	// CSV que atualiza somente usuarios.cc_proprio_id, casando por e-mail
	// contra um usuário já provisionado via SSO. Mesma proteção das rotas
	// acima; diferente delas, reimportar é sempre permitido (nunca 409).
	http.HandleFunc("POST /api/admin/colaboradores/carga", handlers.RequireAuth(withDB(handlers.CarregarColaboradoresHandler), "administrador"))

	// Abrir solicitação (Story 3.1, FR-5/FR-10) — primeira rota do FB_APU05
	// que não exige perfil `administrador` (RequireAuth(..., "") — qualquer
	// solicitante autenticado). Só tipo_solicitacao="transferencia" tem
	// suporte nesta story; o mesmo handler será estendido pelas histórias
	// 3.2-3.5 para os outros 4 tipos.
	http.HandleFunc("POST /api/solicitacoes", handlers.RequireAuth(withDB(handlers.AbrirSolicitacaoHandler), ""))

	// Fila do administrador (Story 4.1, Epic 4) — primeiras rotas que fazem
	// UPDATE em aprovador_snapshot/versao/status/administrador_id de
	// `solicitacoes` (AD-4). ListarFilaHandler é só leitura e fica na
	// conexão geral (withDB/fb_apu05_app); AssumirSolicitacaoHandler é quem
	// precisa da conexão privilegiada (withPrivilegedDB/
	// fb_apu05_privilegiado) — a única com GRANT UPDATE nessas colunas.
	http.HandleFunc("GET /api/fila/solicitacoes", handlers.RequireAuth(withDB(handlers.ListarFilaHandler), "administrador"))
	http.HandleFunc("POST /api/solicitacoes/{id}/assumir", handlers.RequireAuth(withPrivilegedDB(handlers.AssumirSolicitacaoHandler), "administrador"))

	// Pendência e reabertura (Story 4.2, Epic 4) — 2 novas escritas em
	// status/versao de `solicitacoes`, mesma linha/lock do AD-4:
	// MarcarPendenciaHandler (administrador dono, em_atendimento->pendente) e
	// ComentarSolicitacaoHandler (qualquer perfil autenticado pode chamar;
	// só o solicitante dono tem o comentário aceito, fila.ErrNaoAutorizado ->
	// 403 para os demais — por isso RequireAuth(..., "") aqui, não
	// "administrador"). ObterSolicitacaoHandler é só leitura (withDB) —
	// expõe o histórico de solicitacao_comentarios e a versao atual a quem
	// é dono da solicitação (solicitante OU administrador).
	http.HandleFunc("POST /api/solicitacoes/{id}/pendencia", handlers.RequireAuth(withPrivilegedDB(handlers.MarcarPendenciaHandler), "administrador"))
	http.HandleFunc("POST /api/solicitacoes/{id}/comentarios", handlers.RequireAuth(withPrivilegedDB(handlers.ComentarSolicitacaoHandler), ""))
	http.HandleFunc("GET /api/solicitacoes/{id}", handlers.RequireAuth(withDB(handlers.ObterSolicitacaoHandler), ""))

	if iamBaseURL := os.Getenv("IAM_BASE_URL"); iamBaseURL != "" {
		var allowedClientIDs []string
		for _, id := range strings.Split(os.Getenv("IAM_ALLOWED_CLIENT_IDS"), ",") {
			if id = strings.TrimSpace(id); id != "" {
				allowedClientIDs = append(allowedClientIDs, id)
			}
		}
		if len(allowedClientIDs) == 0 {
			log.Printf("[Auth] AVISO: SSO Keycloak habilitado (IAM_BASE_URL=%s) mas IAM_ALLOWED_CLIENT_IDS está vazio — toda tentativa de login via Keycloak vai falhar (azp nunca bate com uma allowlist vazia)", iamBaseURL)
		}
		jwksClient := iam.NewJWKSClient(iamBaseURL, "", time.Hour, false)
		iamAuthMW := iam.IAMAuthMiddleware(jwksClient, iamBaseURL, allowedClientIDs, nil, nil)
		http.Handle("/api/auth/sso/keycloak", iamAuthMW(withDB(handlers.KeycloakSSOHandler)))
		log.Printf("[Auth] SSO Keycloak habilitado (realm: %s)", iamBaseURL)
	} else {
		log.Println("[Auth] SSO Keycloak desabilitado (IAM_BASE_URL não setada) — /api/auth/sso/keycloak não registrado.")
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      http.DefaultServeMux,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down gracefully...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}

		log.Println("Closing database connections...")
		if database := getDB(); database != nil {
			_ = database.Close()
		}
		if database := getDBApp(); database != nil {
			_ = database.Close()
		}
		if database := getDBPrivileged(); database != nil {
			_ = database.Close()
		}

		log.Println("Shutdown complete.")
	}()

	fmt.Printf("FB_APU05 backend starting on port %s...\n", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
