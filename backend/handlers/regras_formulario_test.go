package handlers

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"fb_apu05/internal/sapodata"
)

// Os testes do handler de abertura usam sqlmock sem simular o SAP: a conferência de saldo é testada aqui.
func init() {
	conferirSaldoOrigem = func(context.Context, *sql.DB, *sql.Tx, []lancamentoValidado, string) (string, error) { return "", nil }
}

func mesT(a, m int) time.Time { return time.Date(a, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }

func TestPodeRemanejar_MenorEntreMesEAcumulado_NuncaNegativo(t *testing.T) {
	casos := []struct{ mes, acum, quer float64 }{
		{0, 20000, 0},       // junho do exemplo: nada pode sair
		{5000, 20000, 5000}, // maio do exemplo: até 5 mil
		{8000, 3000, 3000},
		{-100, 5000, 0},
		{500, -10, 0},
	}
	for _, c := range casos {
		if got := podeRemanejar(c.mes, c.acum); got != c.quer {
			t.Errorf("podeRemanejar(%v,%v)=%v, quer %v", c.mes, c.acum, got, c.quer)
		}
	}
}

func TestVerificarSaldoOrigem_BloqueiaMesNegativoMesmoComAcumuladoEReserva(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	var disp, acum [12]float64
	disp[4], acum[4] = 5000, 20000 // mai
	disp[5], acum[5] = 0, 20000    // jun
	consultarSaldoSAP = func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
		return &sapodata.Saldo{Encontrado: true, Disponivel: disp, DispAcum: acum}, nil
	}
	// reservas: cada linha = (mês, valor, número do chamado)
	rodar := func(reservas [][3]any, linhas ...lancamentoValidado) string {
		t.Helper()
		mock.ExpectBegin()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		esperaBaseSaldo2(mock)
		mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo FROM centros_custo WHERE id = $1")).WillReturnRows(sqlmock.NewRows([]string{"codigo"}).AddRow("10026040"))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo FROM contas WHERE id = $1")).WillReturnRows(sqlmock.NewRows([]string{"codigo"}).AddRow("4210100001"))
		rs := sqlmock.NewRows([]string{"m", "v", "n"})
		for _, r := range reservas {
			rs.AddRow(r[0], r[1], r[2])
		}
		mock.ExpectQuery("FROM solicitacao_lancamentos l JOIN solicitacoes s").WillReturnRows(rs)
		msg, err := verificarSaldoOrigem(context.Background(), db, tx, linhas, "")
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectRollback()
		_ = tx.Rollback()
		return msg
	}
	if msg := rodar(nil, lancamentoValidado{Lado: "origem", CentroCustoID: sCC1, ContaID: sConta, Mes: mesT(2026, 6), Valor: 100},
		lancamentoValidado{Lado: "destino", CentroCustoID: sCC2, ContaID: sConta, Mes: mesT(2026, 6), Valor: 100}); !strings.Contains(msg, "jun/2026") {
		t.Fatalf("junho sem disponível no mês deve ser recusado: %q", msg)
	}
	if msg := rodar(nil, lancamentoValidado{Lado: "origem", CentroCustoID: sCC1, ContaID: sConta, Mes: mesT(2026, 5), Valor: 5000}); msg != "" {
		t.Fatalf("maio até 5 mil passa: %q", msg)
	}
	if msg := rodar(nil, lancamentoValidado{Lado: "origem", CentroCustoID: sCC1, ContaID: sConta, Mes: mesT(2026, 5), Valor: 5000.02}); !strings.Contains(msg, "mai/2026") {
		t.Fatalf("maio acima de 5 mil é recusado: %q", msg)
	}
	// F3: GO-34 já reserva 4.900 em maio → sobram 100; um novo chamado de 200 é recusado citando o GO-34
	msg := rodar([][3]any{{5, 4900.0, int64(34)}}, lancamentoValidado{Lado: "origem", CentroCustoID: sCC1, ContaID: sConta, Mes: mesT(2026, 5), Valor: 200})
	if !strings.Contains(msg, "GO-34") || !strings.Contains(msg, "R$ 100,00") {
		t.Fatalf("a reserva do GO-34 deveria segurar o saldo: %q", msg)
	}
	if msg := rodar([][3]any{{5, 4900.0, int64(34)}}, lancamentoValidado{Lado: "origem", CentroCustoID: sCC1, ContaID: sConta, Mes: mesT(2026, 5), Valor: 100}); msg != "" {
		t.Fatalf("100 cabe no que sobra: %q", msg)
	}
}

func esperaBaseSaldo2(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT chave, valor FROM configuracoes")).WillReturnRows(sqlmock.NewRows([]string{"chave", "valor"}).
		AddRow("sap_bg_ativo", "true").AddRow("sap_bg_url_base", "https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_trava_orcamentaria/srvd_a2x/sap/zsd_trava_orcamentaria/0001").
		AddRow("sap_usuario", "u").AddRow("sap_senha", ""))
}

func TestTransferencia_UmaContaPorLado(t *testing.T) {
	base := []lancamentoValidado{
		{Lado: "origem", CentroCustoID: "a", ContaID: "c1", Mes: mesT(2026, 1), Valor: 1},
		{Lado: "origem", CentroCustoID: "a", ContaID: "c1", Mes: mesT(2026, 2), Valor: 1},
		{Lado: "destino", CentroCustoID: "b", ContaID: "c2", Mes: mesT(2026, 3), Valor: 1},
		{Lado: "destino", CentroCustoID: "b", ContaID: "c2", Mes: mesT(2026, 4), Valor: 1},
	}
	if m := validarTransferenciaUmaContaPorLado(base); m != "" {
		t.Fatalf("meses diferentes na mesma conta valem: %s", m)
	}
	duas := append([]lancamentoValidado{}, base...)
	duas[1].ContaID = "c9"
	if m := validarTransferenciaUmaContaPorLado(duas); !strings.Contains(m, "única conta na origem") {
		t.Fatalf("2 contas na origem: %q", m)
	}
	dest := append([]lancamentoValidado{}, base...)
	dest[3].ContaID = "c9"
	if m := validarTransferenciaUmaContaPorLado(dest); !strings.Contains(m, "única conta no destino") {
		t.Fatalf("2 contas no destino: %q", m)
	}
	rep := append([]lancamentoValidado{}, base...)
	rep[1].Mes = mesT(2026, 1)
	if m := validarTransferenciaUmaContaPorLado(rep); !strings.Contains(m, "duas vezes") {
		t.Fatalf("mês repetido: %q", m)
	}
}

func TestDescricaoObrigatoria(t *testing.T) {
	if validarDescricao("transferencia", "   ") != "Informe a descrição." ||
		validarDescricao("imobilizado", "") != "Informe a descrição do imobilizado." ||
		validarDescricao("obras", "\n") != "Informe o detalhe." {
		t.Fatal("mensagens por formulário")
	}
	if validarDescricao("inclusao", strings.Repeat("x", MaxDescricao+1)) == "" {
		t.Fatal("acima do limite deve recusar")
	}
}

func TestCriarObras_SoOndeAOrdemNasce(t *testing.T) {
	ret := []obraLinhaValidado{{OrdemInvestimento: "CRIAR", Lado: sql.NullString{String: "retirada", Valid: true}}}
	inc := []obraLinhaValidado{{OrdemInvestimento: "CRIAR", Lado: sql.NullString{String: "inclusao", Valid: true}}}
	if validarCriarObras("retirada_saldo", []obraLinhaValidado{{OrdemInvestimento: "CRIAR"}}) == "" ||
		validarCriarObras(classificacaoTransferenciaSaldo, ret) == "" {
		t.Fatal("CRIAR na retirada deve recusar")
	}
	if validarCriarObras(classificacaoTransferenciaSaldo, inc) != "" || validarCriarObras("inclusao", []obraLinhaValidado{{OrdemInvestimento: "CRIAR"}}) != "" || validarCriarObras("fl", []obraLinhaValidado{{OrdemInvestimento: "CRIAR"}}) != "" {
		t.Fatal("CRIAR vale na inclusão, FL e destino")
	}
}

func TestNomeAnexoSeguro(t *testing.T) {
	casos := map[string]string{
		`C:\fakepath\cotação.pdf`:   "cotação.pdf",
		"../../etc/passwd":          "passwd",
		"a<b>:c|d?.pdf":             "a_b__c_d_.pdf",
		"  nome\u0000com\tctrl.pdf": "nome_com_ctrl.pdf",
		"...":                       "anexo",
	}
	for in, quer := range casos {
		if got := nomeAnexoSeguro(in); got != quer {
			t.Errorf("nomeAnexoSeguro(%q)=%q, quer %q", in, got, quer)
		}
	}
	longo := strings.Repeat("a", 300) + ".pdf"
	if got := nomeAnexoSeguro(longo); len([]rune(got)) != 150 || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("longo: %d %q", len(got), got[len(got)-8:])
	}
}

func TestTotalImobilizado(t *testing.T) {
	if totalImobilizado(3, 1234.567) != 3703.70 || totalImobilizado(2, 0.005) != 0.01 {
		t.Fatalf("%v %v", totalImobilizado(3, 1234.567), totalImobilizado(2, 0.005))
	}
}

func init() {
	conferirNaoCadastrado = func(consultaRowCtx, sql.NullString, bool) (string, error) { return "", nil }
	conferirGraoObras = func(*sql.Tx, string, []obraLinhaValidado) (string, error) { return "", nil }
	conferirTipoDestino = func(consultaRowCtx, []lancamentoValidado, string) (string, error) { return "", nil }
	avisarAcompanhantes = func(*sql.DB, string, string, string, string) {}
}

func init() {
	avisarInclusaoEmCopia = func(*sql.DB, string, string, []string, []string) {}
}
