package handlers

// controle_ti.go — rotas do módulo Controle TI (migration 025):
//
//	GET  /api/me/permissoes  qualquer usuário autenticado: o que o menu mostra
//	GET  /api/ti/modelo      orçamento + lançamentos + projeção (modelo do painel)
//	POST /api/ti/importar    carrega a planilha FC FY27 Forecast (.xlsx)
//
// Acesso ao módulo: usuarios.controle_ti (interruptor em Gestão de Usuários).
// A checagem lê o banco a cada chamada — ligar/desligar o interruptor vale na
// hora, sem esperar o token expirar.

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"
	"unicode/utf8"

	"fb_apu05/internal/controleti"
)

// 25 MiB casa com o client_max_body_size do nginx (frontend/nginx.conf).
const limiteUploadPlanilha = 25 << 20

const msgSemControleTI = "acesso ao Controle TI não liberado para este usuário"

// temControleTI diz se o usuário está ativo e com o interruptor ligado.
func temControleTI(db *sql.DB, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var liberado bool
	err := db.QueryRow(`SELECT controle_ti AND ativo FROM usuarios WHERE id = $1`, userID).Scan(&liberado)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return liberado, err
}

// comControleTI barra com 403 quem não tem o interruptor ligado. Vai por
// dentro de RequireAuth (que já garantiu usuário autenticado).
func comControleTI(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		liberado, err := temControleTI(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[ControleTI] Erro ao checar permissão: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !liberado {
			jsonErr(w, http.StatusForbidden, msgSemControleTI)
			return
		}
		next(w, r)
	}
}

// PermissoesHandler — GET /api/me/permissoes. Alimenta o menu lateral.
func PermissoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		liberado, err := temControleTI(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[ControleTI] Erro ao ler permissões: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]bool{"controle_ti": liberado})
	}
}

// TIModeloHandler — GET /api/ti/modelo.
func TIModeloHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		modelo, err := controleti.Carregar(db, time.Now())
		if err != nil {
			log.Printf("[ControleTI] Erro ao montar o modelo: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(modelo)
	})
}

// TIImportarHandler — POST /api/ti/importar (multipart, campo "arquivo").
// Substitui parâmetros, orçamento e lançamentos de fonte "planilha" por
// inteiro, numa transação; lançamentos manuais ou do SAP não são tocados.
func TIImportarHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, limiteUploadPlanilha+1<<20)
		// maxMemory = limite: o arquivo fica em memória, nunca em disco.
		if err := r.ParseMultipartForm(limiteUploadPlanilha); err != nil {
			jsonErr(w, http.StatusBadRequest, "envie a planilha no campo \"arquivo\" (até 25 MB)")
			return
		}
		arquivo, cabecalho, err := r.FormFile("arquivo")
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "envie a planilha no campo \"arquivo\"")
			return
		}
		defer arquivo.Close()

		dados, err := io.ReadAll(io.LimitReader(arquivo, limiteUploadPlanilha+1))
		if err != nil || len(dados) > limiteUploadPlanilha {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o arquivo (limite de 25 MB)")
			return
		}

		planilha, err := controleti.LerPlanilha(dados)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}

		nome := filepath.Base(cabecalho.Filename)
		for utf8.RuneCountInString(nome) > 255 {
			_, tam := utf8.DecodeLastRuneInString(nome)
			nome = nome[:len(nome)-tam]
		}

		resumo, err := controleti.Importar(db, planilha, GetUserIDFromContext(r), nome)
		if err != nil {
			log.Printf("[ControleTI] Erro ao importar %q: %v", nome, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao gravar a planilha — nada foi alterado")
			return
		}
		log.Printf("[ControleTI] %s importou %q: %d linhas de orçamento, %d lançamentos",
			GetUserIDFromContext(r), nome, resumo.OrcamentoLinhas, resumo.Lancamentos)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resumo)
	})
}
