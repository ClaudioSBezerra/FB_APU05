package controleti

import (
	"fmt"
	"testing"
)

func paramsStatus() Params {
	return Params{
		CC: map[string]ParamCC{}, Conta: map[string]ParamConta{}, Divisao: map[string]ParamDivisao{}, Fornecedor: map[string]string{},
		Status: map[string]string{"LANÇADO": "Realizado", "ESTORNADO": "Realizado", "A LANÇAR": "Comprometido"},
	}
}

func planilhaL(ref, pedido, ccSap, comp string, valor float64, status string) Lanc {
	return Lanc{Ref: ref, Pedido: pedido, CCSap: ccSap, Competencia: comp + "-01", Valor: valor, Status: status, Fornecedor: "F" + ref, Conta: "4222000020"}
}

func realizadoPorMes(m *Modelo) map[string]float64 {
	r := map[string]float64{}
	for _, l := range m.Lanc {
		if l.Classif == "Realizado" && l.Comp != nil && l.Kind != "X" {
			r[*l.Comp] += l.Valor
		}
	}
	return r
}

func TestSAPOficial_SubstituiSoOQueDeve(t *testing.T) {
	lancs := []Lanc{
		planilhaL("1", "P1", ccA, "2027-01", 100, "LANÇADO"), // com pedido, mês oficial: SAP substitui
		planilhaL("2", "", "IMOB", "2027-01", 50, "LANÇADO"), // sem pedido: fica (imobilizado/itens sem pedido)
		planilhaL("3", "P2", ccA, "2027-01", 30, "A LANÇAR"), // a lançar: fica
		planilhaL("4", "P1", ccA, "2026-12", 70, "LANÇADO"),  // antes de 2027: fica como está
		planilhaL("5", "P3", ccA, "2027-02", 20, "LANÇADO"),  // mês fechado SEM cobertura da KSB1: provisório, fica
	}
	ksb := []KSB1Linha{
		sap("P1", ccA, "2027-01-10", 105, "FORN SAP"),
		sap("P1", ccA, "2027-01-20", 5, "FORN SAP"),         // soma por pedido+CC+mês = 110
		sap("P9", ccA, "2027-01-11", 40, "NAO E NOSSO"),     // pedido fora da planilha: não entra
		sap("P1", "10026600", "2027-01-12", 99, "FORN SAP"), // CC 6600: fora
		sap("P1", ccA, "2026-12-15", 999, "FORN SAP"),       // dezembro/2026: antes do `Desde`, não vira oficial
	}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, KSB1: ksb, LastClosed: "2027-02", SapDesde: "2027-01"})

	// a KSB1 só cobre jan e dez/2026; fev/2027 é fechado sem KSB1
	r := realizadoPorMes(m)
	if r["2027-01"] != 50+110 {
		t.Errorf("realizado jan/27 = %.2f, esperado 160 (50 do item sem pedido + 110 da KSB1)", r["2027-01"])
	}
	if r["2026-12"] != 70 {
		t.Errorf("dez/26 não muda (antes do Desde): %.2f", r["2026-12"])
	}
	if r["2027-02"] != 20 {
		t.Errorf("fev/27 sem KSB1 continua na planilha: %.2f", r["2027-02"])
	}

	var achouSAP, achouP9, achouSubstituida bool
	for _, l := range m.Lanc {
		switch {
		case l.Origem == "SAP (KSB1)" && l.Pedido == "P1" && l.Valor == 110 && l.Forn == "F1":
			achouSAP = true
		case l.Pedido == "P9":
			achouP9 = true
		case l.ID == "1" && l.Kind == "X":
			achouSubstituida = true
		}
	}
	if !achouSAP {
		t.Error("faltou a linha do SAP para P1 (110, fornecedor da planilha)")
	}
	if achouP9 {
		t.Error("pedido fora da planilha não entra no realizado")
	}
	if !achouSubstituida {
		t.Error("a linha da planilha que o SAP substituiu deve ficar na lista, marcada como kind X (não soma)")
	}
	var aLancar bool
	for _, l := range m.Lanc {
		if l.ID == "3" && l.Classif == "Comprometido" {
			aLancar = true
		}
	}
	if !aLancar {
		t.Error("o a lançar da planilha deveria continuar")
	}

	if fmt.Sprint(m.Meta.Sap.Oficiais) != "[2027-01]" || fmt.Sprint(m.Meta.Sap.Provisorios) != "[2027-02]" {
		t.Errorf("oficiais=%v provisórios=%v", m.Meta.Sap.Oficiais, m.Meta.Sap.Provisorios)
	}
	if m.Meta.Sap.Ksb1De != "2026-12" || m.Meta.Sap.Ksb1Ate != "2027-01" || m.Meta.Sap.Desde != "2027-01" {
		t.Errorf("meta SAP: %+v", m.Meta.Sap)
	}
}

func TestSAPOficial_SemKSB1_MesFechadoDe2027EhProvisorio(t *testing.T) {
	lancs := []Lanc{planilhaL("1", "P1", ccA, "2027-01", 100, "LANÇADO"), planilhaL("2", "P2", ccA, "2027-02", 10, "LANÇADO")}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, LastClosed: "2027-02", SapDesde: "2027-01"})
	if fmt.Sprint(m.Meta.Sap.Provisorios) != "[2027-01 2027-02]" || len(m.Meta.Sap.Oficiais) != 0 {
		t.Errorf("sem KSB1 os meses fechados de 2027 ficam provisórios: %+v", m.Meta.Sap)
	}
	if realizadoPorMes(m)["2027-01"] != 100 {
		t.Error("sem KSB1 o realizado continua o da planilha")
	}
}

func TestSAPOficial_DesdeAntesDoSAP_PlanilhaVale(t *testing.T) {
	// antes do `Desde` o realizado é o da planilha (a KSB1 só confere)
	lancs := []Lanc{planilhaL("1", "P1", ccA, "2026-05", 100, "LANÇADO")}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, SapDesde: "2027-01", KSB1: []KSB1Linha{sap("P1", ccA, "2026-05-10", 777, "X")}, LastClosed: "2026-10"})
	if realizadoPorMes(m)["2026-05"] != 100 {
		t.Errorf("2026 não pode mudar: %.2f", realizadoPorMes(m)["2026-05"])
	}
	if len(m.Meta.Sap.Provisorios) != 0 {
		t.Errorf("2026 não tem mês provisório: %v", m.Meta.Sap.Provisorios)
	}
}

func TestSAPOficial_DesdeConfiguravel(t *testing.T) {
	lancs := []Lanc{planilhaL("1", "P1", ccA, "2026-05", 100, "LANÇADO")}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, SapDesde: "2026-01",
		KSB1: []KSB1Linha{sap("P1", ccA, "2026-05-10", 777, "X")}, LastClosed: "2026-10"})
	if realizadoPorMes(m)["2026-05"] != 777 {
		t.Errorf("com Desde=2026-01 o SAP vale em 2026: %.2f", realizadoPorMes(m)["2026-05"])
	}
}

func TestMontar_SaldoDaKSB2NosItensALancar(t *testing.T) {
	lancs := []Lanc{
		planilhaL("1", "P1", ccA, "2026-11", 100, "A LANÇAR"),
		planilhaL("2", "P2", ccA, "2026-11", 100, "LANÇADO"),  // realizado: sem saldo
		planilhaL("3", "P3", ccA, "2026-11", 100, "A LANÇAR"), // sem linha na KSB2: sem saldo
	}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, LastClosed: "2026-10",
		SaldosSAP: map[string]float64{"P1": 42.5, "P2": 7}})
	for _, l := range m.Lanc {
		switch l.ID {
		case "1":
			if l.SaldoSap == nil || *l.SaldoSap != 42.5 {
				t.Errorf("P1 a lançar deveria ter saldo 42,50: %v", l.SaldoSap)
			}
		case "2", "3":
			if l.SaldoSap != nil {
				t.Errorf("linha %s não deveria ter saldo: %v", l.ID, *l.SaldoSap)
			}
		}
	}
}

func TestMesEfetivo_EhADataDeLancamento(t *testing.T) {
	// decisão de 19/11/2026: a KSB1 não tem data de entrada; vale o mês da data de lançamento
	for lanc, quer := range map[string]string{"2026-10-20": "2026-10", "2026-11-02": "2026-11", "2026-12-31": "2026-12", "": ""} {
		if got := MesEfetivo(KSB1Linha{DataLanc: lanc}); got != quer {
			t.Errorf("lançamento %q: mês %q, esperava %q", lanc, got, quer)
		}
	}
}

func TestSAPOficial_SemTardios(t *testing.T) {
	lancs := []Lanc{planilhaL("1", "P1", ccA, "2026-10", 100, "LANÇADO")}
	ksb := []KSB1Linha{sap("P1", ccA, "2026-10-28", 100, "SAP")}
	m := Montar(Entrada{Params: paramsStatus(), Lancamentos: lancs, KSB1: ksb, LastClosed: "2026-10", SapDesde: "2026-01"})
	if m.Meta.Tardios.N != 0 || m.Meta.Sap.K1Modo != "data de lançamento" || len(m.Meta.Warn) != 0 {
		t.Errorf("sem data de entrada não há tardios nem aviso: %+v modo=%s warn=%v", m.Meta.Tardios, m.Meta.Sap.K1Modo, m.Meta.Warn)
	}
	if realizadoPorMes(m)["2026-10"] != 100 {
		t.Errorf("out/26 = %.2f, esperava 100 (o valor da KSB1 substitui o da planilha)", realizadoPorMes(m)["2026-10"])
	}
}
