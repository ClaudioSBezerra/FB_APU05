package controleti

// Gabarito do cruzamento de pagamentos: o Dashboard TI 2027.html do Welton traz, no JSON embutido
// (fcx-data), as linhas de lançamento com o status de pagamento já calculado pela fcxPag, e o
// export da FBL1N que gerou esse resultado. O teste refaz o cruzamento em Go com as mesmas
// entradas e compara linha a linha. Os arquivos reais (dados de fornecedor) não ficam no repositório:
//
//	TI_FBL1N_HTML="…/Dashboard TI 2027.html" TI_FBL1N_XLSX="…/EXPORT_….xlsx" go test ./internal/controleti -run FBL1N -v

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type oraculoPag struct {
	St      string   `json:"st"`
	How     string   `json:"how"`
	DtComp  string   `json:"dtComp"`
	DocComp string   `json:"docComp"`
	Liq     float64  `json:"liq"`
	VencSap string   `json:"vencSap"`
	Ref     string   `json:"ref"`
	Emp     string   `json:"emp"`
	Bloq    bool     `json:"bloq"`
	_       struct{} `json:"-"`
}

type oraculoLanc struct {
	ID      string      `json:"id"`
	BP      string      `json:"bp"`
	NF      string      `json:"nf"`
	Miro    string      `json:"miro"`
	Comp    *string     `json:"comp"`
	Valor   float64     `json:"valor"`
	Forn    string      `json:"forn"`
	Classif string      `json:"classif"`
	Src     string      `json:"src"`
	Pag     *oraculoPag `json:"pag"`
}

type oraculoDados struct {
	Lanc []oraculoLanc `json:"lanc"`
	Pag  struct {
		Orphans []Orfa `json:"orphans"`
	} `json:"pag"`
}

func carregarOraculoHTML(t *testing.T, caminho string) oraculoDados {
	t.Helper()
	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	const abre = `<script id="fcx-data" type="application/json">`
	i := strings.Index(s, abre)
	if i < 0 {
		t.Fatal("o HTML não tem o bloco fcx-data")
	}
	i += len(abre)
	j := strings.Index(s[i:], "</script>")
	var d oraculoDados
	if err := json.Unmarshal([]byte(s[i:i+j]), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFBL1N_Oraculo(t *testing.T) {
	html, xlsx := os.Getenv("TI_FBL1N_HTML"), os.Getenv("TI_FBL1N_XLSX")
	if html == "" || xlsx == "" {
		t.Skip("defina TI_FBL1N_HTML e TI_FBL1N_XLSX para rodar o gabarito (arquivos reais não ficam no repositório)")
	}
	d := carregarOraculoHTML(t, html)
	dados, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	tipo, err := DetectarTipo(dados)
	if err != nil || tipo != TipoFBL1N {
		t.Fatalf("a FBL1N deveria ser detectada como %q: tipo=%q err=%v", TipoFBL1N, tipo, err)
	}
	partidas, leitura, err := LerFBL1N(dados)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FBL1N: %d linhas lidas, %d partidas, empresas %v", leitura.Linhas, len(partidas), leitura.Empresas)

	rows := make([]LancRow, len(d.Lanc))
	for i, l := range d.Lanc {
		rows[i] = LancRow{ID: l.ID, BP: l.BP, NF: l.NF, Miro: l.Miro, Comp: l.Comp, Valor: l.Valor, Forn: l.Forn, Classif: l.Classif, daSAP: l.Src == "SAP"}
	}
	antes := bpPorNomeNaFBL1N
	bpPorNomeNaFBL1N = false // o gabarito não tem o último recurso por nome
	t.Cleanup(func() { bpPorNomeNaFBL1N = antes })
	pm := CruzarPagamentos(rows, partidas, "FBL1N", "2026-11-18", "2026-11-18")

	difs, comPag := 0, 0
	for i, l := range d.Lanc {
		got := rows[i].Pag
		if l.Pag == nil {
			if got != nil {
				difs++
				if difs <= 8 {
					t.Errorf("%s: esperava sem pagamento, veio %+v", l.ID, *got)
				}
			}
			continue
		}
		comPag++
		if got == nil {
			difs++
			if difs <= 8 {
				t.Errorf("%s: esperava %+v, veio nada", l.ID, *l.Pag)
			}
			continue
		}
		want := Pag{St: l.Pag.St, How: l.Pag.How, DtComp: l.Pag.DtComp, DocComp: l.Pag.DocComp, Liq: l.Pag.Liq, VencSap: l.Pag.VencSap, Ref: l.Pag.Ref, Emp: l.Pag.Emp, Bloq: l.Pag.Bloq}
		if !reflect.DeepEqual(*got, want) {
			difs++
			if difs <= 8 {
				t.Errorf("%s (%s): esperava %+v, veio %+v", l.ID, l.Forn, want, *got)
			}
		}
	}
	t.Logf("%d linhas com pagamento no gabarito; %d divergências", comPag, difs)
	if difs > 0 {
		t.Fatalf("%d linhas diferem do gabarito", difs)
	}

	if len(pm.Orphans) != len(d.Pag.Orphans) {
		t.Fatalf("órfãs: esperava %d, veio %d", len(d.Pag.Orphans), len(pm.Orphans))
	}
	for i := range pm.Orphans {
		if !reflect.DeepEqual(pm.Orphans[i], d.Pag.Orphans[i]) {
			t.Fatalf("órfã %d difere: esperava %+v, veio %+v", i, d.Pag.Orphans[i], pm.Orphans[i])
		}
	}
	if pm.Meta.Linhas != 3283 && pm.Meta.Linhas != len(partidas) {
		t.Errorf("meta.linhas inesperado: %d", pm.Meta.Linhas)
	}
}

// Sem a coluna BP na planilha (caso da planilha de 2026), o cruzamento tenta o BP pelo nome do
// fornecedor na própria FBL1N. Mede quanto do gabarito isso recupera (só registra; não é regra).
func TestFBL1N_SemBP_Cobertura(t *testing.T) {
	html, xlsx := os.Getenv("TI_FBL1N_HTML"), os.Getenv("TI_FBL1N_XLSX")
	if html == "" || xlsx == "" {
		t.Skip("defina TI_FBL1N_HTML e TI_FBL1N_XLSX")
	}
	d := carregarOraculoHTML(t, html)
	dados, _ := os.ReadFile(xlsx)
	partidas, _, err := LerFBL1N(dados)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]LancRow, len(d.Lanc))
	for i, l := range d.Lanc {
		rows[i] = LancRow{ID: l.ID, NF: l.NF, Miro: l.Miro, Comp: l.Comp, Valor: l.Valor, Forn: l.Forn, Classif: l.Classif, daSAP: l.Src == "SAP"} // sem BP
	}
	CruzarPagamentos(rows, partidas, "FBL1N", "2026-11-18", "2026-11-18")
	iguais, total, achadas, gabAchadas := 0, 0, 0, 0
	for i, l := range d.Lanc {
		if l.Pag == nil {
			continue
		}
		total++
		got := rows[i].Pag
		if l.Pag.St != "Não localizada" {
			gabAchadas++
		}
		if got != nil && got.St != "Não localizada" {
			achadas++
		}
		if got != nil && got.St == l.Pag.St {
			iguais++
		}
	}
	t.Logf("sem BP na planilha: status igual ao gabarito em %d de %d (%.1f%%); localizadas %d (gabarito com BP: %d)", iguais, total, 100*float64(iguais)/float64(total), achadas, gabAchadas)
}
