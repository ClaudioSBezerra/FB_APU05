package handlers

// erp_bridge.go — credenciais para o daemon ERP Bridge externo (réplica,
// para o FB_APU05, do mecanismo já validado em produção no FB_APU02:
// backend/handlers/erp_bridge.go de lá). FB_APU05 nunca se conecta ao
// Oracle/SAP diretamente: só guarda as credenciais cifradas (AES-256-GCM,
// backend/crypto) e as serve ao daemon via API key. O daemon é quem de
// fato consulta VW_COLABORADORES no servidor FCCORP e, com essas
// credenciais, importa colaborador × centro de custo (hoje carregado
// manualmente via POST /api/admin/colaboradores/carga, Story 2.2).
//
// Diferente do FB_APU02 (multi-tenant, `erp_bridge_config` por
// company_id), aqui a tabela é singleton (AD-7, single-company) — sem
// nenhum conceito de empresa para resolver por requisição.
//
// GET/PATCH /api/erp-bridge/config e POST /api/erp-bridge/config/generate-api-key
// exigem perfil administrador (RequireAuth) — mais estrito que o FB_APU02
// (lá, qualquer autenticado acessa; aqui replicamos o texto da própria tela
// "Visível apenas para administradores" também na trava do servidor, não só
// na UI). GET /api/erp-bridge/credentials é o único endpoint público
// (autenticado via header X-API-Key, não JWT) — é o que o daemon chama.

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"

	"fb_apu05/crypto"
)

type erpBridgeConfigResponse struct {
	ErpType          string `json:"erp_type"`
	OracleDsn        string `json:"oracle_dsn"`
	FBTaxEmail       string `json:"fbtax_email"`
	FBTaxPasswordSet bool   `json:"fbtax_password_set"`
	OracleUsuario    string `json:"oracle_usuario"`
	OracleSenhaSet   bool   `json:"oracle_senha_set"`
	APIKey           string `json:"api_key"`
}

// ERPBridgeConfigHandler — GET/PATCH /api/erp-bridge/config.
func ERPBridgeConfigHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			erpBridgeHandleGet(w, db)
		case http.MethodPatch:
			erpBridgeHandlePatch(w, r, db)
		default:
			w.Header().Set("Allow", "GET, PATCH")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func erpBridgeHandleGet(w http.ResponseWriter, db *sql.DB) {
	var resp erpBridgeConfigResponse
	var erpType, oracleDsn, fbtaxEmail, fbtaxPassword, oracleUsuario, oracleSenha, apiKey sql.NullString

	err := db.QueryRow(`
		SELECT erp_type, oracle_dsn, fbtax_email, fbtax_password, oracle_usuario, oracle_senha, api_key
		FROM erp_bridge_config WHERE id = true
	`).Scan(&erpType, &oracleDsn, &fbtaxEmail, &fbtaxPassword, &oracleUsuario, &oracleSenha, &apiKey)

	if err == sql.ErrNoRows {
		resp.ErpType = "sap_s4hana"
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	if err != nil {
		log.Printf("[ERPBridge] Erro ao consultar configuração: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	if erpType.Valid {
		resp.ErpType = erpType.String
	} else {
		resp.ErpType = "sap_s4hana"
	}
	if oracleDsn.Valid {
		resp.OracleDsn = oracleDsn.String
	}
	if fbtaxEmail.Valid {
		resp.FBTaxEmail = fbtaxEmail.String
	}
	resp.FBTaxPasswordSet = fbtaxPassword.Valid && fbtaxPassword.String != ""
	if oracleUsuario.Valid && oracleUsuario.String != "" {
		decrypted, decErr := crypto.DecryptField(oracleUsuario.String)
		if decErr != nil {
			log.Printf("[ERPBridge] Erro ao decifrar oracle_usuario: %v", decErr)
		} else {
			resp.OracleUsuario = decrypted
		}
	}
	resp.OracleSenhaSet = oracleSenha.Valid && oracleSenha.String != ""
	if apiKey.Valid && apiKey.String != "" {
		decrypted, decErr := crypto.DecryptField(apiKey.String)
		if decErr != nil {
			log.Printf("[ERPBridge] Erro ao decifrar api_key: %v", decErr)
		} else {
			resp.APIKey = decrypted
		}
	}

	_ = json.NewEncoder(w).Encode(resp)
}

type erpBridgePatchRequest struct {
	ErpType       *string `json:"erp_type"`
	OracleDsn     *string `json:"oracle_dsn"`
	FBTaxEmail    *string `json:"fbtax_email"`
	FBTaxPassword *string `json:"fbtax_password"`
	OracleUsuario *string `json:"oracle_usuario"`
	OracleSenha   *string `json:"oracle_senha"`
}

func erpBridgeHandlePatch(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req erpBridgePatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	if req.ErpType != nil && *req.ErpType != "oracle_xml" && *req.ErpType != "sap_s4hana" {
		jsonErr(w, http.StatusBadRequest, "campo 'erp_type' deve ser 'oracle_xml' ou 'sap_s4hana'")
		return
	}

	var encFBTaxPassword, encOracleUsuario, encOracleSenha sql.NullString
	if req.FBTaxPassword != nil && *req.FBTaxPassword != "" {
		enc, err := crypto.EncryptField(*req.FBTaxPassword)
		if err != nil {
			log.Printf("[ERPBridge] Erro ao cifrar fbtax_password: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		encFBTaxPassword = sql.NullString{String: enc, Valid: true}
	}
	if req.OracleUsuario != nil && *req.OracleUsuario != "" {
		enc, err := crypto.EncryptField(*req.OracleUsuario)
		if err != nil {
			log.Printf("[ERPBridge] Erro ao cifrar oracle_usuario: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		encOracleUsuario = sql.NullString{String: enc, Valid: true}
	}
	if req.OracleSenha != nil && *req.OracleSenha != "" {
		enc, err := crypto.EncryptField(*req.OracleSenha)
		if err != nil {
			log.Printf("[ERPBridge] Erro ao cifrar oracle_senha: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		encOracleSenha = sql.NullString{String: enc, Valid: true}
	}

	_, err := db.Exec(`
		INSERT INTO erp_bridge_config (id, erp_type, oracle_dsn, fbtax_email, fbtax_password, oracle_usuario, oracle_senha, updated_at)
		VALUES (true, COALESCE($1, 'sap_s4hana'), $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE SET
		    erp_type       = COALESCE($1, erp_bridge_config.erp_type),
		    oracle_dsn     = COALESCE($2, erp_bridge_config.oracle_dsn),
		    fbtax_email    = COALESCE($3, erp_bridge_config.fbtax_email),
		    fbtax_password = COALESCE($4, erp_bridge_config.fbtax_password),
		    oracle_usuario = COALESCE($5, erp_bridge_config.oracle_usuario),
		    oracle_senha   = COALESCE($6, erp_bridge_config.oracle_senha),
		    updated_at     = now()
	`, req.ErpType, req.OracleDsn, req.FBTaxEmail, encFBTaxPassword, encOracleUsuario, encOracleSenha)
	if err != nil {
		log.Printf("[ERPBridge] Erro ao salvar configuração: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ERPBridgeGenerateAPIKeyHandler — POST /api/erp-bridge/config/generate-api-key.
// Gera 32 bytes aleatórios (chave hex de 64 caracteres), grava cifrada (para
// reexibir no painel) + seu hash SHA-256 (para lookup O(1) em
// ERPBridgeCredentialsHandler) e devolve a chave em claro — único momento em
// que ela existe em claro fora do cliente que a gerou.
func ERPBridgeGenerateAPIKeyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			log.Printf("[ERPBridge] Erro ao gerar API key: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		key := hex.EncodeToString(raw)
		hash := sha256.Sum256([]byte(key))
		hashHex := hex.EncodeToString(hash[:])

		enc, err := crypto.EncryptField(key)
		if err != nil {
			log.Printf("[ERPBridge] Erro ao cifrar API key: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		_, err = db.Exec(`
			INSERT INTO erp_bridge_config (id, api_key, api_key_hash, updated_at)
			VALUES (true, $1, $2, now())
			ON CONFLICT (id) DO UPDATE SET api_key = $1, api_key_hash = $2, updated_at = now()
		`, enc, hashHex)
		if err != nil {
			log.Printf("[ERPBridge] Erro ao salvar API key: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"api_key": key})
	}
}

// ERPBridgeCredentialsHandler — GET /api/erp-bridge/credentials. Único
// endpoint do ERP Bridge sem RequireAuth (JWT) — autenticado via header
// X-API-Key, comparado por hash contra api_key_hash (nunca por igualdade
// direta de texto, mesmo padrão de verificação por hash já usado em
// LoginHandler para senha). Usado exclusivamente pelo daemon externo.
func ERPBridgeCredentialsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			jsonErr(w, http.StatusUnauthorized, "X-API-Key obrigatório")
			return
		}
		hash := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(hash[:])

		var erpType, oracleDsn, fbtaxEmail, fbtaxPassword, oracleUsuario, oracleSenha sql.NullString
		err := db.QueryRow(`
			SELECT erp_type, oracle_dsn, fbtax_email, fbtax_password, oracle_usuario, oracle_senha
			FROM erp_bridge_config WHERE api_key_hash = $1
		`, hashHex).Scan(&erpType, &oracleDsn, &fbtaxEmail, &fbtaxPassword, &oracleUsuario, &oracleSenha)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusUnauthorized, "API key inválida")
			return
		}
		if err != nil {
			log.Printf("[ERPBridge] Erro ao consultar credenciais: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		result := map[string]string{
			"erp_type":       "sap_s4hana",
			"oracle_dsn":     "",
			"fbtax_email":    "",
			"fbtax_password": "",
			"oracle_usuario": "",
			"oracle_senha":   "",
		}
		if erpType.Valid {
			result["erp_type"] = erpType.String
		}
		if oracleDsn.Valid {
			result["oracle_dsn"] = oracleDsn.String
		}
		if fbtaxEmail.Valid {
			result["fbtax_email"] = fbtaxEmail.String
		}
		if v, ok := decryptIfPresent(fbtaxPassword); ok {
			result["fbtax_password"] = v
		}
		if v, ok := decryptIfPresent(oracleUsuario); ok {
			result["oracle_usuario"] = v
		}
		if v, ok := decryptIfPresent(oracleSenha); ok {
			result["oracle_senha"] = v
		}

		if _, err := db.Exec(`UPDATE erp_bridge_config SET daemon_last_seen = now() WHERE api_key_hash = $1`, hashHex); err != nil {
			log.Printf("[ERPBridge] Erro ao atualizar daemon_last_seen: %v", err)
		}

		_ = json.NewEncoder(w).Encode(result)
	}
}

// decryptIfPresent decifra v se ele tiver um valor não vazio — devolve
// ("", false) para NULL/vazio e loga (sem abortar a resposta) se a
// decifragem falhar, já que um único campo corrompido não deveria derrubar
// as demais credenciais que o daemon ainda pode usar.
func decryptIfPresent(v sql.NullString) (string, bool) {
	if !v.Valid || v.String == "" {
		return "", false
	}
	decrypted, err := crypto.DecryptField(v.String)
	if err != nil {
		log.Printf("[ERPBridge] Erro ao decifrar campo: %v", err)
		return "", false
	}
	return decrypted, true
}
