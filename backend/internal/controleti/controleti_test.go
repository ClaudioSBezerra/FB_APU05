package controleti

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// serial devolve o número de série do Excel para AAAA-MM-DD.
func serial(t *testing.T, iso string) int {
	t.Helper()
	d, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t.Fatal(err)
	}
	return int(d.Sub(baseExcel).Hours() / 24)
}

// planilhaSintetica monta um .xlsx mínimo com as 3 abas que o importador lê,
// cabeçalhos em maiúsculas/sem acento para exercitar a normalização.
func planilhaSintetica(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	set := func(sheet, cell string, v any) {
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}

	// --- Parâmetros: 4 tabelas lado a lado, cabeçalho na linha 5
	f.SetSheetName("Sheet1", "Parâmetros")
	p := "Parâmetros"
	set(p, "B4", "Último mês fechado")
	for c, h := range []string{"CC", "Descrição Centro de Custo", "Responsável", "Pasta"} {
		cel, _ := excelize.CoordinatesToCellName(2+c, 5)
		set(p, cel, h)
	}
	set(p, "B6", "6150")
	set(p, "C6", "TI - Filial")
	set(p, "D6", "Welton")
	set(p, "E6", "6150 TI Filial")
	set(p, "B7", "6720")
	set(p, "C7", "TI Infra")
	for c, h := range []string{"Conta", "Descrição Conta", "Tipo de Lançamento"} {
		cel, _ := excelize.CoordinatesToCellName(7+c, 5)
		set(p, cel, h)
	}
	set(p, "G6", "4222000020")
	set(p, "H6", "Despesa Software")
	set(p, "I6", "Software e Licenças")
	for c, h := range []string{"Status", "Classificação"} {
		cel, _ := excelize.CoordinatesToCellName(11+c, 5)
		set(p, cel, h)
	}
	set(p, "K6", "LANÇADO")
	set(p, "L6", "Realizado")
	set(p, "K7", "A LANÇAR")
	set(p, "L7", "Comprometido")
	for c, h := range []string{"Divisão", "Descrição Divisão", "Empresa", "Pasta"} {
		cel, _ := excelize.CoordinatesToCellName(14+c, 5)
		set(p, cel, h)
	}
	set(p, "N6", "1002")
	set(p, "O6", "IMBIRIBEIRA")
	set(p, "P6", "Ferreira Costa")

	// --- Orçamento Forecast: 3 meses
	o := "Orçamento Forecast"
	f.NewSheet(o)
	for c, h := range []string{"ID", "Origem", "Divisão", "CC", "Desc. CC", "Conta", "Desc. Conta", "Tipo de Lançamento", "Fornecedor", "Descrição"} {
		cel, _ := excelize.CoordinatesToCellName(1+c, 4)
		set(o, cel, h)
	}
	for j, m := range []string{"2026-01-01", "2026-02-01", "2026-03-01"} {
		cel, _ := excelize.CoordinatesToCellName(11+j, 4)
		set(o, cel, serial(t, m))
	}
	set(o, "A5", 1)
	set(o, "B5", "Orç 2026")
	set(o, "C5", "1002")
	set(o, "D5", "6150")
	set(o, "F5", "4222000020")
	set(o, "I5", "ACME")
	set(o, "J5", "Licenças")
	set(o, "K5", 1000)
	set(o, "L5", 1000)
	set(o, "M5", 1000)
	set(o, "B6", "Total") // linha de total é descartada
	set(o, "A6", 99)
	set(o, "K6", 3000)

	// --- Lançamentos Mensais
	l := "Lançamentos Mensais"
	f.NewSheet(l)
	for c, h := range []string{"ID", "Origem", "Status", "Pedido", "Folha", "MIRO", "NF/Fatura", "Competência", "Vencimento", "Valor", "Fornecedor", "Descrição", "CC SAP", "Conta Contábil"} {
		cel, _ := excelize.CoordinatesToCellName(1+c, 4)
		set(l, cel, h)
	}
	set(l, "A5", 1)
	set(l, "B5", "DESPESAS TI")
	set(l, "C5", "lançado") // minúsculo: o status é gravado em maiúsculas
	set(l, "D5", "4522894258")
	set(l, "H5", serial(t, "2026-01-01"))
	set(l, "I5", serial(t, "2026-01-26"))
	set(l, "J5", 700.5)
	set(l, "K5", "ACME")
	set(l, "L5", "Nota janeiro")
	set(l, "M5", "10026150")
	set(l, "N5", "4222000020")
	// linha só com fórmulas/lixo: sem ID, fornecedor nem valor -> descartada
	set(l, "O7", "x")
	// A LANÇAR sem competência
	set(l, "A8", 2)
	set(l, "C8", "A LANÇAR")
	set(l, "J8", "1.234,5") // texto não numérico "BR" vira 0 (como no parser original)
	set(l, "K8", "ACME")
	set(l, "M8", "IMOB")

	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLerPlanilha_Sintetica(t *testing.T) {
	pl, err := LerPlanilha(planilhaSintetica(t))
	if err != nil {
		t.Fatal(err)
	}

	if got := pl.Meses; len(got) != 3 || got[0] != "2026-01" || got[2] != "2026-03" {
		t.Fatalf("meses = %v", got)
	}
	if len(pl.Orcamento) != 1 {
		t.Fatalf("orçamento = %d linhas, esperado 1 (a linha Total é descartada)", len(pl.Orcamento))
	}
	if o := pl.Orcamento[0]; o.CC != "6150" || o.Valores[0] != 1000 || o.Valores[2] != 1000 || o.Fornecedor != "ACME" {
		t.Fatalf("linha de orçamento inesperada: %+v", o)
	}

	if len(pl.Lancamentos) != 2 {
		t.Fatalf("lançamentos = %d, esperado 2 (linha-lixo descartada)", len(pl.Lancamentos))
	}
	a := pl.Lancamentos[0]
	if a.Status != "LANÇADO" || a.Competencia != "2026-01-01" || a.Vencimento != "2026-01-26" || a.Valor != 700.5 || a.Pedido != "4522894258" {
		t.Fatalf("lançamento 1 inesperado: %+v", a)
	}
	b := pl.Lancamentos[1]
	if b.Competencia != "" || b.Valor != 0 {
		t.Fatalf("lançamento 2: competência vazia e valor 0 esperados: %+v", b)
	}

	if v := pl.Params.CC["6150"]; v.Descricao != "TI - Filial" || v.Responsavel != "Welton" || v.Pasta != "6150 TI Filial" {
		t.Fatalf("T_CC: %+v", v)
	}
	if v := pl.Params.CC["6720"]; v.Descricao != "TI Infra" {
		t.Fatalf("T_CC 6720: %+v", v)
	}
	if v := pl.Params.Conta["4222000020"]; v.Tipo != "Software e Licenças" {
		t.Fatalf("T_Conta: %+v", v)
	}
	if pl.Params.Status["LANÇADO"] != "Realizado" || pl.Params.Status["A LANÇAR"] != "Comprometido" {
		t.Fatalf("T_Status: %+v", pl.Params.Status)
	}
	if v := pl.Params.Divisao["1002"]; v.Descricao != "IMBIRIBEIRA" || v.Empresa != "Ferreira Costa" {
		t.Fatalf("T_Divisao: %+v", v)
	}
}

func TestLerPlanilha_Rejeita(t *testing.T) {
	if _, err := LerPlanilha([]byte("isto não é uma planilha")); err == nil {
		t.Fatal("texto puro deveria ser rejeitado")
	}
	if _, err := LerPlanilha([]byte("PK\x03\x04quebrado")); err == nil {
		t.Fatal("zip quebrado deveria ser rejeitado")
	}

	// planilha válida, mas sem as abas esperadas
	f := excelize.NewFile()
	var buf bytes.Buffer
	_, _ = f.WriteTo(&buf)
	if _, err := LerPlanilha(buf.Bytes()); err == nil {
		t.Fatal("planilha sem a aba Parâmetros deveria ser rejeitada")
	}
}

func TestMontar_CamposDerivadosEProjecao(t *testing.T) {
	pl, err := LerPlanilha(planilhaSintetica(t))
	if err != nil {
		t.Fatal(err)
	}
	orc := []OrcDB{}
	for _, o := range pl.Orcamento {
		v := map[string]float64{}
		for j, ym := range pl.Meses {
			v[ym] = o.Valores[j]
		}
		orc = append(orc, OrcDB{Ref: o.Ref, Divisao: o.Divisao, CC: o.CC, Conta: o.Conta, Valores: v})
	}
	m := Montar(Entrada{Params: pl.Params, Orcamento: orc, Lancamentos: pl.Lancamentos, LastClosed: "2026-01"})

	// divisão e CC saem do CC SAP; descrições e tipo, dos parâmetros
	l := m.Lanc[0]
	if l.Div != "1002" || l.CC != "6150" || l.DivDesc != "IMBIRIBEIRA" || l.CCDesc != "TI - Filial" ||
		l.ContaDesc != "Despesa Software" || l.Tipo != "Software e Licenças" || l.Classif != "Realizado" {
		t.Fatalf("derivados do lançamento 1: %+v", l)
	}
	// IMOB: divisão N/D; cadastro ausente vira "Verificar"; status sem
	// parâmetro seria "Comprometido"
	i := m.Lanc[1]
	if i.Div != "N/D" || i.CC != "IMOB" || i.DivDesc != "Verificar" || i.Tipo != "Não classificado" || i.Classif != "Comprometido" {
		t.Fatalf("derivados do lançamento IMOB: %+v", i)
	}
	if i.Comp != nil || i.Venc != nil {
		t.Fatalf("datas vazias deveriam ser nulas: %+v", i)
	}

	// projeção: janeiro fechado = realizado (700,50); fev/mar abertos = o
	// maior entre orçado (1000) e lançado (0)
	if len(m.CF) != 1 {
		t.Fatalf("cf = %d linhas", len(m.CF))
	}
	cf := m.CF[0]
	if cf.Key != "1002|6150|4222000020" || cf.M[0] != 700.5 || cf.M[1] != 1000 || cf.M[2] != 1000 {
		t.Fatalf("projeção inesperada: %+v", cf)
	}
	// grupo (empresa) vem junto de cada linha: 1002 está em "Ferreira Costa"; IMOB também
	if m.Lanc[0].Grp != "FC" || m.Lanc[1].Grp != "FC" || m.Budget[0].Grp != "FC" || cf.Grp != "FC" {
		t.Fatalf("grupos: lanc=%q/%q budget=%q cf=%q", m.Lanc[0].Grp, m.Lanc[1].Grp, m.Budget[0].Grp, cf.Grp)
	}
	if m.Meta.RefDate != "2026-01-31" {
		t.Fatalf("refDate = %s", m.Meta.RefDate)
	}
}

func TestMesFechado(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	casos := []struct {
		hoje, override, want string
	}{
		{"2026-11-16", "", "2026-10"}, // dia >= 6: o mês anterior está fechado
		{"2026-11-06", "", "2026-10"}, // dia 6 já fecha
		{"2026-11-05", "", "2026-09"}, // dia 5: o SAP ainda aceita retroativo
		{"2026-11-01", "", "2026-09"},
		{"2027-01-10", "", "2026-12"}, // virada de ano
		{"2027-01-03", "", "2026-11"},
		{"2026-11-16", "2026-08", "2026-08"}, // ajuste manual vence
		{"2026-11-16", "lixo", "2026-10"},    // ajuste inválido é ignorado
	}
	for _, c := range casos {
		if got := MesFechado(d(c.hoje), c.override); got != c.want {
			t.Errorf("MesFechado(%s, %q) = %s, esperado %s", c.hoje, c.override, got, c.want)
		}
	}
}

func TestDivCCDoSap(t *testing.T) {
	casos := []struct{ in, div, cc string }{
		{"10026770", "1002", "6770"},
		{"40166151", "4016", "6151"},
		{"IMOB", "N/D", "IMOB"},
		{"N/D", "N/D", "N/D"},
		{"6150", "N/D", "6150"}, // curto (< 5): sem divisão
		{"", "", ""},
	}
	for _, c := range casos {
		if d, cc := DivCCDoSap(c.in); d != c.div || cc != c.cc {
			t.Errorf("DivCCDoSap(%q) = (%q, %q), esperado (%q, %q)", c.in, d, cc, c.div, c.cc)
		}
	}
}

func TestGrupoDaDivisao(t *testing.T) {
	casos := []struct{ div, desc, emp, want string }{
		{"1002", "IMBIRIBEIRA", "", "FC"},
		{"91", "CD Cabo", "", "FC"},
		{"1009", "BARRIS", "", "FC"},
		{"4016", "RETAILX LABS", "", "RX"},
		{"9090", "IMPORTADORA [ FCM ]", "", "IMP"},
		{"N/D", "IMOB", "", "FC"},
		{"1500", "", "", "OUT"},
		{"9999", "", "RetailX", "RX"}, // a tabela de divisões vale quando informa a empresa
		{"4016", "", "Ferreira Costa", "FC"},
	}
	for _, c := range casos {
		if got := GrupoDaDivisao(c.div, c.desc, c.emp); got != c.want {
			t.Errorf("GrupoDaDivisao(%q,%q,%q) = %s, esperado %s", c.div, c.desc, c.emp, got, c.want)
		}
	}
}
