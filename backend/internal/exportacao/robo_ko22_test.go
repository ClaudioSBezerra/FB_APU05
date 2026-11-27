package exportacao

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func lerAba(t *testing.T, arq []byte, caminho string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(arq), int64(len(arq)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == caminho {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatalf("%s não está no arquivo", caminho)
	return ""
}

func TestRoboKO22_PreencheIncluir(t *testing.T) {
	tpl, err := os.ReadFile("templates/lote_obra.xlsm")
	if err != nil {
		t.Skip("modelo do robô ausente")
	}
	if !templateTemAba(tpl, abaRoboKO22) {
		t.Fatal("modelo sem aba incluir")
	}
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`SELECT numero, ano_orcamento FROM solicitacoes`).WithArgs("s1").
		WillReturnRows(sqlmock.NewRows([]string{"numero", "ano_orcamento"}).AddRow(75, 2026))
	linhas := []linhaExportacaoObra{
		{SolicitacaoID: "s1", Classificacao: "transferencia_saldo", OrdemInvestimento: "602492", Valor: 1000, Lado: sql.NullString{String: "retirada", Valid: true}},
		{SolicitacaoID: "s1", Classificacao: "transferencia_saldo", OrdemInvestimento: "602484", Valor: 1000, Lado: sql.NullString{String: "inclusao", Valid: true}},
	}
	arq, err := montarLoteRoboKO22(db, tpl, linhas)
	if err != nil {
		t.Fatal(err)
	}
	x := lerAba(t, arq, "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<c r="A6" s="2"><v>602492</v></c>`, `<c r="B6" s="3"><v>2026</v></c>`, `<c r="C6" t="inlineStr"><is><t xml:space="preserve">transferencia_saldo</t></is></c>`,
		`<c r="D6" s="1"><v>-1000</v></c>`, `GO-75`, `<c r="D7" s="1"><v>1000</v></c>`,
	} {
		if !strings.Contains(x, want) {
			t.Errorf("faltou %s", want)
		}
	}
	if strings.Count(x, "<row ") != strings.Count(lerAba(t, tpl, "xl/worksheets/sheet1.xml"), "<row ") {
		t.Error("número de linhas da aba mudou")
	}
	if !strings.Contains(lerAba(t, arq, "xl/workbook.xml"), "fullCalcOnLoad") {
		t.Error("sem recálculo ao abrir")
	}
	// vbaProject.bin intacto
	if !bytes.Equal([]byte(lerAba(t, arq, "xl/vbaProject.bin")), []byte(lerAba(t, tpl, "xl/vbaProject.bin"))) {
		t.Error("vbaProject.bin mudou")
	}
	_ = os.WriteFile(os.TempDir()+"/lote_ko22_teste.xlsm", arq, 0o644)
}

func TestRoboKO22_Duplicidade(t *testing.T) {
	tpl, err := os.ReadFile("templates/lote_obra.xlsm")
	if err != nil {
		t.Skip()
	}
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`SELECT numero`).WillReturnRows(sqlmock.NewRows([]string{"numero", "ano_orcamento"}).AddRow(7, nil))
	l := linhaExportacaoObra{SolicitacaoID: "s1", Classificacao: "inclusao", OrdemInvestimento: "602492", Valor: 10}
	if _, err := montarLoteRoboKO22(db, tpl, []linhaExportacaoObra{l, l}); !errorsIsRobo(err) {
		t.Fatalf("esperava ErrLoteRobo, veio %v", err)
	}
}

func TestValorETipoKO22(t *testing.T) {
	casos := []struct {
		cl, lado string
		v        float64
		tipo     string
	}{{"inclusao", "", 5, "inclusao"}, {"fl", "", 5, "fl"}, {"retirada_saldo", "", -5, "retirada_saldo"}, {"transferencia_saldo", "retirada", -5, "transferencia_saldo"}, {"transferencia_saldo", "inclusao", 5, "transferencia_saldo"}}
	for _, c := range casos {
		v, tp := valorETipoKO22(c.cl, c.lado, 5)
		if v != c.v || tp != c.tipo {
			t.Errorf("%s/%s: %v %s", c.cl, c.lado, v, tp)
		}
	}
}

func TestRoboKP06_PreencheChamadosEOperacoes(t *testing.T) {
	tpl, err := os.ReadFile("templates/lote_despesa.xlsm")
	if err != nil {
		t.Skip("modelo do robô ausente")
	}
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`SELECT s.numero, s.tipo_solicitacao`).WithArgs("s1", "adm").
		WillReturnRows(sqlmock.NewRows([]string{"numero", "tipo", "sol", "resp", "aut", "desc", "forn", "tit"}).
			AddRow(80, "transferencia", "Maria", "Ana", "Wilker", "Treinamento", "Educa", "Treinamento"))
	mes := sqlmock.NewRows([]string{"lado", "d", "cc", "conta", "mes", "valor"}).
		AddRow("origem", "0101", "6040", "4210400004", mustData("2026-08-01"), 1500.0).
		AddRow("destino", "0101", "6040", "4210400003", mustData("2026-08-01"), 1500.0)
	mock.ExpectQuery(`FROM solicitacao_lancamentos`).WithArgs("s1").WillReturnRows(mes)
	arq, err := montarLoteRoboKP06(db, tpl, []string{"s1"}, "adm")
	if err != nil {
		t.Fatal(err)
	}
	ch := lerAba(t, arq, "xl/worksheets/sheet2.xml")
	op := lerAba(t, arq, "xl/worksheets/sheet3.xml")
	for _, w := range []string{"GO-80", "TRANSFERÊNCIA", "Wilker", ">NÃO<"} {
		if !strings.Contains(ch, w) {
			t.Errorf("Chamados sem %s", w)
		}
	}
	if strings.Contains(ch, "GO-4") || strings.Contains(op, "GO-14") {
		t.Error("ficaram dados de demonstração")
	}
	for _, w := range []string{"GO-80-L01", "GO-80-L02", "<v>-1500</v>", "ORIGEM", "DESTINO", ">0101<", "<v>4210400004</v>", "PENDENTE", "DUPLICADO"} {
		if !strings.Contains(op, w) {
			t.Errorf("Operacoes sem %s", w)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	_ = os.WriteFile(os.TempDir()+"/lote_kp06_teste.xlsm", arq, 0o644)
}

func mustData(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
