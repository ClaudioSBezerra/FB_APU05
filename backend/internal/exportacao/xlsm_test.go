package exportacao

// xlsm_test.go — cobre montarXLSM (AD-12) com um `.xlsm` SINTÉTICO de
// teste (nunca o arquivo de produção, que não existe neste repositório,
// Intent da spec): prova a preservação byte a byte de `vbaProject.bin`, a
// remoção de `calcChain.xml`, a reescrita isolada só da aba "SAP Export"
// (a outra aba do template sintético permanece intocada), a remoção de
// `state="hidden"` da aba "SAP Export" e o escape de texto livre.

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// escreverZipSintetico monta um .xlsm sintético mínimo a partir de um mapa
// nome->conteúdo — helper compartilhado por xlsm_test.go e vba_test.go.
func escreverZipSintetico(t *testing.T, arquivos map[string][]byte, ordem []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, nome := range ordem {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: nome, Method: zip.Deflate})
		if err != nil {
			t.Fatalf("erro ao criar %s no zip sintético: %v", nome, err)
		}
		if _, err := w.Write(arquivos[nome]); err != nil {
			t.Fatalf("erro ao escrever %s no zip sintético: %v", nome, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("erro ao finalizar zip sintético: %v", err)
	}
	return buf.Bytes()
}

// construirXLSMSintetico monta um template sintético com 2 abas (uma
// "Outra Aba" que NUNCA deve ser tocada, e "SAP Export", marcada
// state="hidden" de propósito para testar a remoção desse estado),
// xl/calcChain.xml (deve ser removido) e xl/vbaProject.bin com os bytes
// informados (deve ser preservado byte a byte).
func construirXLSMSintetico(t *testing.T, vbaBytes []byte) []byte {
	t.Helper()
	ordem := []string{
		"[Content_Types].xml",
		"xl/workbook.xml",
		"xl/_rels/workbook.xml.rels",
		"xl/worksheets/sheet1.xml",
		"xl/worksheets/sheet2.xml",
		"xl/calcChain.xml",
		"xl/vbaProject.bin",
	}
	arquivos := map[string][]byte{
		"[Content_Types].xml": []byte(`<?xml version="1.0"?><Types/>`),
		"xl/workbook.xml": []byte(`<workbook><sheets>` +
			`<sheet name="Outra Aba" sheetId="1" r:id="rId1"/>` +
			`<sheet name="SAP Export" sheetId="2" r:id="rId2" state="hidden"/>` +
			`</sheets></workbook>`),
		"xl/_rels/workbook.xml.rels": []byte(`<Relationships>` +
			`<Relationship Id="rId1" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Target="worksheets/sheet2.xml"/>` +
			`</Relationships>`),
		"xl/worksheets/sheet1.xml": []byte(`<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData></worksheet>`),
		"xl/worksheets/sheet2.xml": []byte(`<worksheet><sheetData><row r="1"><c r="A1"><v>999</v></c></row></sheetData></worksheet>`),
		"xl/calcChain.xml":         []byte(`<calcChain/>`),
		"xl/vbaProject.bin":        vbaBytes,
	}
	return escreverZipSintetico(t, arquivos, ordem)
}

// lerArquivoDoZip devolve o conteúdo de `nome` dentro de `conteudoZip`, ou
// (false) se o arquivo não existir no zip.
func lerArquivoDoZip(t *testing.T, conteudoZip []byte, nome string) ([]byte, bool) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(conteudoZip), int64(len(conteudoZip)))
	if err != nil {
		t.Fatalf("erro ao reabrir zip resultante: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != nome {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("erro ao abrir %s no zip resultante: %v", nome, err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("erro ao ler %s no zip resultante: %v", nome, err)
		}
		return buf.Bytes(), true
	}
	return nil, false
}

// TestMontarXLSM_PreservaVBAERemoveCalcChain cobre o núcleo do AD-12:
// vbaProject.bin idêntico byte a byte, calcChain.xml ausente no resultado,
// e a OUTRA aba (não "SAP Export") inalterada.
func TestMontarXLSM_PreservaVBAERemoveCalcChain(t *testing.T) {
	vbaOriginal := []byte("bytes-nao-interpretaveis-do-vbaProject-original")
	template := construirXLSMSintetico(t, vbaOriginal)

	linhas := []linhaExportacao{{
		TipoSolicitacao:     "transferencia",
		SolicitacaoID:       "11111111-1111-1111-1111-111111111111",
		SolicitanteNome:     "Fulano de Tal",
		Lado:                "origem",
		DivisaoCodigo:       "D1",
		CentroCustoCodigo:   "CC1",
		ContaOuClasseCodigo: "BIFC-1000",
		Mes:                 sql.NullTime{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Valor:               123.45,
	}}

	resultado, err := montarXLSM(template, linhas)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	vbaResultado, ok := lerArquivoDoZip(t, resultado, "xl/vbaProject.bin")
	if !ok {
		t.Fatalf("xl/vbaProject.bin ausente no resultado")
	}
	if !bytes.Equal(vbaResultado, vbaOriginal) {
		t.Fatalf("vbaProject.bin não foi preservado byte a byte: got %q, want %q", vbaResultado, vbaOriginal)
	}

	if _, ok := lerArquivoDoZip(t, resultado, "xl/calcChain.xml"); ok {
		t.Fatalf("xl/calcChain.xml deveria ter sido removido do resultado")
	}

	outraAba, ok := lerArquivoDoZip(t, resultado, "xl/worksheets/sheet1.xml")
	if !ok {
		t.Fatalf("xl/worksheets/sheet1.xml (outra aba) ausente no resultado")
	}
	if !bytes.Contains(outraAba, []byte("<v>1</v>")) {
		t.Fatalf("a aba que NÃO é \"SAP Export\" foi alterada: %s", outraAba)
	}
}

// TestMontarXLSM_ReescreveSoAPlanilhaSAPExport cobre a reescrita de linhas:
// o valor antigo ("999") desaparece da planilha "SAP Export" e os dados da
// linha nova aparecem nela.
func TestMontarXLSM_ReescreveSoAPlanilhaSAPExport(t *testing.T) {
	template := construirXLSMSintetico(t, []byte("vba"))

	linhas := []linhaExportacao{{
		TipoSolicitacao:     "transferencia",
		SolicitacaoID:       "11111111-1111-1111-1111-111111111111",
		SolicitanteNome:     "Fulano",
		Lado:                "destino",
		DivisaoCodigo:       "D1",
		CentroCustoCodigo:   "CC2",
		ContaOuClasseCodigo: "BIFC-2000",
		Mes:                 sql.NullTime{Time: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Valor:               50,
	}}

	resultado, err := montarXLSM(template, linhas)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	abaSAPExport, ok := lerArquivoDoZip(t, resultado, "xl/worksheets/sheet2.xml")
	if !ok {
		t.Fatalf("xl/worksheets/sheet2.xml (SAP Export) ausente no resultado")
	}
	conteudo := string(abaSAPExport)
	if strings.Contains(conteudo, "999") {
		t.Fatalf("valor antigo da planilha SAP Export não foi removido: %s", conteudo)
	}
	for _, esperado := range []string{"transferencia", "11111111-1111-1111-1111-111111111111", "D1", "CC2", "BIFC-2000", "2026-02-01", "50.00", "destino", "Fulano"} {
		if !strings.Contains(conteudo, esperado) {
			t.Fatalf("planilha SAP Export não contém %q: %s", esperado, conteudo)
		}
	}
}

// TestMontarXLSM_RemoveEstadoOcultoDaAba cobre o backstop do AD-12 ("nunca
// marcar a aba de rascunho como oculta"): o template sintético já vem com
// state="hidden" na aba "SAP Export" de propósito, e o resultado não deve
// mais ter esse atributo.
func TestMontarXLSM_RemoveEstadoOcultoDaAba(t *testing.T) {
	template := construirXLSMSintetico(t, []byte("vba"))

	resultado, err := montarXLSM(template, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	workbookXML, ok := lerArquivoDoZip(t, resultado, "xl/workbook.xml")
	if !ok {
		t.Fatalf("xl/workbook.xml ausente no resultado")
	}
	if strings.Contains(string(workbookXML), `state="hidden"`) {
		t.Fatalf("aba \"SAP Export\" continua marcada como oculta: %s", workbookXML)
	}
	if !strings.Contains(string(workbookXML), `name="SAP Export"`) {
		t.Fatalf("workbook.xml perdeu a referência à aba \"SAP Export\": %s", workbookXML)
	}
}

// TestMontarXLSM_EscapaTextoLivre cobre AD-12 ("todo texto livre ... passa
// por função de escape ... nunca por substituição de string não-
// escapada"): um nome de solicitante com caracteres especiais de XML
// entra escapado na planilha, nunca cru (o que corromperia o XML).
func TestMontarXLSM_EscapaTextoLivre(t *testing.T) {
	template := construirXLSMSintetico(t, []byte("vba"))

	linhas := []linhaExportacao{{
		TipoSolicitacao:     "transferencia",
		SolicitacaoID:       "11111111-1111-1111-1111-111111111111",
		SolicitanteNome:     `Fulano & "Beltrano" <teste>`,
		Lado:                "origem",
		DivisaoCodigo:       "D1",
		CentroCustoCodigo:   "CC1",
		ContaOuClasseCodigo: "BIFC-1000",
		Valor:               10,
	}}

	resultado, err := montarXLSM(template, linhas)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	abaSAPExport, _ := lerArquivoDoZip(t, resultado, "xl/worksheets/sheet2.xml")
	conteudo := string(abaSAPExport)
	if strings.Contains(conteudo, "<teste>") || strings.Contains(conteudo, `"Beltrano"`) {
		t.Fatalf("texto livre não foi escapado antes de entrar no XML: %s", conteudo)
	}
	if !strings.Contains(conteudo, "&amp;") || !strings.Contains(conteudo, "&lt;teste&gt;") {
		t.Fatalf("texto livre não contém a forma escapada esperada: %s", conteudo)
	}
}

// TestMontarXLSM_PlanilhaSAPExportAusente cobre a falha de configuração
// documentada no Design Notes da spec: um template sem a aba "SAP Export"
// nunca deveria ter chegado até aqui (gap de ativo externo), mas se
// chegar, falha claramente em vez de corromper qualquer outra aba.
func TestMontarXLSM_PlanilhaSAPExportAusente(t *testing.T) {
	ordem := []string{"[Content_Types].xml", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml"}
	arquivos := map[string][]byte{
		"[Content_Types].xml":        []byte(`<?xml version="1.0"?><Types/>`),
		"xl/workbook.xml":            []byte(`<workbook><sheets><sheet name="Outra Aba" sheetId="1" r:id="rId1"/></sheets></workbook>`),
		"xl/_rels/workbook.xml.rels": []byte(`<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`),
		"xl/worksheets/sheet1.xml":   []byte(`<worksheet><sheetData></sheetData></worksheet>`),
	}
	template := escreverZipSintetico(t, arquivos, ordem)

	_, err := montarXLSM(template, nil)
	if err == nil {
		t.Fatalf("esperava erro para template sem a planilha \"SAP Export\"")
	}
}

// TestMontarSheetDataXMLObra_LayoutDeColunas cobre o layout melhor-esforço
// de montarSheetDataXMLObra (Story 4.4, Design Notes da spec): tipo_
// solicitacao/solicitacao_id/classificacao/local_obra_codigo/
// subgrupo_codigo/ordem_investimento/valor/lado/solicitante_nome, cada
// coluna passando por escaparTextoXML.
func TestMontarSheetDataXMLObra_LayoutDeColunas(t *testing.T) {
	linhas := []linhaExportacaoObra{{
		TipoSolicitacao:   "obras",
		SolicitacaoID:     "11111111-1111-1111-1111-111111111111",
		SolicitanteNome:   `Fulano & "Beltrano"`,
		Classificacao:     "transferencia_saldo",
		LocalObraCodigo:   "LO1",
		SubgrupoCodigo:    "SG1",
		OrdemInvestimento: "9999",
		Valor:             250.5,
		Lado:              sql.NullString{String: "retirada", Valid: true},
	}}

	resultado := montarSheetDataXMLObra(linhas)

	for _, esperado := range []string{
		"<sheetData>", "</sheetData>",
		"obras", "11111111-1111-1111-1111-111111111111", "transferencia_saldo",
		"LO1", "SG1", "9999", "250.50", "retirada", "Fulano", "&amp;",
	} {
		if !strings.Contains(resultado, esperado) {
			t.Fatalf("montarSheetDataXMLObra não contém %q: %s", esperado, resultado)
		}
	}
}

// TestMontarSheetDataXMLObra_LadoNuloDevolveVazio cobre o caso de uma
// linha sem `lado` (classificação != transferencia_saldo, migration 009) —
// a coluna correspondente deve ficar vazia, nunca gerar erro/"<nil>".
func TestMontarSheetDataXMLObra_LadoNuloDevolveVazio(t *testing.T) {
	linhas := []linhaExportacaoObra{{
		TipoSolicitacao:   "obras",
		SolicitacaoID:     "11111111-1111-1111-1111-111111111111",
		SolicitanteNome:   "Fulano",
		Classificacao:     "inclusao",
		LocalObraCodigo:   "LO1",
		SubgrupoCodigo:    "SG1",
		OrdemInvestimento: "9999",
		Valor:             10,
		Lado:              sql.NullString{Valid: false},
	}}

	resultado := montarSheetDataXMLObra(linhas)
	if strings.Contains(resultado, "<nil>") {
		t.Fatalf("lado nulo não deveria aparecer como \"<nil>\": %s", resultado)
	}
}
