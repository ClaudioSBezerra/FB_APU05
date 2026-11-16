package controleti

import (
	"testing"
)

func comp(m string) *string { return &m }

// lanc monta um lançamento da planilha já classificado.
func lanc(id, pedido, ccSap, mes string, valor float64, status, classif, forn string) LancRow {
	div, cc := DivCCDoSap(ccSap)
	_ = div
	return LancRow{ID: id, Pedido: pedido, CCSap: ccSap, CC: cc, Comp: comp(mes), Valor: valor, Status: status, Classif: classif, Forn: forn, Desc: "desc " + id}
}

func sap(pedido, ccSap, data string, valor float64, forn string) KSB1Linha {
	return KSB1Linha{Pedido: pedido, CCSap: ccSap, DataLanc: data, Valor: valor, Fornecedor: forn, Texto: "texto " + pedido}
}

const (
	ccA = "10026150"
	ccB = "10026720"
)

// cobertura: uma linha de KSB1 de um pedido/fornecedor que não é nosso, só para a
// KSB1 cobrir de janeiro a março (ela não aparece no resultado).
func cobertura() []KSB1Linha {
	return []KSB1Linha{sap("COB1", "99996150", "2026-01-05", 1, "ALHEIO LTDA"), sap("COB2", "99996150", "2026-03-20", 1, "ALHEIO LTDA")}
}

func situacaoDe(t *testing.T, r ConfResultado, pedido, mes string) string {
	t.Helper()
	var achadas []string
	for _, l := range r.Linhas {
		if l.Pedido == pedido && l.Mes == mes {
			achadas = append(achadas, l.Situacao)
		}
	}
	if len(achadas) != 1 {
		t.Fatalf("pedido %s mês %s: esperado 1 linha, achei %d %v", pedido, mes, len(achadas), achadas)
	}
	return achadas[0]
}

func TestConferir_NoveSituacoes(t *testing.T) {
	R := "Realizado"
	lancs := []LancRow{
		lanc("1", "P1", ccA, "2026-02", 100, "LANÇADO", R, "ACME SERVICOS DE INFORMATICA LTDA"), // conferido
		lanc("2", "P2", ccA, "2026-02", 100, "LANÇADO", R, "FORN DOIS"),                         // valor diferente
		lanc("3", "P3", ccA, "2026-02", 100, "LANÇADO", R, "FORN TRES"),                         // rateio (planilha no CC A, SAP no B)
		lanc("4", "P4", ccA, "2026-01", 100, "LANÇADO", R, "FORN QUATRO"),                       // outro mês (SAP em março)
		lanc("5", "P5", ccA, "2026-02", 100, "LANÇADO", R, "FORN CINCO"),                        // pendente
		lanc("6", "P6", ccA, "2026-02", 100, "LANÇADO", R, "FORN SEIS"),                         // pedido não encontrado
		lanc("7", "P7", ccA, "2026-02", 50, "A LANÇAR", "Comprometido", "FORN SETE"),            // pedido nosso, só existe no SAP
		lanc("8", "P8", ccA, "2026-01", 30, "LANÇADO", R, "FORN OITO"),                          // estorno sem original (SAP negativo em fev)
		lanc("11", "P11", ccA, "2026-02", -40, "ESTORNADO", R, "FORN ONZE"),                     // estorno nos dois lados
		lanc("12", "P12", ccA, "2026-02", 70, "LANÇADO", R, "FORN DOZE"),                        // CC 6600: fora
		lanc("13", "P13", ccA, "2026-06", 90, "LANÇADO", R, "FORN TREZE"),                       // fora da cobertura da KSB1
		lanc("14", "P14", ccA, "2026-02", 10, "LANÇADO", "Comprometido", "FORN QUATORZE"),       // não é realizado: não entra
	}
	lancs[9].CCSap, lancs[9].CC = "10026600", "6600"
	ksb := append(cobertura(),
		sap("P1", ccA, "2026-02-10", 100, "ACME SERVICOS DE INFORMATICA LTDA"),
		sap("P2", ccA, "2026-02-10", 90, "FORN DOIS"),
		sap("P3", ccB, "2026-02-10", 100, "FORN TRES"),
		sap("P4", ccA, "2026-03-15", 100, "FORN QUATRO"),
		sap("P5", ccA, "2026-01-15", 7, "FORN CINCO"),
		sap("P7", ccA, "2026-02-12", 50, "FORN SETE"),
		sap("P8", ccA, "2026-01-10", 30, "FORN OITO"),  // o original do mês 1 bate
		sap("P8", ccA, "2026-02-20", -30, "FORN OITO"), // estorno em fev, sem linha na planilha
		sap("P11", ccA, "2026-02-18", -40, "FORN ONZE"),
		sap("P9", ccA, "2026-02-14", 123, "ACME SERVICOS DE INFORMATICA LTDA"), // pedido fora da planilha, fornecedor nosso
		sap("P10", ccA, "2026-02-14", 55, "OUTRA AREA COMERCIAL LTDA"),         // pedido fora da planilha, fornecedor não nosso
		sap("P12", "10026600", "2026-02-10", 70, "FORN DOZE"),
		sap("P15", ccA, "2026-02-01", 100, "ZERADO LTDA"), // lançamento e estorno no mesmo mês: zera
		sap("P15", ccA, "2026-02-09", -100, "ZERADO LTDA"),
	)
	r := Conferir(lancs, ksb)

	casos := []struct{ pedido, mes, esperado string }{
		{"P1", "2026-02", SitConferido},
		{"P2", "2026-02", SitValorDiferente},
		{"P4", "2026-01", SitOutroMes},
		{"P5", "2026-02", SitPendente},
		{"P6", "2026-02", SitNaoEncontrado},
		{"P7", "2026-02", SitSoSAP},
		{"P8", "2026-01", SitConferido},
		{"P8", "2026-02", SitEstorno},
		{"P11", "2026-02", SitConferido},
		{"P9", "2026-02", SitVerificar},
	}
	for _, c := range casos {
		if got := situacaoDe(t, r, c.pedido, c.mes); got != c.esperado {
			t.Errorf("%s/%s = %s, esperado %s", c.pedido, c.mes, got, c.esperado)
		}
	}

	// rateio: as DUAS pontas (CC A na planilha, CC B no SAP) ficam marcadas
	rat := 0
	for _, l := range r.Linhas {
		if l.Pedido == "P3" && l.Situacao == SitRateioDiferente {
			rat++
		}
	}
	if rat != 2 {
		t.Errorf("rateio deveria marcar as duas pontas (CC da planilha e CC do SAP), marcou %d", rat)
	}

	// "lançado em outro mês": a ponta do SAP (março) NÃO vira uma 2ª linha "só no SAP"
	for _, l := range r.Linhas {
		if l.Pedido == "P4" && l.Mes == "2026-03" {
			t.Errorf("a ponta do SAP em março foi consumida pelo outro mês, mas apareceu: %+v", l)
		}
		if l.Pedido == "P4" && l.Situacao == SitOutroMes && (l.MesSAP != "2026-03" || l.ValorSAP != 100) {
			t.Errorf("outro mês sem o mês/valor do SAP: %+v", l)
		}
	}

	// o que NÃO pode aparecer
	for _, l := range r.Linhas {
		switch l.Pedido {
		case "P10":
			t.Errorf("pedido fora da planilha com fornecedor que não é nosso não deveria listar: %+v", l)
		case "P12":
			t.Errorf("CC 6600 (Lei do Bem) não deveria listar: %+v", l)
		case "P13":
			t.Errorf("mês fora da cobertura da KSB1 não deveria listar: %+v", l)
		case "P14":
			t.Errorf("lançamento não realizado não entra na conferência: %+v", l)
		case "P15":
			t.Errorf("lançamento e estorno no mesmo mês somam zero e não deveriam listar: %+v", l)
		case "COB1", "COB2":
			t.Errorf("linha de cobertura apareceu: %+v", l)
		}
	}

	if r.Resumo.PrimeiroMes != "2026-01" || r.Resumo.UltimoMes != "2026-03" {
		t.Errorf("cobertura = %s a %s", r.Resumo.PrimeiroMes, r.Resumo.UltimoMes)
	}
	if r.Resumo.Total != len(r.Linhas) || r.Resumo.Conferidos != 3 {
		t.Errorf("resumo: total=%d conferidos=%d (linhas=%d)", r.Resumo.Total, r.Resumo.Conferidos, len(r.Linhas))
	}
	// a base do percentual não conta "Verificar"
	if r.Resumo.Base != r.Resumo.Total-r.Resumo.Por[SitVerificar].Qtd || r.Resumo.Base == r.Resumo.Total {
		t.Errorf("base = %d, total = %d, verificar = %d", r.Resumo.Base, r.Resumo.Total, r.Resumo.Por[SitVerificar].Qtd)
	}
	var soma int
	for _, v := range r.Resumo.Por {
		soma += v.Qtd
	}
	if soma != r.Resumo.Total {
		t.Errorf("a soma das situações (%d) difere do total (%d)", soma, r.Resumo.Total)
	}
}

func TestConferir_SemKSB1NaoCompara(t *testing.T) {
	r := Conferir([]LancRow{lanc("1", "P1", ccA, "2026-02", 100, "LANÇADO", "Realizado", "X")}, nil)
	if r.Resumo.Total != 0 || len(r.Linhas) != 0 {
		t.Fatalf("sem KSB1 não há o que conferir: %+v", r.Resumo)
	}
}

func TestConferir_ToleranciaDeUmCentavo(t *testing.T) {
	lancs := []LancRow{lanc("1", "P1", ccA, "2026-02", 100.00, "LANÇADO", "Realizado", "X")}
	ksb := append(cobertura(), sap("P1", ccA, "2026-02-10", 100.01, "X"))
	if got := situacaoDe(t, Conferir(lancs, ksb), "P1", "2026-02"); got != SitConferido {
		t.Errorf("diferença de R$ 0,01 deveria conferir, deu %s", got)
	}
	ksb = append(cobertura(), sap("P1", ccA, "2026-02-10", 100.02, "X"))
	if got := situacaoDe(t, Conferir(lancs, ksb), "P1", "2026-02"); got != SitValorDiferente {
		t.Errorf("diferença de R$ 0,02 deveria ser valor diferente, deu %s", got)
	}
}

func TestConferir_SomaVariasLinhasDoMesmoGrupo(t *testing.T) {
	// duas parcelas do mesmo pedido+CC+mês na planilha e no SAP: comparam-se as somas
	lancs := []LancRow{
		lanc("1", "P1", ccA, "2026-02", 60, "LANÇADO", "Realizado", "X"),
		lanc("2", "P1", ccA, "2026-02", 40, "LANÇADO", "Realizado", "X"),
	}
	ksb := append(cobertura(), sap("P1", ccA, "2026-02-03", 100, "X"))
	r := Conferir(lancs, ksb)
	if got := situacaoDe(t, r, "P1", "2026-02"); got != SitConferido {
		t.Fatalf("60+40 contra 100 deveria conferir, deu %s", got)
	}
	for _, l := range r.Linhas {
		if l.Pedido == "P1" && len(l.IDs) != 2 {
			t.Errorf("deveria listar as 2 linhas da planilha envolvidas: %v", l.IDs)
		}
	}
}

func TestFornecedorNosso(t *testing.T) {
	nossos := map[string]struct{}{
		normalizaForn("STEFANINI CONSULT E ASSESSORIA EM"): {},
		normalizaForn("CLARO S.A."):                        {},
	}
	casos := []struct {
		nome string
		want bool
	}{
		{"STEFANINI CONSULT E ASSESSORIA EM", true},
		{"Stefanini Consult e Assessoria em Informática", true}, // nome do SAP mais longo: começa igual
		{"CLARO S.A.", true},
		{"CLARO TELECOM", false}, // curto demais para casar por prefixo
		{"OUTRA EMPRESA QUALQUER LTDA", false},
		{"", false},
	}
	for _, c := range casos {
		if got := fornecedorNosso(nossos, c.nome); got != c.want {
			t.Errorf("fornecedorNosso(%q) = %v, esperado %v", c.nome, got, c.want)
		}
	}
}
