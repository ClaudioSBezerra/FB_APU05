package controleti

// Testes contra um Postgres de verdade (sqlmock não executa o SQL). Só rodam
// com TEST_DATABASE_URL apontando para um banco DESCARTÁVEL — o teste aplica
// as migrations se as tabelas ainda não existirem e grava/apaga dados em
// ti_*:
//
//	docker run -d --rm --name pgti -e POSTGRES_PASSWORD=x -p 55432:5432 postgres:15-alpine
//	TEST_DATABASE_URL='postgres://postgres:x@localhost:55432/postgres?sslmode=disable' \
//	  go test ./internal/controleti -run Integracao -v
//
// Com TI_PLANILHA e TI_ORACULO também confere o ciclo importar -> banco ->
// modelo contra o painel de 2026.

import (
	"database/sql"
	"encoding/json"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func bancoDeTeste(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("defina TEST_DATABASE_URL (banco descartável) para rodar os testes de integração")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("banco de teste inacessível: %v", err)
	}

	var existe sql.NullString
	_ = db.QueryRow(`SELECT to_regclass('ti_lancamentos')::text`).Scan(&existe)
	if !existe.Valid {
		arquivos, _ := filepath.Glob("../../migrations/*.sql")
		sort.Strings(arquivos)
		for _, a := range arquivos {
			sqlTxt, err := os.ReadFile(a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(sqlTxt)); err != nil {
				t.Fatalf("migration %s: %v", filepath.Base(a), err)
			}
		}
	}
	for _, tb := range []string{"ti_ksb1", "ti_ksb2", "ti_cargas_sap", "ti_importacoes", "ti_orcamento_valores", "ti_orcamento_linhas", "ti_lancamentos",
		"ti_param_cc", "ti_param_conta", "ti_param_status", "ti_param_divisao", "ti_param_fornecedor", "ti_config"} {
		if _, err := db.Exec("DELETE FROM " + tb); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// comoApp abre uma conexão com o papel fb_apu05_app — o que o servidor usa nas
// requisições (withDB). Importar/Carregar rodam com ele para pegar falta de
// permissão (ex.: sequências) que o superusuário do teste esconderia.
func comoApp(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL_APP")
	if url == "" {
		u, err := neturl.Parse(os.Getenv("TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		u.User = neturl.UserPassword("fb_apu05_app", "changeme") // senha da migration 010
		url = u.String()
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("papel fb_apu05_app inacessível (defina TEST_DATABASE_URL_APP): %v", err)
	}
	return db
}

func TestIntegracao_ImportarEhIdempotenteEPreservaOutrasFontes(t *testing.T) {
	db := bancoDeTeste(t)
	app := comoApp(t)
	pl, err := LerPlanilha(planilhaSintetica(t))
	if err != nil {
		t.Fatal(err)
	}

	r1, err := Importar(app, pl, "", "teste.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if r1.OrcamentoLinhas != 1 || r1.Meses != 3 || r1.Lancamentos != 2 || r1.PrimeiroMes != "2026-01" || r1.UltimoMes != "2026-03" {
		t.Fatalf("resumo da 1ª importação: %+v", r1)
	}

	// dados de outras fontes: não podem ser tocados por uma reimportação
	if _, err := db.Exec(`INSERT INTO ti_lancamentos (fonte, status, valor, fornecedor, competencia, cc_sap, conta)
		VALUES ('manual', 'LANÇADO', 50, 'DIGITADO NO SISTEMA', '2026-02-01', '10026150', '4222000020'),
		       ('sap_api', 'LANÇADO', 70, 'VEIO DO SAP', '2026-02-01', '10026150', '4222000020')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ti_orcamento_linhas (fonte, origem, divisao, cc, conta) VALUES ('orcamento_2027', 'Original', '1002', '6150', '4222000020')`); err != nil {
		t.Fatal(err)
	}

	r2, err := Importar(app, pl, "", "teste.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Lancamentos != r1.Lancamentos || r2.OrcamentoLinhas != r1.OrcamentoLinhas {
		t.Fatalf("reimportar mudou a contagem: %+v vs %+v", r2, r1)
	}

	conta := func(q string) int {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := conta(`SELECT count(*) FROM ti_lancamentos WHERE fonte = 'planilha'`); n != 2 {
		t.Errorf("lançamentos da planilha = %d, esperado 2 (sem duplicar)", n)
	}
	if n := conta(`SELECT count(*) FROM ti_lancamentos WHERE fonte IN ('manual','sap_api')`); n != 2 {
		t.Errorf("lançamentos manual/sap_api = %d, esperado 2 (preservados)", n)
	}
	if n := conta(`SELECT count(*) FROM ti_orcamento_linhas WHERE fonte = 'orcamento_2027'`); n != 1 {
		t.Errorf("orçamento de outra fonte = %d, esperado 1 (preservado)", n)
	}
	if n := conta(`SELECT count(*) FROM ti_orcamento_valores`); n != 3 {
		t.Errorf("valores de orçamento = %d, esperado 3 (1 linha x 3 meses, sem sobra da importação anterior)", n)
	}
	if n := conta(`SELECT count(*) FROM ti_importacoes`); n != 2 {
		t.Errorf("histórico de importações = %d, esperado 2", n)
	}

	// o modelo lido do banco tem os derivados e inclui as outras fontes
	m, err := Carregar(app, time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if m.Meta.LastClosed != "2026-01" { // 20/02: dia >= 6 -> janeiro fechado
		t.Errorf("mês fechado = %s", m.Meta.LastClosed)
	}
	if m.Meta.FileName != "teste.xlsx" {
		t.Errorf("arquivo = %q", m.Meta.FileName)
	}
	if len(m.Lanc) != 4 {
		t.Fatalf("lançamentos no modelo = %d, esperado 4 (2 planilha + manual + sap_api)", len(m.Lanc))
	}
	var achouSAP bool
	for _, l := range m.Lanc {
		if l.Forn == "VEIO DO SAP" && l.Div == "1002" && l.CC == "6150" && l.Classif == "Realizado" {
			achouSAP = true
		}
	}
	if !achouSAP {
		t.Error("lançamento sap_api não apareceu com os campos derivados")
	}

	// override manual do mês fechado vence a regra automática
	if _, err := db.Exec(`INSERT INTO ti_config (chave, valor) VALUES ('ultimo_mes_fechado', '2025-12')`); err != nil {
		t.Fatal(err)
	}
	m, _ = Carregar(app, time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC))
	if m.Meta.LastClosed != "2025-12" {
		t.Errorf("override ignorado: %s", m.Meta.LastClosed)
	}
}

func TestIntegracao_ImportarFalhaNaMetadeNaoGravaNada(t *testing.T) {
	db := bancoDeTeste(t)
	app := comoApp(t)
	pl, err := LerPlanilha(planilhaSintetica(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Importar(app, pl, "", "bom.xlsx"); err != nil {
		t.Fatal(err)
	}

	// 2º arquivo com um lançamento inválido (status maior que o campo): a
	// transação inteira tem de voltar e o estado anterior continuar intacto
	ruim := *pl
	ruim.Lancamentos = append(append([]Lanc{}, pl.Lancamentos...), Lanc{Status: strings.Repeat("X", 80), Valor: 1})
	if _, err := Importar(app, &ruim, "", "ruim.xlsx"); err == nil {
		t.Fatal("importação inválida deveria falhar")
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM ti_lancamentos WHERE fonte = 'planilha'`).Scan(&n)
	if n != 2 {
		t.Errorf("rollback falhou: %d lançamentos da planilha, esperado 2 (estado anterior)", n)
	}
	_ = db.QueryRow(`SELECT count(*) FROM ti_importacoes`).Scan(&n)
	if n != 1 {
		t.Errorf("importação falha foi registrada no histórico: %d", n)
	}
}

func TestIntegracao_CicloCompletoContraOPainel2026(t *testing.T) {
	arq, orac := os.Getenv("TI_PLANILHA"), os.Getenv("TI_ORACULO")
	if arq == "" || orac == "" {
		t.Skip("defina TI_PLANILHA e TI_ORACULO para o ciclo completo")
	}
	db := bancoDeTeste(t)
	app := comoApp(t)

	dados, err := os.ReadFile(arq)
	if err != nil {
		t.Fatal(err)
	}
	var want Modelo
	bruto, _ := os.ReadFile(orac)
	if err := json.Unmarshal(bruto, &want); err != nil {
		t.Fatal(err)
	}

	pl, err := LerPlanilha(dados)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Importar(app, pl, "", filepath.Base(arq))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("importado: %+v", res)

	// a regra automática daria outro mês; fixa o mês do gabarito
	if _, err := db.Exec(`INSERT INTO ti_config (chave, valor) VALUES ('ultimo_mes_fechado', $1)`, want.Meta.LastClosed); err != nil {
		t.Fatal(err)
	}
	got, err := Carregar(app, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range comparar(got, &want) {
		t.Error(d)
	}
	t.Logf("banco -> modelo: budget=%d lanc=%d cf=%d", len(got.Budget), len(got.Lanc), len(got.CF))
}

func TestIntegracao_KSB1SubstituiSoOsMesesQueCobre(t *testing.T) {
	db := bancoDeTeste(t)
	app := comoApp(t)

	grava := func(linhas []KSB1Linha) {
		t.Helper()
		var res ResumoLeituraSAP
		res.Linhas, res.Aproveitadas = len(linhas), len(linhas)
		for _, l := range linhas {
			m := l.DataLanc[:7]
			if res.PrimeiroMes == "" || m < res.PrimeiroMes {
				res.PrimeiroMes = m
			}
			if m > res.UltimoMes {
				res.UltimoMes = m
			}
		}
		if _, err := ImportarKSB1(app, linhas, res, "", "ksb1.xlsx"); err != nil {
			t.Fatal(err)
		}
	}
	conta := func(q string) int {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	grava([]KSB1Linha{
		sap("P1", ccA, "2026-01-10", 10, "F"), sap("P1", ccA, "2026-02-10", 20, "F"), sap("P1", ccA, "2026-03-10", 30, "F"),
	})
	if n := conta(`SELECT count(*) FROM ti_ksb1`); n != 3 {
		t.Fatalf("1ª carga: %d linhas", n)
	}

	// 2ª carga cobre só fevereiro, com valor corrigido e uma linha nova: jan e mar não mudam
	grava([]KSB1Linha{sap("P1", ccA, "2026-02-12", 25, "F"), sap("P2", ccB, "2026-02-20", 5, "G")})
	if n := conta(`SELECT count(*) FROM ti_ksb1 WHERE data_lancamento < '2026-02-01' OR data_lancamento > '2026-02-28'`); n != 2 {
		t.Errorf("jan e mar deveriam continuar (2 linhas), há %d", n)
	}
	var fev float64
	_ = db.QueryRow(`SELECT COALESCE(SUM(valor),0)::float8 FROM ti_ksb1 WHERE data_lancamento BETWEEN '2026-02-01' AND '2026-02-28'`).Scan(&fev)
	if fev != 30 {
		t.Errorf("fevereiro deveria ter sido substituído (25+5=30), soma %.2f", fev)
	}
	if n := conta(`SELECT count(*) FROM ti_cargas_sap WHERE tipo='ksb1'`); n != 2 {
		t.Errorf("histórico de cargas = %d", n)
	}

	// carga com valor impossível de gravar volta inteira e preserva o que existia
	ruim := []KSB1Linha{sap("P3", ccA, "2026-02-15", 1, "H")}
	ruim[0].DataLanc = "isto-nao-e-data"
	if _, err := ImportarKSB1(app, ruim, ResumoLeituraSAP{Linhas: 1, Aproveitadas: 1, PrimeiroMes: "2026-02", UltimoMes: "2026-02"}, "", "ruim.xlsx"); err == nil {
		t.Fatal("data inválida deveria falhar")
	}
	if n := conta(`SELECT count(*) FROM ti_ksb1 WHERE data_lancamento BETWEEN '2026-02-01' AND '2026-02-28'`); n != 2 {
		t.Errorf("falha na carga apagou fevereiro: %d linhas (esperado 2)", n)
	}
	if n := conta(`SELECT count(*) FROM ti_cargas_sap`); n != 2 {
		t.Errorf("carga que falhou entrou no histórico: %d", n)
	}
}

func TestIntegracao_KSB2SubstituiTudoESaldoEntraNoModelo(t *testing.T) {
	db := bancoDeTeste(t)
	app := comoApp(t)
	pl, err := LerPlanilha(planilhaSintetica(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Importar(app, pl, "", "p.xlsx"); err != nil {
		t.Fatal(err)
	}
	// a planilha sintética tem o lançamento A LANÇAR sem pedido; dá um pedido a ele
	if _, err := db.Exec(`UPDATE ti_lancamentos SET pedido = 'P77' WHERE status = 'A LANÇAR'`); err != nil {
		t.Fatal(err)
	}

	k1 := []KSB2Linha{{Pedido: "P77", CCSap: "IMOB", Conta: "1230110001", Valor: 40, Item: "10"}, {Pedido: "P77", CCSap: "IMOB", Conta: "1230110001", Valor: 2.5, Item: "20"}}
	if _, err := ImportarKSB2(app, k1, ResumoLeituraSAP{Linhas: 2, Aproveitadas: 2}, "", "ksb2.xlsx"); err != nil {
		t.Fatal(err)
	}
	m, err := Carregar(app, time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var saldo *float64
	for _, l := range m.Lanc {
		if l.Pedido == "P77" {
			saldo = l.SaldoSap
		}
	}
	if saldo == nil || *saldo != 42.5 {
		t.Fatalf("saldo da KSB2 no item a lançar = %v, esperado 42,50 (soma dos itens do pedido)", saldo)
	}
	if m.Meta.Sap.Ksb2Em == "" {
		t.Error("meta.sap.ksb2Em deveria dizer quando a KSB2 foi carregada")
	}

	// nova KSB2 sem esse pedido: a foto antiga some
	if _, err := ImportarKSB2(app, []KSB2Linha{{Pedido: "P99", CCSap: ccA, Conta: "1", Valor: 1}}, ResumoLeituraSAP{Linhas: 1, Aproveitadas: 1}, "", "ksb2b.xlsx"); err != nil {
		t.Fatal(err)
	}
	m, _ = Carregar(app, time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC))
	for _, l := range m.Lanc {
		if l.Pedido == "P77" && l.SaldoSap != nil {
			t.Errorf("a KSB2 nova substitui tudo; P77 não deveria mais ter saldo: %v", *l.SaldoSap)
		}
	}
}

// Ciclo completo com os arquivos REAIS: planilha + KSB1 + KSB2 -> banco -> modelo e conferência.
func TestIntegracao_CicloSAPComArquivosReais(t *testing.T) {
	pf, p1, p2 := os.Getenv("TI_PLANILHA"), os.Getenv("TI_KSB1"), os.Getenv("TI_KSB2")
	if pf == "" || p1 == "" || p2 == "" {
		t.Skip("defina TI_PLANILHA, TI_KSB1 e TI_KSB2")
	}
	bancoDeTeste(t)
	app := comoApp(t)

	dPl, _ := os.ReadFile(pf)
	pl, err := LerPlanilha(dPl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Importar(app, pl, "", "forecast.xlsx"); err != nil {
		t.Fatal(err)
	}
	d1, _ := os.ReadFile(p1)
	l1, r1, err := LerKSB1(d1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportarKSB1(app, l1, r1, "", "ksb1.xlsx"); err != nil {
		t.Fatal(err)
	}
	d2, _ := os.ReadFile(p2)
	l2, r2, err := LerKSB2(d2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportarKSB2(app, l2, r2, "", "ksb2.xlsx"); err != nil {
		t.Fatal(err)
	}

	agora := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	conf, err := Conferencia(app, agora)
	if err != nil {
		t.Fatal(err)
	}
	// o mesmo resultado que a conferência feita em memória sobre os mesmos arquivos
	m := modeloDaPlanilha(t, pf, MesFechado(agora, ""))
	mem := Conferir(m.Lanc, l1)
	if conf.Resumo.Total != mem.Resumo.Total || conf.Resumo.Conferidos != mem.Resumo.Conferidos {
		t.Errorf("banco: %d grupos / %d conferidos; memória: %d / %d", conf.Resumo.Total, conf.Resumo.Conferidos, mem.Resumo.Total, mem.Resumo.Conferidos)
	}
	for _, s := range Situacoes {
		if conf.Resumo.Por[s.Codigo] != mem.Resumo.Por[s.Codigo] {
			t.Errorf("%s: banco %+v, memória %+v", s.Codigo, conf.Resumo.Por[s.Codigo], mem.Resumo.Por[s.Codigo])
		}
	}
	t.Logf("conferência pelo banco: %d grupos, %d conferidos (%.1f%% da base)", conf.Resumo.Total, conf.Resumo.Conferidos, 100*conf.Resumo.PctConferido)

	modelo, err := Carregar(app, agora)
	if err != nil {
		t.Fatal(err)
	}
	com := 0
	for _, l := range modelo.Lanc {
		if l.SaldoSap != nil {
			com++
		}
	}
	t.Logf("itens a lançar com saldo da KSB2: %d | KSB1 %s a %s | oficiais=%v provisórios=%v", com, modelo.Meta.Sap.Ksb1De, modelo.Meta.Sap.Ksb1Ate, modelo.Meta.Sap.Oficiais, modelo.Meta.Sap.Provisorios)
	if com == 0 {
		t.Error("nenhum item a lançar ganhou saldo da KSB2")
	}
	// 2026: o SAP só confere; o realizado do painel continua o da planilha
	if len(modelo.Meta.Sap.Oficiais) != 0 {
		t.Errorf("em 2026 nenhum mês é oficial do SAP: %v", modelo.Meta.Sap.Oficiais)
	}
}
