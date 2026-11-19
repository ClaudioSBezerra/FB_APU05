package controleti

import "testing"

func mes(s string) *string { return &s }

func TestNfNumero(t *testing.T) {
	casos := map[string]string{"NF 000123": "123", "nfs-e 45": "45", "0069358": "69358", "FAT-0042/A": "42", "abc": "", "": "", "0": "0"}
	for in, want := range casos {
		if got := nfNumero(in); got != want {
			t.Errorf("nfNumero(%q) = %q, queria %q", in, got, want)
		}
	}
}

func TestNumeroFBL(t *testing.T) {
	casos := map[string]float64{"-4180.00": -4180, "1.234,56": 1234.56, "1.234,56-": -1234.56, "1,234.50": 1234.5, "": 0, "x": 0}
	for in, want := range casos {
		if got := numeroFBL(in); got != want {
			t.Errorf("numeroFBL(%q) = %v, queria %v", in, got, want)
		}
	}
}

func TestMesSoma(t *testing.T) {
	if mesSoma("2026-12", 1) != "2027-01" || mesSoma("2026-01", -1) != "2025-12" || mesSoma("2026-05", 0) != "2026-05" {
		t.Fatal("mesSoma errado na virada de ano")
	}
}

func TestCruzarPagamentos(t *testing.T) {
	bp := "2000111222"
	rows := []LancRow{
		{ID: "paga", BP: bp, NF: "100", Comp: mes("2026-03"), Valor: 500, Forn: "ACME", Classif: "Realizado"},
		{ID: "aberta", BP: bp, NF: "NF 200", Comp: mes("2026-03"), Valor: 300, Forn: "ACME", Classif: "Realizado"},
		{ID: "vencida", BP: bp, NF: "300", Comp: mes("2026-03"), Valor: 200, Forn: "ACME", Classif: "Realizado"},
		{ID: "nao", BP: bp, NF: "999", Comp: mes("2026-03"), Valor: 77, Forn: "ACME", Classif: "Realizado"},
		{ID: "semBP", NF: "1", Comp: mes("2026-03"), Valor: 10, Forn: "OUTRO SEM CADASTRO", Classif: "Realizado"},
		{ID: "doSAP", BP: bp, NF: "100", Comp: mes("2026-03"), Valor: 500, Classif: "Realizado", daSAP: true},
		{ID: "alancar", BP: bp, NF: "400", Comp: mes("2026-03"), Valor: 50, Classif: "Comprometido"},
	}
	partidas := []PartidaFBL1N{
		{Empresa: "FC01", BP: bp, Tipo: "RE", Ref: "100", Valor: 500, Doc: "5101", Venc: "2026-04-10", DtComp: "2026-04-10", DocComp: "2001"},
		{Empresa: "FC01", BP: bp, Tipo: "RE", Ref: "200", Valor: 300, Doc: "5102", Venc: "2099-01-01"},
		{Empresa: "FC01", BP: bp, Tipo: "RE", Ref: "300", Valor: 200, Doc: "5103", Venc: "2026-05-01", Bloq: true},
		{Empresa: "FC01", BP: bp, Tipo: "RE", Ref: "777", Valor: 1234, Doc: "5104"}, // ninguém explica: órfã
		{Empresa: "FC01", BP: bp, Tipo: "ZP", Ref: "100", Valor: -500, Doc: "2001"}, // pagamento, não é nota
	}
	pm := CruzarPagamentos(rows, partidas, "x.xlsx", "2026-11-18", "2026-11-18")

	quer := map[string]string{"paga": "Paga", "aberta": "Em aberto", "vencida": "Vencida", "nao": "Não localizada", "semBP": "Não localizada"}
	for _, r := range rows {
		want, tem := quer[r.ID]
		if !tem {
			if r.Pag != nil {
				t.Errorf("%s não deveria ter status de pagamento: %+v", r.ID, *r.Pag)
			}
			continue
		}
		if r.Pag == nil || r.Pag.St != want {
			t.Errorf("%s: esperava %q, veio %+v", r.ID, want, r.Pag)
		}
	}
	if p := rows[0].Pag; p.How != "BP + NF" || p.DtComp != "2026-04-10" || p.DocComp != "2001" || p.Liq != 500 {
		t.Errorf("detalhe da nota paga: %+v", p)
	}
	if !rows[2].Pag.Bloq || rows[2].Pag.VencSap != "2026-05-01" {
		t.Errorf("a vencida deveria trazer bloqueio e vencimento: %+v", rows[2].Pag)
	}
	if rows[4].Pag.How != "sem BP (preencha CNPJ/CPF na planilha)" {
		t.Errorf("motivo da sem BP: %q", rows[4].Pag.How)
	}
	if len(pm.Orphans) != 1 || pm.Orphans[0].Ref != "777" {
		t.Errorf("deveria sobrar só a nota 777 como órfã: %+v", pm.Orphans)
	}
	if pm.Meta.Linhas != 5 || len(pm.Meta.Empresas) != 1 || pm.Meta.Fonte != "x.xlsx" {
		t.Errorf("meta: %+v", pm.Meta)
	}
}

func TestCruzarPagamentos_BPPeloNomeNaFBL1N(t *testing.T) {
	bpPorNomeNaFBL1N = true
	defer func() { bpPorNomeNaFBL1N = false }()
	rows := []LancRow{{ID: "a", NF: "100", Comp: mes("2026-03"), Valor: 500, Forn: "Acme Serviços de TI Ltda", Classif: "Realizado"}}
	partidas := []PartidaFBL1N{{Empresa: "FC01", BP: "2000111222", Fornecedor: "ACME SERVICOS DE TI LTDA", Tipo: "RE", Ref: "100", Valor: 500, Venc: "2026-04-10", DtComp: "2026-04-11"}}
	pm := CruzarPagamentos(rows, partidas, "f", "2026-11-18", "2026-11-18")
	if rows[0].Pag.St != "Paga" || len(pm.Orphans) != 0 {
		t.Fatalf("deveria achar o BP pelo nome (sem acento/caixa): %+v", rows[0].Pag)
	}
	bpPorNomeNaFBL1N = false
	rows[0].Pag = nil
	CruzarPagamentos(rows, partidas, "f", "2026-11-18", "2026-11-18")
	if rows[0].Pag.St != "Não localizada" {
		t.Fatalf("sem o último recurso deveria ficar Não localizada: %+v", rows[0].Pag)
	}
}
