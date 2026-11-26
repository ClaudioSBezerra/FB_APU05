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
	"time"

	"fb_apu05/internal/sla"

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
		"TRUNCATE grupos CASCADE",
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
	if n != 4 { // assumiu (evento) + pendência + resposta + reabertura (evento)
		t.Errorf("esperava 4 registros no histórico, há %d", n)
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
	if err := e.admin.QueryRow(`INSERT INTO contas (plano, codigo, nome) VALUES ($1, '4221'||$2, 'Despesa real') RETURNING id`, sapodata.PlanoContas(), fmt.Sprintf("%d", os.Getpid())).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer e.admin.Exec(`DELETE FROM contas WHERE id = $1`, id)
	var idFora string
	if err := e.admin.QueryRow(`INSERT INTO contas (plano, codigo, nome) VALUES ($1, '4251'||$2, 'Despesa financeira') RETURNING id`, sapodata.PlanoContas(), fmt.Sprintf("%d", os.Getpid())).Scan(&idFora); err != nil {
		t.Fatal(err)
	}
	defer e.admin.Exec(`DELETE FROM contas WHERE id = $1`, idFora)

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
		if !strings.HasPrefix(c.Codigo, "411") && !strings.HasPrefix(c.Codigo, "421") && !strings.HasPrefix(c.Codigo, "422") {
			t.Errorf("conta fora da faixa de despesa (411/421/422): %s", c.Codigo)
		}
		if strings.HasPrefix(c.Codigo, "4221"+fmt.Sprintf("%d", os.Getpid())) {
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

// Ano do orçamento de ponta a ponta: criada em janeiro com o ano anterior, a solicitação segue o fluxo
// (consulta, assumir) com o ano gravado mesmo depois da data-limite.
func TestPG_AnoDoOrcamento_JaneiroEDepois(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	if _, err := e.admin.Exec(`UPDATE usuarios SET cc_proprio_id = $1 WHERE id = $2`, b.cc, b.usuario); err != nil {
		t.Fatal(err)
	}
	e.cfg(t, "aprovacao_obrigatoria", "false")
	e.cfg(t, "orcamento_ano_anterior_ate", "01-31")

	abrir := func(mes string) (int, map[string]any) {
		corpo := corpoTransferencia(
			linhaJSON("origem", b.divisao, b.cc, b.conta4, mes, 1000) + "," + linhaJSON("destino", b.divisao, b.cc, b.conta5, mes, 1000))
		req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes", strings.NewReader(corpo))
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": b.usuario, "perfil": "solicitante"}))
		rec := httptest.NewRecorder()
		AbrirSolicitacaoHandler(e.app)(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	comRelogio(t, recife(2027, time.January, 15, 9, 0, 0))
	code, out := abrir("2026-12-01")
	if code != http.StatusCreated {
		t.Fatalf("janeiro com o ano anterior deveria abrir: %d %v", code, out)
	}
	id := out["id"].(string)
	if code, _ := abrir("2027-02-01"); code != http.StatusCreated {
		t.Errorf("janeiro com o ano corrente deveria abrir: %d", code)
	}

	// depois da data-limite: abrir com 2026 é recusado, mas a solicitação já criada continua seguindo o fluxo
	comRelogio(t, recife(2027, time.February, 10, 12, 0, 0))
	if code, out := abrir("2026-12-01"); code != http.StatusBadRequest {
		t.Errorf("fevereiro com 2026 deveria ser recusado: %d %v", code, out)
	}
	var ano int
	if err := e.admin.QueryRow(`SELECT EXTRACT(YEAR FROM mes)::int FROM solicitacao_lancamentos WHERE solicitacao_id = $1 LIMIT 1`, id).Scan(&ano); err != nil || ano != 2026 {
		t.Fatalf("o ano gravado deveria continuar 2026: %d %v", ano, err)
	}
	if s, err := fila.Assumir(e.priv, id, 1, b.usuario); err != nil || s.Status != "em_atendimento" {
		t.Fatalf("a solicitação de janeiro deveria seguir para o atendimento em fevereiro: %+v %v", s, err)
	}

	// o parâmetro vale sem deploy: estendendo a data-limite para 28/02, 2026 volta a ser aceito
	e.cfg(t, "orcamento_ano_anterior_ate", "02-28")
	if code, out := abrir("2026-12-01"); code != http.StatusCreated {
		t.Errorf("com a data-limite em 28/02, 10/02 aceita o ano anterior: %d %v", code, out)
	}
}

// Tela "Solicitações" (itens 20 e 21): atendente vê tudo e acha por código/CC/conta; solicitante vê só os próprios.
func TestPG_BuscaSolicitacoes(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	idMeu := e.solicitacao(t, b, "finalizada_sucesso")
	var outroUsuario string
	if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ('outro'||$1||'@teste.local', 'Outra Pessoa', 'solicitante') ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, fmt.Sprintf("%d", os.Getpid())).Scan(&outroUsuario); err != nil {
		t.Fatal(err)
	}
	b2 := b
	b2.usuario = outroUsuario
	idDeOutro := e.solicitacao(t, b2, "aberta")

	busca := func(usuario, perfil, query string) []map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/solicitacoes/busca?"+query, nil)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		BuscarSolicitacoesHandler(e.app)(rec, req)
		if rec.Code != 200 {
			t.Fatalf("busca %q: %d %s", query, rec.Code, rec.Body.String())
		}
		var out struct {
			Items []map[string]any `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Items
	}
	tem := func(itens []map[string]any, id string) bool {
		for _, i := range itens {
			if i["id"] == id {
				return true
			}
		}
		return false
	}
	// solicitante: só o que abriu (inclusive finalizado)
	r := busca(b.usuario, "solicitante", "")
	if !tem(r, idMeu) || tem(r, idDeOutro) {
		t.Errorf("solicitante deveria ver só os próprios chamados")
	}
	// atendente: todos, e acha pelo código (GO-n e só o número), pela conta e pelo status
	r = busca(b.usuario, "atendente", "")
	if !tem(r, idMeu) || !tem(r, idDeOutro) {
		t.Errorf("atendente deveria ver todos")
	}
	codigo := ""
	for _, i := range r {
		if i["id"] == idDeOutro {
			codigo = i["codigo"].(string)
		}
	}
	for _, q := range []string{"q=" + codigo, "q=" + strings.TrimPrefix(codigo, "GO-"), "q=" + strings.ToLower(codigo)} {
		if res := busca(b.usuario, "atendente", q); len(res) != 1 || res[0]["id"] != idDeOutro {
			t.Errorf("%s deveria achar só o chamado de código %s: %d resultados", q, codigo, len(res))
		}
	}
	if res := busca(b.usuario, "atendente", "q=T4"+fmt.Sprintf("%d", os.Getpid())); !tem(res, idMeu) {
		t.Errorf("busca pelo código da conta deveria achar o chamado")
	}
	if res := busca(b.usuario, "atendente", "status=finalizada_sucesso&q=Solicitante+Teste"); !tem(res, idMeu) || tem(res, idDeOutro) {
		t.Errorf("filtro de status + nome do solicitante")
	}
	if res := busca(b.usuario, "atendente", "q=%25"); len(res) != 0 && tem(res, idMeu) && tem(res, idDeOutro) && len(res) > 2 {
		// '%' é tratado como texto, não como curinga
		t.Errorf("curinga não pode casar tudo")
	}
	if res := busca(b.usuario, "atendente", "de=2999-01-01"); len(res) != 0 {
		t.Errorf("período futuro não deveria achar nada")
	}
}

// Regras de feriado (item 27) lidas pelo cálculo de prazo com o papel da aplicação, usando a carga inicial da migration.
func TestPG_PrazoComRegrasDeFeriado(t *testing.T) {
	e := pgSetup(t)
	cal, err := sla.NovoCalendario(e.app)
	if err != nil {
		t.Fatalf("calendário com regras: %v", err)
	}
	loc, _ := time.LoadLocation("America/Recife")
	// sexta 13/02/2026 10:00: o Carnaval (16 e 17/02) e o fim de semana não contam → 48h úteis terminam quinta 19/02 10:00
	got := cal.PrazoLimite(time.Date(2026, 2, 13, 10, 0, 0, 0, loc))
	if want := time.Date(2026, 2, 19, 10, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("prazo = %s, esperado %s", got, want)
	}
}

// Compartilhar chamado (somente leitura): só o dono compartilha; quem recebe vê o chamado, o anexo e a lista; terceiro não.
func TestPG_CompartilharChamado(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "aberta")
	suf := fmt.Sprintf("%d", os.Getpid())
	novoUsuario := func(prefixo string) string {
		var uid string
		if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ($1||$2||'@teste.local', $1||' Teste', 'solicitante') ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, prefixo, suf).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		return uid
	}
	amigo, estranho := novoUsuario("amigo"), novoUsuario("estranho")
	enviados := make(chan string, 4)
	orig := enviarEmailCompartilhamento
	enviarEmailCompartilhamento = func(to []string, assunto, corpo string) error { enviados <- to[0] + "|" + assunto; return nil }
	defer func() { enviarEmailCompartilhamento = orig }()

	chamar := func(h http.HandlerFunc, metodo, caminho, usuario, corpo string, valores map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, caminho, strings.NewReader(corpo))
		for k, v := range valores {
			req.SetPathValue(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": "solicitante"}))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	pv := map[string]string{"id": id}

	// antes de compartilhar, o amigo não vê o chamado
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", amigo, "", pv); rec.Code != 403 {
		t.Fatalf("sem compartilhar deveria ser 403, veio %d", rec.Code)
	}
	// quem não é dono não compartilha
	if rec := chamar(CompartilharHandler(e.app), "POST", "/", estranho, `{"usuario_id":"`+amigo+`"}`, pv); rec.Code != 403 {
		t.Errorf("não-dono compartilhando deveria ser 403, veio %d", rec.Code)
	}
	// o dono compartilha (e não consigo mesmo)
	if rec := chamar(CompartilharHandler(e.app), "POST", "/", b.usuario, `{"usuario_id":"`+b.usuario+`"}`, pv); rec.Code != 400 {
		t.Errorf("compartilhar consigo mesmo deveria ser 400, veio %d", rec.Code)
	}
	if rec := chamar(CompartilharHandler(e.app), "POST", "/", b.usuario, `{"usuario_id":"`+amigo+`"}`, pv); rec.Code != 200 {
		t.Fatalf("dono compartilhando: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case m := <-enviados:
		if !strings.Contains(m, "amigo") || !strings.Contains(m, "GO-") {
			t.Errorf("e-mail inesperado: %s", m)
		}
	case <-time.After(2 * time.Second):
		t.Error("o aviso por e-mail deveria ser enviado")
	}
	// compartilhar de novo não duplica nem reenvia
	if rec := chamar(CompartilharHandler(e.app), "POST", "/", b.usuario, `{"usuario_id":"`+amigo+`"}`, pv); rec.Code != 200 {
		t.Fatalf("repetir: %d", rec.Code)
	}
	select {
	case m := <-enviados:
		t.Errorf("não deveria reenviar o e-mail: %s", m)
	case <-time.After(300 * time.Millisecond):
	}
	// agora o amigo vê, o estranho continua sem ver; desde 26/11 (§9.1) quem acompanha vê quem mais acompanha
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", amigo, "", pv); rec.Code != 200 {
		t.Errorf("compartilhado deveria ver: %d", rec.Code)
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", estranho, "", pv); rec.Code != 403 {
		t.Errorf("terceiro deveria continuar sem ver: %d", rec.Code)
	}
	if rec := chamar(CompartilhamentosHandler(e.app), "GET", "/", amigo, "", pv); rec.Code != 200 {
		t.Errorf("quem acompanha vê a lista de visualizadores: %d", rec.Code)
	}
	if rec := chamar(CompartilhamentosHandler(e.app), "GET", "/", estranho, "", pv); rec.Code != 403 {
		t.Errorf("terceiro não vê a lista de visualizadores: %d", rec.Code)
	}
	// aparece na busca do amigo (e só nela), como compartilhado
	reqB := httptest.NewRequest("GET", "/api/solicitacoes/busca", nil)
	reqB = reqB.WithContext(context.WithValue(reqB.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": amigo, "perfil": "solicitante"}))
	recB := httptest.NewRecorder()
	BuscarSolicitacoesHandler(e.app)(recB, reqB)
	if !strings.Contains(recB.Body.String(), id) || !strings.Contains(recB.Body.String(), "Solicitante Teste") {
		t.Errorf("o chamado deveria aparecer na lista do amigo, com quem compartilhou: %s", recB.Body.String())
	}
	// §9.1 (26/11): o visualizador comenta, sem mudar o status do chamado
	var stAntes, stDepois string
	_ = e.app.QueryRow(`SELECT status FROM solicitacoes WHERE id = $1`, id).Scan(&stAntes)
	if rec := chamar(ComentarSolicitacaoHandler(e.priv), "POST", "/", amigo, `{"versao":1,"comentario":"oi"}`, pv); rec.Code != 200 {
		t.Errorf("visualizador deveria comentar: %d %s", rec.Code, rec.Body.String())
	}
	_ = e.app.QueryRow(`SELECT status FROM solicitacoes WHERE id = $1`, id).Scan(&stDepois)
	if stAntes != stDepois {
		t.Errorf("comentário do visualizador não muda o status: %s -> %s", stAntes, stDepois)
	}
	// o terceiro passa a ver quando entra num GRUPO em cópia; o grupo inativo deixa de valer
	var gid string
	if err := e.app.QueryRow(`INSERT INTO grupos (nome, gestor_id, criado_por) VALUES ('Grupo Expansão', $1, $1) RETURNING id`, b.usuario).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Exec(`INSERT INTO grupo_membros (grupo_id, usuario_id) VALUES ($1, $2)`, gid, estranho); err != nil {
		t.Fatal(err)
	}
	if rec := chamar(IncluirGrupoNoChamadoHandler(e.app), "POST", "/", b.usuario, `{"grupo_id":"`+gid+`"}`, pv); rec.Code != 200 {
		t.Fatalf("incluir grupo: %d %s", rec.Code, rec.Body.String())
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", estranho, "", pv); rec.Code != 200 {
		t.Errorf("membro do grupo em cópia deveria ver: %d", rec.Code)
	}
	if _, err := e.app.Exec(`UPDATE grupos SET ativo = false WHERE id = $1`, gid); err != nil {
		t.Fatal(err)
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", estranho, "", pv); rec.Code != 403 {
		t.Errorf("grupo inativo não dá acesso: %d", rec.Code)
	}
	// retirar o acesso
	if rec := chamar(RemoverCompartilhamentoHandler(e.app), "DELETE", "/", b.usuario, "", map[string]string{"id": id, "usuarioId": amigo}); rec.Code != 200 {
		t.Fatalf("remover: %d", rec.Code)
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", "/", amigo, "", pv); rec.Code != 403 {
		t.Errorf("depois de retirar, deveria voltar a 403: %d", rec.Code)
	}
	// busca de usuários: mínimo de 3 letras e não devolve a si mesmo
	rec := chamar(BuscarUsuariosHandler(e.app), "GET", "/api/usuarios/busca?q=am", b.usuario, "", nil)
	if strings.Contains(rec.Body.String(), "amigo") {
		t.Errorf("busca com menos de 3 caracteres não deveria devolver ninguém")
	}
	rec = chamar(BuscarUsuariosHandler(e.app), "GET", "/api/usuarios/busca?q=amigo", b.usuario, "", nil)
	if !strings.Contains(rec.Body.String(), "amigo") {
		t.Errorf("busca deveria achar o amigo: %s", rec.Body.String())
	}
}

// Resposta da Controladoria (26/11): §3 edição pela Controladoria (histórico com antes/depois, destino único acompanha
// a origem) e §2 nota interna (o solicitante não vê).
func TestPG_EdicaoPelaControladoriaENotaInterna(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	id := e.solicitacao(t, b, "em_atendimento")
	var atendente string
	if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ('atend'||$1||'@teste.local', 'Atendente Teste', 'atendente')
		ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, fmt.Sprint(os.Getpid())).Scan(&atendente); err != nil {
		t.Fatal(err)
	}
	chamar := func(h http.HandlerFunc, metodo, usuario, perfil, corpo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(metodo, "/", strings.NewReader(corpo))
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	var origemID string
	if err := e.app.QueryRow(`SELECT id FROM solicitacao_lancamentos WHERE solicitacao_id = $1 AND lado = 'origem'`, id).Scan(&origemID); err != nil {
		t.Fatal(err)
	}
	// o chamado de exemplo nasce com origem = destino; o destino vai para outra conta (senão a edição é recusada)
	if _, err := e.admin.Exec(`UPDATE solicitacao_lancamentos SET conta_id = $2 WHERE solicitacao_id = $1 AND lado = 'destino'`, id, b.conta5); err != nil {
		t.Fatal(err)
	}
	corpo := `{"versao":1,"descricao":"Descrição nova","lancamentos":[{"id":"` + origemID + `","valor":45000}]}`
	rec := chamar(EditarChamadoHandler(e.app, e.priv), "PUT", atendente, "atendente", corpo)
	if rec.Code != 200 {
		t.Fatalf("editar: %d %s", rec.Code, rec.Body.String())
	}
	var destino float64
	_ = e.app.QueryRow(`SELECT valor::float8 FROM solicitacao_lancamentos WHERE solicitacao_id = $1 AND lado = 'destino'`, id).Scan(&destino)
	if destino != 45000 {
		t.Errorf("o destino único acompanha a origem: %v", destino)
	}
	var hist string
	_ = e.app.QueryRow(`SELECT texto FROM solicitacao_comentarios WHERE solicitacao_id = $1 AND tipo = 'edicao'`, id).Scan(&hist)
	if !strings.Contains(hist, "R$ 60.000,00 → R$ 45.000,00") || !strings.Contains(hist, "Descrição nova") {
		t.Errorf("histórico da edição: %q", hist)
	}
	if _, v := statusDe(t, e.admin, id); v != 2 {
		t.Errorf("a edição sobe a versão: %d", v)
	}
	// versão velha: conflito
	if rec := chamar(EditarChamadoHandler(e.app, e.priv), "PUT", atendente, "atendente", corpo); rec.Code != 409 {
		t.Errorf("versão desatualizada deveria dar 409: %d", rec.Code)
	}
	// nota interna: o atendimento vê, o solicitante não
	if rec := chamar(NotaInternaHandler(e.app), "POST", atendente, "atendente", `{"texto":"conferir com o SAP"}`); rec.Code != 201 {
		t.Fatalf("nota: %d %s", rec.Code, rec.Body.String())
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", b.usuario, "solicitante", ""); strings.Contains(rec.Body.String(), "conferir com o SAP") {
		t.Errorf("o solicitante não pode ver a nota interna")
	}
	if rec := chamar(ObterSolicitacaoHandler(e.app), "GET", atendente, "atendente", ""); !strings.Contains(rec.Body.String(), "conferir com o SAP") {
		t.Errorf("o atendimento vê a nota interna: %s", rec.Body.String())
	}
}

// Segunda leva (26/11): F6 escopo de CC do grupo, F3 cancelar, F5 filtro "Quem abriu" e visibilidade do painel,
// F4 marcas FL/Externo do destino, F7/F8 grão de Obras.
func TestPG_SegundaLeva(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	suf := fmt.Sprint(os.Getpid())
	novoUsuario := func(nome string) string {
		var id string
		if err := e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ($1, $2, 'solicitante')
			ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, nome+suf+"@teste.local", nome).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	colega := novoUsuario("Colega")
	// CC com código de 8 dígitos (divisão 9001 + CC 6120) para o escopo do grupo
	var div, cc string
	if err := e.admin.QueryRow(`INSERT INTO divisoes (codigo, nome) VALUES ('9001', '9001') ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`).Scan(&div); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(`INSERT INTO centros_custo (codigo, nome, divisao_id) VALUES ('90016120', 'CC escopo', $1) ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, div).Scan(&cc); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := e.app.QueryRow(`INSERT INTO solicitacoes (tipo_solicitacao, solicitante_id, centro_custo_id, status, aprovador_snapshot)
		VALUES ('inclusao', $1, $2, 'aberta', '{}') RETURNING id`, b.usuario, cc).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ver := func(usuario, perfil string) int {
		req := httptest.NewRequest("GET", "/", nil)
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		ObterSolicitacaoHandler(e.app)(rec, req)
		return rec.Code
	}
	if c := ver(colega, "solicitante"); c != 403 {
		t.Fatalf("sem grupo o colega não vê: %d", c)
	}
	var gid string
	if err := e.app.QueryRow(`INSERT INTO grupos (nome, gestor_id, criado_por) VALUES ('Grupo Escopo', $1, $1) RETURNING id`, b.usuario).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	_, _ = e.app.Exec(`INSERT INTO grupo_membros (grupo_id, usuario_id) VALUES ($1, $2)`, gid, colega)
	// F6: CC 6120 limitado à divisão 0101 → não vê; CC 6120 sem divisão → vê
	_, _ = e.app.Exec(`INSERT INTO grupo_escopo (grupo_id, cc, divisao) VALUES ($1, '6120', '0101')`, gid)
	if c := ver(colega, "solicitante"); c != 403 {
		t.Fatalf("escopo em outra divisão não dá acesso: %d", c)
	}
	_, _ = e.app.Exec(`INSERT INTO grupo_escopo (grupo_id, cc, divisao) VALUES ($1, '6120', '9001')`, gid)
	if c := ver(colega, "solicitante"); c != 200 {
		t.Fatalf("escopo com a divisão do chamado dá acesso: %d", c)
	}
	// F5: no painel do colega aparece o filtro com "Você" e o solicitante do chamado
	req := httptest.NewRequest("GET", "/api/paineis/consolidado", nil)
	req.SetPathValue("painel", "consolidado")
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": colega, "perfil": "solicitante"}))
	rec := httptest.NewRecorder()
	ObterPainelHandler(e.app)(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"nome":"Você"`) || !strings.Contains(rec.Body.String(), "Solicitante Teste") {
		t.Fatalf("painel do colega: %d %s", rec.Code, rec.Body.String())
	}
	// F3: cancelar (dono, chamado aberto) → cancelada; de novo → recusado
	cancelar := func(usuario, perfil string, versao int) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/", strings.NewReader(fmt.Sprintf(`{"versao":%d,"motivo":"teste"}`, versao)))
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		CancelarChamadoHandler(e.priv)(rec, req)
		return rec
	}
	if rec := cancelar(colega, "solicitante", 1); rec.Code != 409 {
		t.Fatalf("quem só acompanha não cancela: %d %s", rec.Code, rec.Body.String())
	}
	if rec := cancelar(b.usuario, "solicitante", 1); rec.Code != 200 {
		t.Fatalf("dono cancela: %d %s", rec.Code, rec.Body.String())
	}
	if s, _ := statusDe(t, e.admin, id); s != "cancelada" {
		t.Fatalf("status: %s", s)
	}
	// F4: marcas — divisão só FL recusa destino Externo
	if _, err := e.admin.Exec(`UPDATE divisoes SET faz_fl = true, faz_externo = false WHERE id = $1`, div); err != nil {
		t.Fatal(err)
	}
	tx, _ := e.app.Begin()
	msg, err := verificarTipoDestino(tx, []lancamentoValidado{{Lado: "destino", DivisaoID: div}}, "Externo")
	_ = tx.Rollback()
	if err != nil || !strings.Contains(msg, "não faz compra externa") {
		t.Fatalf("tipo do destino: %q %v", msg, err)
	}
	// F7/F8: obra com estrutura "Anexo Central", subgrupo F1.1 com ordem externa e FL
	var loc, sub, subFora string
	_ = e.admin.QueryRow(`INSERT INTO locais_obra (codigo, nome, tipo_estrutura) VALUES ('T91.3'||$1, 'Anexo teste', 'Anexo Central') RETURNING id`, suf).Scan(&loc)
	_ = e.admin.QueryRow(`INSERT INTO subgrupos_despesa (codigo, nome, familias) VALUES ('TF1.1'||$1, 'Fundações', '{Anexo Central}') RETURNING id`, suf).Scan(&sub)
	_ = e.admin.QueryRow(`INSERT INTO subgrupos_despesa (codigo, nome, familias) VALUES ('TK1.1'||$1, 'Só Fortaleza', '{Fortaleza}') RETURNING id`, suf).Scan(&subFora)
	_, _ = e.admin.Exec(`INSERT INTO obra_ordens (numero_ordem, local_obra_id, subgrupo_despesa_id, tipo_despesa, tipo_faturamento) VALUES
		('T6024'||$3, $1, $2, 'aquisicao_externa', 'aquisicao_externa'), ('T6025'||$3, $1, $2, 'aquisicao_interna', 'fl')`, loc, sub, suf)
	linha := func(subgrupo, ordem string) []obraLinhaValidado {
		return []obraLinhaValidado{{LocalObraID: loc, SubgrupoDespesaID: subgrupo, OrdemInvestimento: ordem, Valor: 10}}
	}
	conf := func(classif string, l []obraLinhaValidado) string {
		tx, _ := e.app.Begin()
		defer func() { _ = tx.Rollback() }()
		m, err := verificarGraoObras(tx, classif, l)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := conf("inclusao", linha(sub, "CRIAR")); !strings.Contains(m, "já existe a ordem T6024") {
		t.Errorf("CRIAR com ordem existente: %q", m)
	}
	if m := conf("fl", linha(sub, "T6024"+suf)); !strings.Contains(m, "use T6025") {
		t.Errorf("FL deve usar a ordem interna: %q", m)
	}
	if m := conf("fl", linha(sub, "T6025"+suf)); m != "" {
		t.Errorf("FL com a ordem interna passa: %q", m)
	}
	if m := conf("inclusao", linha(subFora, "CRIAR")); !strings.Contains(m, "não pertence à estrutura") {
		t.Errorf("subgrupo de outra família: %q", m)
	}
	// F3: a origem de uma transferência viva reserva o valor (dez/2026, 60 mil); cancelada, deixa de reservar
	tid := e.solicitacao(t, b, "aberta")
	res, cods, err := reservasDoAno(context.Background(), e.app, b.cc, b.conta4, 2026, "")
	if err != nil || res[11] != 60000 || len(cods[11]) != 1 {
		t.Fatalf("reserva viva: %v %v %v", res[11], cods[11], err)
	}
	if res2, _, _ := reservasDoAno(context.Background(), e.app, b.cc, b.conta4, 2026, tid); res2[11] != 0 {
		t.Errorf("a edição ignora a reserva do próprio chamado: %v", res2[11])
	}
	_, _ = e.priv.Exec(`UPDATE solicitacoes SET status = 'cancelada' WHERE id = $1`, tid)
	if res3, _, _ := reservasDoAno(context.Background(), e.app, b.cc, b.conta4, 2026, ""); res3[11] != 0 {
		t.Errorf("cancelada não reserva: %v", res3[11])
	}
}

// Terceira leva (26/11): F12 comentário (pendência e reabertura), nota com anexo interno, F14 atribuir, F11 filtros
// com lado, F8b ordem bloqueada.
func TestPG_TerceiraLeva(t *testing.T) {
	e := pgSetup(t)
	b := e.seed(t)
	suf := fmt.Sprint(os.Getpid())
	var atend string
	_ = e.admin.QueryRow(`INSERT INTO usuarios (email, nome, perfil) VALUES ('at3'||$1||'@teste.local', 'Atendente Três', 'atendente')
		ON CONFLICT (LOWER(email)) DO UPDATE SET nome = EXCLUDED.nome RETURNING id`, suf).Scan(&atend)
	chamar := func(h http.HandlerFunc, id, usuario, perfil, corpo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/", strings.NewReader(corpo))
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	prio := func(id string) (string, string, bool) {
		var st, p string
		var resp bool
		_ = e.admin.QueryRow(`SELECT status, prioridade, respondido FROM solicitacoes WHERE id = $1`, id).Scan(&st, &p, &resp)
		return st, p, resp
	}
	// pendência → resposta do solicitante: em atendimento, prioridade normal, respondido
	id := e.solicitacao(t, b, "pendente")
	if rec := chamar(ComentarSolicitacaoHandler(e.priv), id, b.usuario, "solicitante", `{"versao":1,"comentario":"segue o documento","anexos":[`+anexoJSON("doc.pdf", "application/pdf", testAnexoConteudoBase64)+`]}`); rec.Code != 200 {
		t.Fatalf("responder pendência: %d %s", rec.Code, rec.Body.String())
	}
	if st, p, r := prio(id); st != "em_atendimento" || p != "normal" || !r {
		t.Fatalf("resposta à pendência: %s %s %v", st, p, r)
	}
	var nAnexos int
	_ = e.admin.QueryRow(`SELECT count(*) FROM comentario_anexos ca JOIN solicitacao_anexos a ON a.id = ca.anexo_id WHERE a.solicitacao_id = $1`, id).Scan(&nAnexos)
	if nAnexos != 1 {
		t.Fatalf("anexo do comentário: %d", nAnexos)
	}
	// finalizado → comentário do solicitante reabre com prioridade alta; o do atendimento não reabre
	fin := e.solicitacao(t, b, "finalizada_sucesso")
	if rec := chamar(ComentarSolicitacaoHandler(e.priv), fin, atend, "atendente", `{"versao":1,"comentario":"só registro"}`); rec.Code != 200 {
		t.Fatalf("atendimento comenta: %d", rec.Code)
	}
	if st, _, _ := prio(fin); st != "finalizada_sucesso" {
		t.Fatalf("comentário do atendimento não reabre: %s", st)
	}
	if rec := chamar(ComentarSolicitacaoHandler(e.priv), fin, b.usuario, "solicitante", `{"versao":2,"comentario":"voltou a dar problema"}`); rec.Code != 200 {
		t.Fatalf("solicitante comenta finalizado: %d %s", rec.Code, rec.Body.String())
	}
	if st, p, _ := prio(fin); st != "em_atendimento" || p != "alta" {
		t.Fatalf("reabertura: %s %s", st, p)
	}
	// nota interna com anexo: atendimento vê; solicitante não vê nem baixa
	if rec := chamar(NotaInternaHandler(e.app), id, atend, "atendente", `{"texto":"conferido","anexos":[`+anexoJSON("interno.pdf", "application/pdf", testAnexoConteudoBase64)+`]}`); rec.Code != 201 {
		t.Fatalf("nota com anexo: %d %s", rec.Code, rec.Body.String())
	}
	var anexoInt string
	_ = e.admin.QueryRow(`SELECT id FROM solicitacao_anexos WHERE solicitacao_id = $1 AND interno`, id).Scan(&anexoInt)
	ver := func(usuario, perfil string) string {
		req := httptest.NewRequest("GET", "/", nil)
		req.SetPathValue("id", id)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		ObterSolicitacaoHandler(e.app)(rec, req)
		return rec.Body.String()
	}
	if strings.Contains(ver(b.usuario, "solicitante"), "interno.pdf") || !strings.Contains(ver(atend, "atendente"), "interno.pdf") {
		t.Fatalf("anexo interno: visibilidade errada")
	}
	baixar := func(usuario, perfil string) int {
		req := httptest.NewRequest("GET", "/", nil)
		req.SetPathValue("anexoId", anexoInt)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
		rec := httptest.NewRecorder()
		BaixarAnexoHandler(e.app)(rec, req)
		return rec.Code
	}
	if c := baixar(b.usuario, "solicitante"); c != 404 {
		t.Fatalf("solicitante baixando anexo interno: %d", c)
	}
	// nota com anexo em chamado finalizado: recusada
	fin2 := e.solicitacao(t, b, "finalizada_erro")
	if rec := chamar(NotaInternaHandler(e.app), fin2, atend, "atendente", `{"texto":"x","anexos":[`+anexoJSON("a.pdf", "application/pdf", testAnexoConteudoBase64)+`]}`); rec.Code != 400 {
		t.Fatalf("anexo em nota de finalizado: %d", rec.Code)
	}
	// F14: atribuir 2 abertos ao atendente; um com versão velha falha só ele
	a1, a2 := e.solicitacao(t, b, "aberta"), e.solicitacao(t, b, "aberta")
	corpo := fmt.Sprintf(`{"atendente_id":"%s","itens":[{"id":"%s","versao":1},{"id":"%s","versao":9}]}`, atend, a1, a2)
	rec := chamar(AtribuirHandler(e.priv), "", b.usuario, "atendente", corpo)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) || !strings.Contains(rec.Body.String(), "o chamado mudou") {
		t.Fatalf("atribuir: %d %s", rec.Code, rec.Body.String())
	}
	if st, _ := statusDe(t, e.admin, a1); st != "em_atendimento" {
		t.Fatalf("atribuído: %s", st)
	}
	// F11: filtro por conta com lado (o chamado de exemplo tem origem e destino na conta4)
	busca := func(qs string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/solicitacoes/busca?"+qs, nil)
		req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": atend, "perfil": "atendente"}))
		rec := httptest.NewRecorder()
		BuscarSolicitacoesHandler(e.app)(rec, req)
		return rec
	}
	var conta4 string
	_ = e.admin.QueryRow(`SELECT codigo FROM contas WHERE id = $1`, b.conta4).Scan(&conta4)
	_ = conta4
	if rec := busca("filial=zzz"); rec.Code != 400 {
		t.Fatalf("filtro desconhecido: %d", rec.Code)
	}
	if rec := busca("lado=destino&cc=9999"); rec.Code != 200 || strings.Contains(rec.Body.String(), a1) {
		t.Fatalf("cc inexistente não acha: %d", rec.Code)
	}
	// F8b: ordem bloqueada é recusada
	var loc, sub string
	_ = e.admin.QueryRow(`INSERT INTO locais_obra (codigo, nome, tipo_estrutura) VALUES ('TF3'||$1, 'Fortaleza teste', 'Fortaleza') RETURNING id`, suf).Scan(&loc)
	_ = e.admin.QueryRow(`INSERT INTO subgrupos_despesa (codigo, nome, familias) VALUES ('TD1LOJA.1'||$1, 'TERRAPLANAGEM LOJA', '{Fortaleza}') RETURNING id`, suf).Scan(&sub)
	_, _ = e.admin.Exec(`INSERT INTO obra_ordens (numero_ordem, local_obra_id, subgrupo_despesa_id, tipo_despesa, tipo_faturamento, bloqueada, observacao) VALUES
		('T60125'||$3, $1, $2, 'aquisicao_externa', 'direto', true, 'Ordem bloqueada'), ('T60126'||$3, $1, $2, 'aquisicao_externa', 'sub_contratada', true, 'Ordem bloqueada')`, loc, sub, suf)
	tx, _ := e.app.Begin()
	m, err := verificarGraoObras(tx, "inclusao", []obraLinhaValidado{{LocalObraID: loc, SubgrupoDespesaID: sub, OrdemInvestimento: "T60125" + suf, Valor: 1, Tipo: "Externa"}})
	m2, _ := verificarGraoObras(tx, "fl", []obraLinhaValidado{{LocalObraID: loc, SubgrupoDespesaID: sub, OrdemInvestimento: "CRIAR", Valor: 1, Tipo: "FL"}})
	m3, _ := verificarGraoObras(tx, "inclusao", []obraLinhaValidado{{LocalObraID: loc, SubgrupoDespesaID: sub, OrdemInvestimento: "CRIAR", Valor: 1, Tipo: "Externa"}})
	_ = tx.Rollback()
	if err != nil || !strings.Contains(m, "bloqueada para lançamento") {
		t.Fatalf("ordem bloqueada: %q %v", m, err)
	}
	if !strings.Contains(m2, "Fortaleza não tem FL") {
		t.Fatalf("FL em Fortaleza: %q", m2)
	}
	if !strings.Contains(m3, "já existem as ordens") {
		t.Fatalf("CRIAR cita as duas ordens: %q", m3)
	}
}
