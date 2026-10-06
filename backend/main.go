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
	"sync"
	"syscall"
	"time"

	"fb_apu05/handlers"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var (
	db      *sql.DB
	dbMutex sync.RWMutex
	dbErr   error
)

func getDB() *sql.DB {
	dbMutex.RLock()
	defer dbMutex.RUnlock()
	return db
}

// initDBAsync conecta ao Postgres em background, com retry, para não travar a
// subida do processo HTTP enquanto o banco (ex. container `db`) ainda não está
// pronto. Ao conectar com sucesso, dispara o runner de migrations.
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
					conn.SetMaxOpenConns(50)
					conn.SetMaxIdleConns(15)
					conn.SetConnMaxLifetime(15 * time.Minute)

					dbMutex.Lock()
					db = conn
					dbErr = nil
					dbMutex.Unlock()
					handlers.SetDBError(nil)

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
			handlers.SetDBError(dbErr)

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

	initDBAsync()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	http.HandleFunc("/api/health", handlers.HealthHandler)

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

		if database := getDB(); database != nil {
			log.Println("Closing database connections...")
			_ = database.Close()
		}

		log.Println("Shutdown complete.")
	}()

	fmt.Printf("FB_APU05 backend starting on port %s...\n", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
