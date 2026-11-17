package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"fb_apu05/services"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

func reqPromocao(metodo, caminho, corpo string) *http.Request {
	req := httptest.NewRequest(metodo, caminho, strings.NewReader(corpo))
	ctx := context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": testAlvoID, "perfil": "solicitante"})
	return req.WithContext(ctx)
}

const (
	sqlPromoUsuario = "SELECT nome, email, perfil FROM usuarios WHERE id = $1 AND ativo = true"
	sqlPromoAdmins  = "SELECT email FROM usuarios WHERE perfil = 'administrador' AND ativo = true"
	sqlPromoInsert  = "INSERT INTO usuarios_promocao_pedidos"
	sqlPromoExtra   = "SELECT valor FROM configuracoes WHERE chave = $1"
)

// semExtra: o parâmetro "e-mails extras" está vazio.
func semExtra(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoExtra)).WithArgs("promocao_email_extra").
		WillReturnRows(sqlmock.NewRows([]string{"valor"}).AddRow(""))
}

// trocarEnvio substitui o envio real e devolve o que foi pedido (para as asserções).
func trocarEnvio(t *testing.T, resp func(admins []string, p services.PedidoPromocao) (int, error)) *struct {
	chamadas int
	admins   []string
	pedido   services.PedidoPromocao
} {
	t.Helper()
	orig := enviarPedidoPromocao
	t.Cleanup(func() { enviarPedidoPromocao = orig })
	reg := &struct {
		chamadas int
		admins   []string
		pedido   services.PedidoPromocao
	}{}
	enviarPedidoPromocao = func(admins []string, p services.PedidoPromocao) (int, error) {
		reg.chamadas++
		reg.admins, reg.pedido = admins, p
		return resp(admins, p)
	}
	return reg
}

func TestSolicitarPromocao_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)
	reg := trocarEnvio(t, func(a []string, _ services.PedidoPromocao) (int, error) { return len(a), nil })

	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Maria Souza", "maria@fc.com.br", "solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoAdmins)).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("adm1@fc.com.br").AddRow("adm2@fc.com.br"))
	semExtra(mock)
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoInsert)).
		WithArgs(testAlvoID, "solicitante", "Preciso atender a fila de TI", 2, 86400).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/api/me/solicitar-promocao", `{"mensagem":"Preciso atender a fila de TI"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (%s)", rec.Code, rec.Body.String())
	}
	if reg.chamadas != 1 || len(reg.admins) != 2 || reg.pedido.Email != "maria@fc.com.br" || reg.pedido.PerfilSolicitado != "atendente" {
		t.Fatalf("envio inesperado: %+v", reg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSolicitarPromocao_MensagemComHTMLEhSaneada(t *testing.T) {
	db, mock := newSQLMock(t)
	reg := trocarEnvio(t, func(a []string, _ services.PedidoPromocao) (int, error) { return len(a), nil })
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Maria", "m@fc.com.br", "solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoAdmins)).WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@fc.com.br"))
	semExtra(mock)
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoInsert)).
		WithArgs(testAlvoID, "solicitante", "scriptalert(1)/script oi", 1, 86400). // sem < >, quebra de linha vira espaço
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/x", `{"mensagem":"<script>alert(1)</script>\noi"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.ContainsAny(reg.pedido.Mensagem, "<>\n") {
		t.Fatalf("mensagem não foi saneada: %q", reg.pedido.Mensagem)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSolicitarPromocao_SoSolicitantePede(t *testing.T) {
	db, mock := newSQLMock(t)
	reg := trocarEnvio(t, func(a []string, _ services.PedidoPromocao) (int, error) { return len(a), nil })
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Ana", "a@fc.com.br", "atendente"))

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/x", `{}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, obtido %d", rec.Code)
	}
	if reg.chamadas != 0 {
		t.Fatal("não deveria enviar e-mail")
	}
}

func TestSolicitarPromocao_SemAdministradorAtivo(t *testing.T) {
	db, mock := newSQLMock(t)
	reg := trocarEnvio(t, func(a []string, _ services.PedidoPromocao) (int, error) { return len(a), nil })
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Maria", "m@fc.com.br", "solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoAdmins)).WillReturnRows(sqlmock.NewRows([]string{"email"}))
	semExtra(mock)

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/x", ``))
	if rec.Code != http.StatusConflict || reg.chamadas != 0 {
		t.Fatalf("esperado 409 sem envio, obtido %d (envios=%d)", rec.Code, reg.chamadas)
	}
}

func TestSolicitarPromocao_TravaDe24Horas(t *testing.T) {
	db, mock := newSQLMock(t)
	reg := trocarEnvio(t, func(a []string, _ services.PedidoPromocao) (int, error) { return len(a), nil })
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Maria", "m@fc.com.br", "solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoAdmins)).WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@fc.com.br"))
	semExtra(mock)
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoInsert)).WillReturnRows(sqlmock.NewRows([]string{"id"})) // já há pedido recente

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/x", `{}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("esperado 429, obtido %d", rec.Code)
	}
	if reg.chamadas != 0 {
		t.Fatal("pedido repetido não pode reenviar e-mail")
	}
}

func TestSolicitarPromocao_FalhaDeEmailDesfazOPedido(t *testing.T) {
	db, mock := newSQLMock(t)
	trocarEnvio(t, func([]string, services.PedidoPromocao) (int, error) { return 0, errors.New("smtp fora do ar") })
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoUsuario)).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "perfil"}).AddRow("Maria", "m@fc.com.br", "solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoAdmins)).WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@fc.com.br"))
	semExtra(mock)
	mock.ExpectQuery(regexp.QuoteMeta(sqlPromoInsert)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM usuarios_promocao_pedidos WHERE id = $1")).WithArgs(int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	SolicitarPromocaoHandler(db)(rec, reqPromocao(http.MethodPost, "/x", `{}`))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperado 502, obtido %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "smtp") {
		t.Fatalf("detalhe técnico vazou para o cliente: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("o pedido deveria ser desfeito: %v", err)
	}
}

func TestPromocaoStatus(t *testing.T) {
	// solicitante sem pedido: pode pedir
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil FROM usuarios WHERE id = $1 AND ativo = true")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil"}).AddRow("solicitante"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT max(created_at) FROM usuarios_promocao_pedidos")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(nil))
	rec := httptest.NewRecorder()
	PromocaoStatusHandler(db)(rec, reqPromocao(http.MethodGet, "/api/me/promocao", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pode_solicitar":true`) {
		t.Fatalf("sem pedido: %d %s", rec.Code, rec.Body.String())
	}

	// pedido há 1 hora: não pode, e informa quando poderá
	db2, mock2 := newSQLMock(t)
	mock2.ExpectQuery(regexp.QuoteMeta("SELECT perfil FROM usuarios")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil"}).AddRow("solicitante"))
	mock2.ExpectQuery(regexp.QuoteMeta("SELECT max(created_at)")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(time.Now().Add(-time.Hour)))
	rec = httptest.NewRecorder()
	PromocaoStatusHandler(db2)(rec, reqPromocao(http.MethodGet, "/api/me/promocao", ""))
	if !strings.Contains(rec.Body.String(), `"pode_solicitar":false`) || !strings.Contains(rec.Body.String(), "proximo_pedido") {
		t.Fatalf("pedido recente: %s", rec.Body.String())
	}

	// atendente: nunca pode
	db3, mock3 := newSQLMock(t)
	mock3.ExpectQuery(regexp.QuoteMeta("SELECT perfil FROM usuarios")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil"}).AddRow("atendente"))
	mock3.ExpectQuery(regexp.QuoteMeta("SELECT max(created_at)")).WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(nil))
	rec = httptest.NewRecorder()
	PromocaoStatusHandler(db3)(rec, reqPromocao(http.MethodGet, "/api/me/promocao", ""))
	if !strings.Contains(rec.Body.String(), `"pode_solicitar":false`) {
		t.Fatalf("atendente: %s", rec.Body.String())
	}
}
