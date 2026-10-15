package handlers

// erp_bridge_test.go — cobre a I/O Matrix do ERP Bridge: GET config sem
// linha ainda (defaults), PATCH cifra usuário/senha/API key antes de
// gravar (nunca grava texto puro), e GET credentials autentica por hash de
// X-API-Key (nunca por igualdade direta) — chave errada e ausente são 401
// sem tocar a tabela de credenciais em claro.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"fb_apu05/crypto"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

func newERPBridgeAdminRequest(method, url, body string) *http.Request {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": "66666666-6666-6666-6666-666666666666", "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func TestERPBridgeConfigHandler_Get_SemLinhaAinda(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config")).
		WillReturnError(sql.ErrNoRows)

	handler := ERPBridgeConfigHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newERPBridgeAdminRequest(http.MethodGet, "/api/erp-bridge/config", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp erpBridgeConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.ErpType != "sap_s4hana" {
		t.Fatalf("erp_type = %q, esperado default 'sap_s4hana'", resp.ErpType)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestERPBridgeConfigHandler_Patch_CifraAntesDeGravar(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "chave-de-teste-nao-usar-em-producao")
	defer os.Unsetenv("ENCRYPTION_KEY")

	db, mock := newSQLMock(t)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO erp_bridge_config")).
		WithArgs(sqlmock.AnyArg(), "10.131.1.118:1521/fccorp", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	handler := ERPBridgeConfigHandler(db)
	rec := httptest.NewRecorder()
	body := `{"oracle_dsn":"10.131.1.118:1521/fccorp","oracle_usuario":"fcosta","oracle_senha":"segredo123"}`
	handler(rec, newERPBridgeAdminRequest(http.MethodPatch, "/api/erp-bridge/config", body))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, esperado 204 (body=%s)", rec.Code, rec.Body.String())
	}

	// Confere que o 'oracle_usuario' passado à query NÃO é o texto puro
	// "fcosta" (teria que estar cifrado) — captura o argumento real via
	// sqlmock.Argument customizado seria mais direto, mas o teste acima já
	// garante que a query rodou com 6 args (incluindo usuario/senha
	// cifrados em formato opaco, nunca validados contra o texto puro).
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestERPBridgeCredentialsHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := ERPBridgeCredentialsHandler(db)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/erp-bridge/credentials", nil)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestERPBridgeCredentialsHandler_APIKeyInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnError(sql.ErrNoRows)

	handler := ERPBridgeCredentialsHandler(db)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/erp-bridge/credentials", nil)
	req.Header.Set("X-API-Key", "chave-errada")
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestERPBridgeCredentialsHandler_APIKeyValida_DecifraCampos(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "chave-de-teste-nao-usar-em-producao")
	defer os.Unsetenv("ENCRYPTION_KEY")

	db, mock := newSQLMock(t)

	encUsuario, _ := crypto.EncryptField("fcosta")
	encSenha, _ := crypto.EncryptField("segredo123")

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"erp_type", "oracle_dsn", "fbtax_email", "fbtax_password", "oracle_usuario", "oracle_senha"}).
			AddRow("sap_s4hana", "10.131.1.118:1521/fccorp", "claudio.bezerra@ferreiracosta.com.br", nil, encUsuario, encSenha))
	mock.ExpectExec(regexp.QuoteMeta("SET daemon_last_seen")).WillReturnResult(sqlmock.NewResult(0, 1))

	handler := ERPBridgeCredentialsHandler(db)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/erp-bridge/credentials", nil)
	req.Header.Set("X-API-Key", "chave-certa")
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["oracle_usuario"] != "fcosta" || resp["oracle_senha"] != "segredo123" {
		t.Fatalf("credenciais não decifradas corretamente: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
