package handlers

// Testes de integração contra um Postgres REAL (migrations 001..N aplicadas). Só rodam com
// TEST_DATABASE_URL (superusuário de um banco DESCARTÁVEL) definida; sem ela são ignorados.
//   docker run -d --name pgtest -e POSTGRES_USER=teste -e POSTGRES_PASSWORD=teste -e POSTGRES_DB=teste -p 55432:5432 postgres:15-alpine
//   (aplicar migrations/*.sql em ordem) && TEST_DATABASE_URL=postgres://teste:teste@localhost:55432/teste?sslmode=disable go test ./handlers -run PG_
// Cobrem o que sqlmock não consegue: constraints, permissões por role (AD-4), CASE de reabertura, concorrência e o
// fluxo de aprovação por link de ponta a ponta.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"fb_apu05/internal/aprovacao"
	"fb_apu05/internal/exportacao"
	"fb_apu05/internal/fila"
	"fb_apu05/internal/sapodata"

	"github.com/golang-jwt/jwt/v5"

	_ "github.com/lib/pq"
)

type pgEnv struct {
	admin, app, priv *sql.DB
}

func abrirPG(t *testing.T, raw, user, pass string) *sql.DB {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" {
		u.User = url.UserPassword(user, pass)
	}
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Skipf("banco de testes indisponível para %q: %v", user, err)
	}
	return db
}

func pgSetup(t *testing.T) *pgEnv {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL não definida — teste de integração ignorado")
	}
	e := &pgEnv{
		admin: abrirPG(t, raw, "", ""),
		app:   abrirPG(t, raw, "fb_apu05_app", "changeme"),
		priv:  abrirPG(t, raw, "fb_apu05_privilegiado", "changeme"),
	}
	// trava de segurança: a limpeza abaixo APAGA dados; só roda em banco cujo nome indica que é de teste
	var nome string
	if err := e.admin.QueryRow(`SELECT current_database()`).Scan(&nome); err != nil || !strings.Contains(strings.ToLower(nome), "test") {
		t.Fatalf("recusado: o banco %q não parece ser de teste (o nome precisa conter 'test')", nome)
	}
	// banco descartável: limpa o que os testes criam (respeita as FKs)
	for _, q := range []string{
		"TRUNCATE solicitacoes, papel_pessoa CASCADE", // leva junto lançamentos, anexos, comentários, aprovações, exportações
		"DELETE FROM contas WHERE codigo LIKE 'T%' OR codigo LIKE '4%'",
		"DELETE FROM classes_imobilizado WHERE codigo LIKE 'T%'",
	} {
		if _, err := e.admin.Exec(q); err != nil {
			t.Fatalf("limpeza (%s): %v", q, err)
		}
	}
	return e
}

type base struct{ usuario, divisao, cc, conta4, conta5, classe string }

func (e *pgEnv) seed(t *testing.T) base {
	t.Helper()
	var b base
	must := func(q string, args ...any) string {
		t.Helper()
		var id string
		if err := e.admin.QueryRow(q, args...).Scan(&id); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
		return id
	}
	suf := fmt.Sprintf("%d", os.Getpid())
	b.divisao = must(`INSERT INTO divisoes (codigo, nome) VALUES ('T'||$1, 'Div teste') ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, suf)
	b.cc = must(`INSERT INTO centros_custo (codigo, nome, divisao_id, filial) VALUES ('T'||$1, 'CC teste', $2, 'F1') ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, suf, b.divisao)
	b.usuario = must(`INSERT INTO usuarios (email, nome, perfil) VALUES ('solic'||$1||'@teste.local', 'Solicitante Teste', 'solicitante') ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, suf)
	b.conta4 = must(`INSERT INTO contas (plano, codigo, nome) VALUES ('0050', 'T4'||$1, 'Despesa teste') RETURNING id`, suf)
	b.conta5 = must(`INSERT INTO contas (plano, codigo, nome) VALUES ('0050', 'T5'||$1, 'Outra teste') RETURNING id`, suf)
	b.classe = must(`INSERT INTO classes_imobilizado (codigo, nome) VALUES ('T'||$1, 'Classe teste') RETURNING id`, suf)
	return b
}

func (e *pgEnv) solicitacao(t *testing.T, b base, status string) string {
	t.Helper()
	var id string
	if err := e.app.QueryRow(`INSERT INTO solicitacoes (tipo_solicitacao, solicitante_id, centro_custo_id, status, aprovador_snapshot)
		VALUES ('transferencia', $1, $2, $3, '{}') RETURNING id`, b.usuario, b.cc, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, l := range []struct {
		lado  string
		valor float64
	}{{"origem", 60000}, {"destino", 60000}} {
		if _, err := e.app.Exec(`INSERT INTO solicitacao_lancamentos (solicitacao_id, lado, divisao_id, centro_custo_id, conta_id, mes, valor)
			VALUES ($1, $2, $3, $4, $5, '2026-12-01', $6)`, id, l.lado, b.divisao, b.cc, b.conta4, l.valor); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func statusDe(t *testing.T, db *sql.DB, id string) (string, int) {
	t.Helper()
	var s string
	var v int
	if err := db.QueryRow(`SELECT status, versao FROM solicitacoes WHERE id = $1`, id).Scan(&s, &v); err != nil {
		t.Fatal(err)
	}
	return s, v
}

// ---- constraints do banco (DW-8) ----

func TestPG_ConstraintsDosLancamentos(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aberta")
	casos := map[string]string{
		"conta E classe ao mesmo tempo": fmt.Sprintf(`INSERT INTO solicitacao_lancamentos (solicitacao_id,lado,divisao_id,centro_custo_id,conta_id,mes,classe_imobilizado_id,valor) VALUES ('%s','origem','%s','%s','%s','2026-12-01','%s',10)`, id, b.divisao, b.cc, b.conta4, b.classe),
		"nem conta nem classe":          fmt.Sprintf(`INSERT INTO solicitacao_lancamentos (solicitacao_id,lado,divisao_id,centro_custo_id,valor) VALUES ('%s','origem','%s','%s',10)`, id, b.divisao, b.cc),
		"valor zero":                    fmt.Sprintf(`INSERT INTO solicitacao_lancamentos (solicitacao_id,lado,divisao_id,centro_custo_id,conta_id,mes,valor) VALUES ('%s','origem','%s','%s','%s','2026-12-01',0)`, id, b.divisao, b.cc, b.conta4),
		"lado inválido":                 fmt.Sprintf(`INSERT INTO solicitacao_lancamentos (solicitacao_id,lado,divisao_id,centro_custo_id,conta_id,mes,valor) VALUES ('%s','meio','%s','%s','%s','2026-12-01',5)`, id, b.divisao, b.cc, b.conta4),
	}
	for nome, q := range casos {
		if _, err := e.app.Exec(q); err == nil {
			t.Errorf("%s: o banco deveria recusar", nome)
		}
	}
	if _, err := e.app.Exec(fmt.Sprintf(`INSERT INTO solicitacao_lancamentos (solicitacao_id,lado,divisao_id,centro_custo_id,classe_imobilizado_id,valor) VALUES ('%s','origem','%s','%s','%s',10)`, id, b.divisao, b.cc, b.classe)); err != nil {
		t.Errorf("imobilizado (só classe) deveria ser aceito: %v", err)
	}
}

// ---- AD-4: só a conexão privilegiada grava o status ----

func TestPG_RoleDaAplicacaoNaoAtualizaSolicitacao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aberta")
	if _, err := e.app.Exec(`UPDATE solicitacoes SET status = 'finalizada' WHERE id = $1`, id); err == nil {
		t.Fatal("a role fb_apu05_app não pode alterar solicitacoes (AD-4)")
	}
	if _, err := e.priv.Exec(`UPDATE solicitacoes SET status = 'em_atendimento', versao = versao + 1 WHERE id = $1`, id); err != nil {
		t.Fatalf("a role privilegiada deveria conseguir: %v", err)
	}
}

// ---- fila: assumir, pendência, reabertura (DW-11) e decisão ----

func TestPG_FilaAssumirPendenciaReabertura(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aberta")
	s, err := fila.Assumir(e.priv, id, 1, b.usuario)
	if err != nil || s.Status != "em_atendimento" || s.Versao != 2 {
		t.Fatalf("assumir: %+v %v", s, err)
	}
	if _, err := fila.Assumir(e.priv, id, 1, b.usuario); err == nil {
		t.Fatal("assumir com versão obsoleta deveria falhar")
	}
	if _, err := e.admin.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) SELECT $1, $2, 'x', 'x' WHERE false`, id, b.usuario); err != nil {
		t.Fatal(err)
	}
	s, err = fila.MarcarPendencia(e.priv, id, 2, b.usuario, "falta a cotação")
	if err != nil || s.Status != "pendente" {
		t.Fatalf("pendência: %+v %v", s, err)
	}
	// comentário do solicitante reabre: pendente -> em_atendimento
	s, err = fila.Comentar(e.priv, id, s.Versao, b.usuario, "segue a cotação")
	if err != nil || s.Status != "em_atendimento" {
		t.Fatalf("reabertura pelo comentário: %+v %v", s, err)
	}
	var n int
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_comentarios WHERE solicitacao_id = $1`, id).Scan(&n)
	if n != 2 {
		t.Errorf("esperava 2 comentários (pendência + resposta), há %d", n)
	}
}

func TestPG_DecidirAprovacao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aguardando_aprovacao")
	if _, err := fila.Decidir(e.priv, id, 99, true); err == nil {
		t.Fatal("versão errada deveria dar conflito")
	}
	s, err := fila.Decidir(e.priv, id, 1, true)
	if err != nil || s.Status != "aberta" || s.Versao != 2 {
		t.Fatalf("aprovar: %+v %v", s, err)
	}
	if _, err := fila.Decidir(e.priv, id, 0, false); err == nil {
		t.Fatal("já decidida: não pode decidir de novo")
	}
	id2 := e.solicitacao(t, b, "aguardando_aprovacao")
	if s, err := fila.Decidir(e.priv, id2, 0, false); err != nil || s.Status != "rejeitada" {
		t.Fatalf("rejeitar: %+v %v", s, err)
	}
	// uma solicitação já 'aberta' nunca vira 'rejeitada' por aqui
	id3 := e.solicitacao(t, b, "aberta")
	if _, err := fila.Decidir(e.priv, id3, 0, false); err == nil {
		t.Fatal("só 'aguardando_aprovacao' pode ser decidida")
	}
}

// Duas decisões simultâneas: exatamente uma vence.
func TestPG_DecidirConcorrente(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aguardando_aprovacao")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(aprova bool) {
			defer wg.Done()
			if _, err := fila.Decidir(e.priv, id, 0, aprova); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i%2 == 0)
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("exatamente uma decisão deve vencer, venceram %d", ok)
	}
}

// ---- aprovação por link, de ponta a ponta ----

func postLink(h http.HandlerFunc, token, corpo string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, reqToken("POST", token, corpo))
	return rec
}

func (e *pgEnv) convite(t *testing.T, sol, nome, expira string) string {
	t.Helper()
	tok, hash, _ := novoTokenAprovacao()
	if _, err := e.app.Exec(`INSERT INTO solicitacao_aprovacoes (solicitacao_id, token_hash, aprovador_nome, aprovador_email, canal, expira_em)
		VALUES ($1, $2, $3, $4, 'email', now() + $5::interval)`, sol, hash, nome, strings.ToLower(nome)+"@teste.local", expira); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestPG_AprovacaoPorLink_UsoUnicoEPrimeiroVale(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aguardando_aprovacao")
	tokA := e.convite(t, id, "Diretor A", "7 days")
	tokB := e.convite(t, id, "Diretor B", "7 days")
	tokExp := e.convite(t, id, "Diretor Velho", "-1 hours")

	// a consulta mostra o resumo, sem decidir
	rec := httptest.NewRecorder()
	ConsultarAprovacaoHandler(e.app)(rec, reqToken("GET", tokA, ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"estado":"pendente"`) || !strings.Contains(rec.Body.String(), "60000") {
		t.Fatalf("consulta: %d %s", rec.Code, rec.Body.String())
	}
	if s, _ := statusDe(t, e.admin, id); s != "aguardando_aprovacao" {
		t.Fatalf("consultar não pode decidir (status=%s)", s)
	}

	h := DecidirAprovacaoPorLinkHandler(e.app, e.priv)
	if r := postLink(h, tokExp, `{"decisao":"aprovada"}`); r.Code != http.StatusGone {
		t.Errorf("link expirado: esperado 410, veio %d", r.Code)
	}
	if r := postLink(h, tokA, `{"decisao":"aprovada"}`); r.Code != 200 {
		t.Fatalf("aprovar: %d %s", r.Code, r.Body.String())
	}
	if s, v := statusDe(t, e.admin, id); s != "aberta" || v != 2 {
		t.Fatalf("depois de aprovar: status=%s versao=%d", s, v)
	}
	if r := postLink(h, tokA, `{"decisao":"rejeitada","motivo":"mudei de ideia"}`); r.Code != http.StatusGone {
		t.Errorf("reuso do mesmo link: esperado 410, veio %d", r.Code)
	}
	// o outro diretor chega depois: a solicitação já foi decidida; a reserva dele é desfeita
	if r := postLink(h, tokB, `{"decisao":"rejeitada","motivo":"não concordo"}`); r.Code != http.StatusConflict {
		t.Errorf("segundo diretor: esperado 409, veio %d %s", r.Code, r.Body.String())
	}
	if s, _ := statusDe(t, e.admin, id); s != "aberta" {
		t.Errorf("a decisão do segundo não pode alterar o status (status=%s)", s)
	}
	var livre int
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_aprovacoes WHERE solicitacao_id = $1 AND decisao IS NULL AND token_hash IS NOT NULL`, id).Scan(&livre)
	if livre != 2 { // B (revertido) e o expirado nunca usado
		t.Errorf("a reserva do convite do segundo deveria ter sido desfeita (convites sem decisão: %d)", livre)
	}
	rec = httptest.NewRecorder()
	ConsultarAprovacaoHandler(e.app)(rec, reqToken("GET", tokB, ""))
	if !strings.Contains(rec.Body.String(), `"estado":"encerrada"`) {
		t.Errorf("o link do segundo deveria mostrar 'encerrada': %s", rec.Body.String())
	}
}

func TestPG_AprovacaoPorLink_Rejeicao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aguardando_aprovacao")
	tok := e.convite(t, id, "Diretor A", "7 days")
	h := DecidirAprovacaoPorLinkHandler(e.app, e.priv)
	if r := postLink(h, tok, `{"decisao":"rejeitada"}`); r.Code != 400 {
		t.Errorf("rejeitar sem motivo: esperado 400, veio %d", r.Code)
	}
	if r := postLink(h, tok, `{"decisao":"rejeitada","motivo":"fora do orçamento"}`); r.Code != 200 {
		t.Fatalf("rejeitar: %d %s", r.Code, r.Body.String())
	}
	if s, _ := statusDe(t, e.admin, id); s != "rejeitada" {
		t.Errorf("status=%s", s)
	}
	var motivo string
	_ = e.admin.QueryRow(`SELECT motivo FROM solicitacao_aprovacoes WHERE token_hash IS NOT NULL AND solicitacao_id = $1 AND decisao = 'rejeitada'`, id).Scan(&motivo)
	if motivo != "fora do orçamento" {
		t.Errorf("motivo gravado = %q", motivo)
	}
}

// Cargo -> todos os usuários do papel recebem convite (um token cada); sem ninguém no papel, nada é criado.
func TestPG_ConvidarCargoCriaUmConvitePorPessoa(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aguardando_aprovacao")
	var u1, u2 string
	for i, p := range []*string{&u1, &u2} {
		if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ($1, $2, 'solicitante')
			ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`,
			fmt.Sprintf("dir%d.%d@teste.local", i, os.Getpid()), fmt.Sprintf("Diretor %d", i)).Scan(p); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.Exec(`INSERT INTO papel_pessoa (papel, pessoa_nome, colaborador_id) VALUES ('Diretor', $1, $2)`, fmt.Sprintf("Diretor %d", i), *p); err != nil {
			t.Fatal(err)
		}
	}
	convidarAprovador(e.app, id, aprovacao.Aprovador{Tipo: aprovacao.TipoCargo, Nome: "diretor"}, "transferencia")
	var n int
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_aprovacoes WHERE solicitacao_id = $1`, id).Scan(&n)
	if n != 2 {
		t.Errorf("esperava 2 convites (papel 'diretor' sem diferenciar caixa), há %d", n)
	}
	id2 := e.solicitacao(t, b, "aguardando_aprovacao")
	convidarAprovador(e.app, id2, aprovacao.Aprovador{Tipo: aprovacao.TipoCargo, Nome: "Superintendência"}, "imobilizado")
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_aprovacoes WHERE solicitacao_id = $1`, id2).Scan(&n)
	if n != 0 {
		t.Errorf("papel sem ninguém não deve gerar convite, há %d", n)
	}
}

// Papel × pessoa aceita várias pessoas por papel, mas não repetir a mesma pessoa.
func TestPG_PapelPessoaUnicidade(t *testing.T) {
	e := pgSetup(t)
	if _, err := e.admin.Exec(`INSERT INTO papel_pessoa (papel, pessoa_nome) VALUES ('Diretor', 'Ana'), ('Diretor', 'Bia')`); err != nil {
		t.Fatalf("duas pessoas no mesmo papel deveriam ser aceitas: %v", err)
	}
	if _, err := e.admin.Exec(`INSERT INTO papel_pessoa (papel, pessoa_nome) VALUES ('diretor', 'ANA')`); err == nil {
		t.Error("mesma pessoa no mesmo papel (ignorando caixa) deveria ser recusada")
	}
}

// Opções do formulário (handler real): só contas de despesa (código começa com 4), ordenadas por código.
func TestPG_OpcoesFormularioSoContasDeDespesa(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	var id string
	if err := e.admin.QueryRow(`INSERT INTO contas (plano, codigo, nome) VALUES ($1, '4'||$2, 'Despesa real') RETURNING id`, sapodata.PlanoContas(), fmt.Sprintf("%d", os.Getpid())).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer e.admin.Exec(`DELETE FROM contas WHERE id = $1`, id)

	req := httptest.NewRequest(http.MethodGet, "/api/formulario/opcoes", nil)
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": b.usuario, "perfil": "solicitante"}))
	rec := httptest.NewRecorder()
	OpcoesFormularioHandler(e.app)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("opções: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Contas []struct{ Codigo string } `json:"contas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	achou := false
	for _, c := range out.Contas {
		if !strings.HasPrefix(c.Codigo, "4") {
			t.Errorf("conta fora do filtro de despesa: %s", c.Codigo)
		}
		if strings.HasPrefix(c.Codigo, "4"+fmt.Sprintf("%d", os.Getpid())) {
			achou = true
		}
	}
	if !achou {
		t.Error("a conta que começa com 4 deveria aparecer")
	}
}

// ---- abertura de solicitação de ponta a ponta (handler real + banco real) ----

func (e *pgEnv) abrirTransferencia(t *testing.T, b base, valor float64) (int, map[string]any) {
	t.Helper()
	corpo := corpoTransferencia(
		linhaJSON("origem", b.divisao, b.cc, b.conta4, "2026-12-01", valor) + "," +
			linhaJSON("destino", b.divisao, b.cc, b.conta5, "2026-12-01", valor),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes", strings.NewReader(corpo))
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": b.usuario, "perfil": "solicitante"}))
	rec := httptest.NewRecorder()
	AbrirSolicitacaoHandler(e.app)(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out == nil {
		out = map[string]any{"raw": rec.Body.String()}
	}
	return rec.Code, out
}

func (e *pgEnv) cfg(t *testing.T, chave, valor string) {
	t.Helper()
	if _, err := e.admin.Exec(`INSERT INTO configuracoes (chave, valor) VALUES ($1, $2) ON CONFLICT (chave) DO UPDATE SET valor = EXCLUDED.valor`, chave, valor); err != nil {
		t.Fatal(err)
	}
}

func TestPG_AbrirTransferencia_ModosDeAprovacao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	// o solicitante precisa ser dono do centro de custo
	if _, err := e.admin.Exec(`UPDATE usuarios SET cc_proprio_id = $1 WHERE id = $2`, b.cc, b.usuario); err != nil {
		t.Fatal(err)
	}
	// papéis do plano 0050 aceitos (as contas de teste foram criadas no plano da carga)
	if _, err := e.admin.Exec(`DELETE FROM alcadas WHERE centro_custo_codigo = (SELECT codigo FROM centros_custo WHERE id = $1)`, b.cc); err != nil {
		t.Fatal(err)
	}
	e.cfg(t, "aprovacao_limite_transferencia", "50000")

	// 1) padrão (aprovação NÃO obrigatória), sem alçada cadastrada: abre e vai direto para a fila
	e.cfg(t, "aprovacao_obrigatoria", "false")
	code, out := e.abrirTransferencia(t, b, 80000)
	if code != http.StatusCreated || out["status"] != "aberta" {
		t.Fatalf("sem alçada e sem obrigatoriedade: esperado 201/aberta, veio %d %v", code, out)
	}
	snap, _ := out["aprovador_snapshot"].(map[string]any)
	if snap["nome"] != "A confirmar pelo atendimento" {
		t.Errorf("aprovador deveria ser 'A confirmar pelo atendimento': %v", snap)
	}

	// 2) obrigatória e sem alçada: 422 como antes
	e.cfg(t, "aprovacao_obrigatoria", "true")
	if code, out := e.abrirTransferencia(t, b, 80000); code != http.StatusUnprocessableEntity {
		t.Fatalf("obrigatória sem alçada: esperado 422, veio %d %v", code, out)
	}

	// 3) obrigatória, com alçada: até o limite vai para a fila; acima aguarda aprovação
	if _, err := e.admin.Exec(`INSERT INTO alcadas (filial, centro_custo_codigo, valor_minimo, valor_maximo, papel_aprovador, ativo)
		SELECT 'F1', codigo, 0, NULL, 'Diretor', true FROM centros_custo WHERE id = $1`, b.cc); err != nil {
		t.Fatal(err)
	}
	if code, out := e.abrirTransferencia(t, b, 30000); code != http.StatusCreated || out["status"] != "aberta" {
		t.Fatalf("até o limite: esperado 201/aberta, veio %d %v", code, out)
	}
	code, out = e.abrirTransferencia(t, b, 80000)
	if code != http.StatusCreated || out["status"] != "aguardando_aprovacao" {
		t.Fatalf("acima do limite: esperado 201/aguardando_aprovacao, veio %d %v", code, out)
	}
	// as linhas foram gravadas atomicamente junto com a solicitação
	var n int
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_lancamentos WHERE solicitacao_id = $1`, out["id"]).Scan(&n)
	if n != 2 {
		t.Errorf("esperava 2 lançamentos, há %d", n)
	}
	e.cfg(t, "aprovacao_obrigatoria", "false")
}

// ---- atendimento: comentar em chamado alheio e finalizar com erro (feedback do Wilker) ----

func TestPG_AtendenteComentaChamadoAlheioSemMudarStatus(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	var atendente string
	if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ('atend'||$1||'@teste.local', 'Atendente', 'atendente')
		ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, fmt.Sprintf("%d", os.Getpid())).Scan(&atendente); err != nil {
		t.Fatal(err)
	}
	id := e.solicitacao(t, b, "pendente")
	// o solicitante-dono responde: reabre
	if _, err := fila.Comentar(e.priv, id, 1, atendente, "x"); err == nil {
		t.Fatal("quem não é o solicitante não deveria passar por Comentar")
	}
	s, err := fila.ComentarAtendimento(e.priv, id, 1, atendente, "preciso do anexo")
	if err != nil || s.Status != "pendente" || s.Versao != 2 {
		t.Fatalf("atendente comenta sem mudar o status: %+v %v", s, err)
	}
	if _, err := fila.ComentarAtendimento(e.priv, id, 1, atendente, "de novo"); err == nil {
		t.Fatal("versão obsoleta deveria dar conflito")
	}
	var n int
	_ = e.admin.QueryRow(`SELECT count(*) FROM solicitacao_comentarios WHERE solicitacao_id = $1 AND autor_id = $2`, id, atendente).Scan(&n)
	if n != 1 {
		t.Errorf("esperava 1 comentário do atendente, há %d", n)
	}
}

func TestPG_FinalizarComErroNaoExigeExportacao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	exp := exportacao.ExportadorVBA{}
	// sem exportação: erro pode finalizar; sucesso não
	id := e.solicitacao(t, b, "em_atendimento")
	if _, err := e.priv.Exec(`UPDATE solicitacoes SET administrador_id = $1 WHERE id = $2`, b.usuario, id); err != nil {
		t.Fatal(err)
	}
	if _, err := exp.Finalizar(e.priv, id, 1, b.usuario, "sucesso", nil); err == nil {
		t.Fatal("sucesso sem exportação deveria ser recusado")
	}
	if s, _ := statusDe(t, e.admin, id); s != "em_atendimento" {
		t.Fatalf("a recusa não pode deixar escrita parcial (status=%s)", s)
	}
	s, err := exp.Finalizar(e.priv, id, 1, b.usuario, "erro", nil)
	if err != nil || s.Status != "finalizada_erro" {
		t.Fatalf("finalizar com erro sem exportar: %+v %v", s, err)
	}
}

// Finalizar com SUCESSO sem lote: dispensado por padrão (robô SAP não ligado); com a chave ligada, volta a exigir.
func TestPG_FinalizarSucessoSemExportacao_ConformeConfiguracao(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	rodar := func(exige string) (int, string) {
		e.cfg(t, "finalizar_exige_exportacao", exige)
		id := e.solicitacao(t, b, "em_atendimento")
		if _, err := e.priv.Exec(`UPDATE solicitacoes SET administrador_id = $1 WHERE id = $2`, b.usuario, id); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/"+id+"/finalizar", strings.NewReader(`{"versao":1,"resultado":"sucesso","ordens_criadas":[]}`))
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": b.usuario, "perfil": "atendente"}))
		rec := httptest.NewRecorder()
		FinalizarSolicitacaoComConfigHandler(e.app, e.priv)(rec, req)
		s, _ := statusDe(t, e.admin, id)
		return rec.Code, s
	}
	if code, st := rodar("false"); code != 200 || st != "finalizada_sucesso" {
		t.Errorf("chave desligada: esperado 200/finalizada_sucesso, veio %d/%s", code, st)
	}
	if code, st := rodar("true"); code != http.StatusConflict || st != "em_atendimento" {
		t.Errorf("chave ligada sem lote: esperado 409 e nada gravado, veio %d/%s", code, st)
	}
}
