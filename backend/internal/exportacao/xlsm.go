package exportacao

// xlsm.go — montarXLSM (Story 4.3, AD-12). Manipula o `.xlsm` como arquivo
// ZIP/XML via `archive/zip` + reescrita de partes XML (sem biblioteca
// externa de OOXML, mesma técnica do gerador legado JS/VBA que esta story
// substitui) — reescreve só a planilha "SAP Export" (localizada via
// xl/workbook.xml + xl/_rels/workbook.xml.rels, nunca por um nome de
// arquivo fixo como "sheet1.xml", que pode não corresponder à aba certa) e
// remove `xl/calcChain.xml` (corrupção de fórmula/calcChain desatualizado
// são bugs documentados do gerador legado, AD-12). Todo o resto —
// incluindo `xl/vbaProject.bin` — é copiado byte a byte via
// `zip.Writer.Copy` (forma raw do stdlib: bypassa decompressão/
// recompressão, Boundaries "Never" da spec: "nunca tocar o part
// vbaProject.bin").
import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var (
	sheetTagRegexp        = regexp.MustCompile(`<sheet\b[^>]*/>`)
	sheetNameAttrRegexp   = regexp.MustCompile(`name="([^"]*)"`)
	sheetRIDAttrRegexp    = regexp.MustCompile(`r:id="([^"]*)"`)
	relationshipTagRegexp = regexp.MustCompile(`<Relationship\b[^>]*/>`)
	relationshipIDRegexp  = regexp.MustCompile(`Id="([^"]*)"`)
	relationshipTgtRegexp = regexp.MustCompile(`Target="([^"]*)"`)
	sheetDataRegexp       = regexp.MustCompile(`(?s)<sheetData\b[^>]*>.*?</sheetData>|<sheetData\s*/>`)
	estadoOcultoRegexp    = regexp.MustCompile(`\s+state="(hidden|veryHidden)"`)
)

// abaSAPExportNome é o nome fixo da aba de rascunho que o robô VBA
// consome (AD-12) — "SAP Export" ou equivalente (Boundaries "Never" da
// spec: "nunca marcar a aba de rascunho como oculta").
const abaSAPExportNome = "SAP Export"

// montarXLSM abre `templateBytes` como ZIP, reescreve a planilha
// "SAP Export" a partir de `linhas` e devolve o novo `.xlsm` pronto para
// download. Falha se o template não tiver uma planilha "SAP Export" (erro
// de configuração/ativo externo — Design Notes da spec; não é um dos
// sentinelas de negócio traduzidos pelo handler). Wrapper fino (Story 4.4)
// sobre `montarXLSMComLinhas` — zero mudança de assinatura/comportamento
// desta função nem dos testes existentes (xlsm_test.go).
func montarXLSM(templateBytes []byte, linhas []linhaExportacao) ([]byte, error) {
	return montarXLSMComLinhas(templateBytes, montarSheetDataXML(linhas))
}

// montarXLSMComLinhas (Story 4.4) é o núcleo antes exclusivo de montarXLSM,
// generalizado para receber o `<sheetData>` já montado em XML (em vez de
// `[]linhaExportacao`) — reaproveitado tanto por montarXLSM (lote Despesa)
// quanto por ExportadorVBA.GerarLoteObra (via montarSheetDataXMLObra,
// obra.go), já que as duas planilhas têm layouts de coluna diferentes.
func montarXLSMComLinhas(templateBytes []byte, sheetDataXML string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return nil, fmt.Errorf("xlsm: abrir template: %w", err)
	}

	arquivos := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		arquivos[f.Name] = f
	}

	caminhoAba, err := resolverCaminhoAbaSAPExport(arquivos)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, f := range zr.File {
		switch {
		case f.Name == "xl/calcChain.xml":
			// AD-12: removido sempre que reescrever linhas — nunca copiado
			// para o arquivo novo.
			continue
		case f.Name == caminhoAba:
			if err := escreverAbaReescrita(zw, f, sheetDataXML); err != nil {
				return nil, err
			}
		case f.Name == "xl/workbook.xml":
			if err := escreverWorkbookSempreVisivel(zw, f); err != nil {
				return nil, err
			}
		default:
			// Inclui xl/vbaProject.bin — copiado byte a byte (Boundaries
			// "Never" da spec), nunca decodificado/recodificado.
			if err := zw.Copy(f); err != nil {
				return nil, fmt.Errorf("xlsm: copiar %s: %w", f.Name, err)
			}
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("xlsm: finalizar arquivo: %w", err)
	}
	return buf.Bytes(), nil
}

// resolverCaminhoAbaSAPExport encontra o caminho real (ex.
// "xl/worksheets/sheet3.xml") da planilha chamada "SAP Export" —
// resolvido via workbook.xml (nome -> r:id) + workbook.xml.rels
// (r:id -> Target), nunca assumido por convenção de nome de arquivo.
func resolverCaminhoAbaSAPExport(arquivos map[string]*zip.File) (string, error) {
	workbookXML, err := lerConteudoDoArquivo(arquivos, "xl/workbook.xml")
	if err != nil {
		return "", err
	}

	rid, err := extrairRIDDaAba(workbookXML, abaSAPExportNome)
	if err != nil {
		return "", err
	}

	relsXML, err := lerConteudoDoArquivo(arquivos, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}

	target, err := extrairTargetDoRelationship(relsXML, rid)
	if err != nil {
		return "", err
	}

	// Target é relativo à pasta "xl/" (convenção OOXML) — ex.
	// "worksheets/sheet3.xml" -> "xl/worksheets/sheet3.xml".
	return "xl/" + strings.TrimPrefix(target, "/"), nil
}

func lerConteudoDoArquivo(arquivos map[string]*zip.File, nome string) (string, error) {
	f, ok := arquivos[nome]
	if !ok {
		return "", fmt.Errorf("xlsm: template não contém %s (planilha %q ausente)", nome, abaSAPExportNome)
	}
	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("xlsm: abrir %s: %w", nome, err)
	}
	defer rc.Close()
	conteudo, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("xlsm: ler %s: %w", nome, err)
	}
	return string(conteudo), nil
}

// extrairRIDDaAba varre as tags `<sheet .../>` de workbook.xml em busca da
// aba `nomeAba` e devolve o `r:id` referenciado por ela.
func extrairRIDDaAba(workbookXML, nomeAba string) (string, error) {
	for _, tag := range sheetTagRegexp.FindAllString(workbookXML, -1) {
		m := sheetNameAttrRegexp.FindStringSubmatch(tag)
		if m == nil || m[1] != nomeAba {
			continue
		}
		rid := sheetRIDAttrRegexp.FindStringSubmatch(tag)
		if rid == nil {
			return "", fmt.Errorf("xlsm: planilha %q sem r:id em workbook.xml", nomeAba)
		}
		return rid[1], nil
	}
	return "", fmt.Errorf("xlsm: template não contém uma planilha %q", nomeAba)
}

// extrairTargetDoRelationship varre as tags `<Relationship .../>` de
// workbook.xml.rels em busca de `Id=rid` e devolve o `Target` associado.
func extrairTargetDoRelationship(relsXML, rid string) (string, error) {
	for _, tag := range relationshipTagRegexp.FindAllString(relsXML, -1) {
		id := relationshipIDRegexp.FindStringSubmatch(tag)
		if id == nil || id[1] != rid {
			continue
		}
		target := relationshipTgtRegexp.FindStringSubmatch(tag)
		if target == nil {
			return "", fmt.Errorf("xlsm: relationship %q sem Target em workbook.xml.rels", rid)
		}
		return target[1], nil
	}
	return "", fmt.Errorf("xlsm: relationship %q não encontrado em workbook.xml.rels", rid)
}

// escreverAbaReescrita substitui o conteúdo de `<sheetData>` da planilha
// "SAP Export" pelo XML já pronto em `sheetDataXML` (Story 4.4: recebe o
// `<sheetData>` pronto em vez de `[]linhaExportacao`, já que lotes Despesa
// e Obra montam esse XML com helpers diferentes — montarSheetDataXML vs.
// montarSheetDataXMLObra) — todo o resto do XML da planilha (formatação,
// larguras de coluna, etc.) é preservado como está no template.
func escreverAbaReescrita(zw *zip.Writer, f *zip.File, sheetDataXML string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("xlsm: abrir planilha %q: %w", abaSAPExportNome, err)
	}
	defer rc.Close()
	conteudo, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("xlsm: ler planilha %q: %w", abaSAPExportNome, err)
	}

	loc := sheetDataRegexp.FindIndex(conteudo)
	if loc == nil {
		return fmt.Errorf("xlsm: planilha %q sem <sheetData> no template", abaSAPExportNome)
	}

	var reescrito bytes.Buffer
	reescrito.Write(conteudo[:loc[0]])
	reescrito.WriteString(sheetDataXML)
	reescrito.Write(conteudo[loc[1]:])

	w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
	if err != nil {
		return fmt.Errorf("xlsm: criar planilha %q: %w", abaSAPExportNome, err)
	}
	if _, err := w.Write(reescrito.Bytes()); err != nil {
		return fmt.Errorf("xlsm: escrever planilha %q: %w", abaSAPExportNome, err)
	}
	return nil
}

// escreverWorkbookSempreVisivel copia xl/workbook.xml removendo qualquer
// `state="hidden"`/`state="veryHidden"` da tag `<sheet>` da aba
// "SAP Export" — backstop ativo do Boundaries "Never" da spec ("nunca
// marcar a aba de rascunho como oculta"), não só um copiar-como-está: se o
// template já viesse com esse estado por engano, esta reescrita o corrige.
func escreverWorkbookSempreVisivel(zw *zip.Writer, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("xlsm: abrir workbook.xml: %w", err)
	}
	defer rc.Close()
	conteudo, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("xlsm: ler workbook.xml: %w", err)
	}

	reescrito := sheetTagRegexp.ReplaceAllStringFunc(string(conteudo), func(tag string) string {
		m := sheetNameAttrRegexp.FindStringSubmatch(tag)
		if m == nil || m[1] != abaSAPExportNome {
			return tag
		}
		return estadoOcultoRegexp.ReplaceAllString(tag, "")
	})

	w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
	if err != nil {
		return fmt.Errorf("xlsm: criar workbook.xml: %w", err)
	}
	if _, err := io.WriteString(w, reescrito); err != nil {
		return fmt.Errorf("xlsm: escrever workbook.xml: %w", err)
	}
	return nil
}

// montarSheetDataXML monta o `<sheetData>` inteiro a partir de `linhas` —
// 1 `<row>` por linha, colunas na ordem A..I (layout melhor-esforço do
// Design Notes da spec: tipo/solicitação/divisão/CC/conta-ou-classe/mês/
// valor/lado, + solicitante no final). Todo texto livre passa por
// escaparTextoXML antes de entrar no XML gerado (AD-12: "nunca por
// substituição de string não-escapada").
func montarSheetDataXML(linhas []linhaExportacao) string {
	var sb strings.Builder
	sb.WriteString("<sheetData>")
	for i, l := range linhas {
		numeroLinha := i + 1
		colunas := []string{
			l.TipoSolicitacao,
			l.SolicitacaoID,
			l.DivisaoCodigo,
			l.CentroCustoCodigo,
			l.ContaOuClasseCodigo,
			formatarMesExportacao(l.Mes),
			formatarValorExportacao(l.Valor),
			l.Lado,
			l.SolicitanteNome,
		}
		fmt.Fprintf(&sb, `<row r="%d">`, numeroLinha)
		for coluna, valor := range colunas {
			ref := fmt.Sprintf("%s%d", colunaParaLetra(coluna), numeroLinha)
			sb.WriteString(`<c r="`)
			sb.WriteString(ref)
			sb.WriteString(`" t="inlineStr"><is><t>`)
			sb.WriteString(escaparTextoXML(valor))
			sb.WriteString(`</t></is></c>`)
		}
		sb.WriteString("</row>")
	}
	sb.WriteString("</sheetData>")
	return sb.String()
}

// montarSheetDataXMLObra (Story 4.4) monta o `<sheetData>` do lote Obra —
// mesmos helpers de montarSheetDataXML (colunaParaLetra/escaparTextoXML/
// formatarValorExportacao, já genéricos), layout de colunas próprio
// (melhor-esforço, mesma ressalva de confirmação da Story 4.3): tipo_
// solicitacao="obras"/solicitacao_id/classificacao/local_obra_codigo/
// subgrupo_codigo/ordem_investimento/valor/lado/solicitante_nome.
func montarSheetDataXMLObra(linhas []linhaExportacaoObra) string {
	var sb strings.Builder
	sb.WriteString("<sheetData>")
	for i, l := range linhas {
		numeroLinha := i + 1
		colunas := []string{
			l.TipoSolicitacao,
			l.SolicitacaoID,
			l.Classificacao,
			l.LocalObraCodigo,
			l.SubgrupoCodigo,
			l.OrdemInvestimento,
			formatarValorExportacao(l.Valor),
			l.Lado.String,
			l.SolicitanteNome,
		}
		fmt.Fprintf(&sb, `<row r="%d">`, numeroLinha)
		for coluna, valor := range colunas {
			ref := fmt.Sprintf("%s%d", colunaParaLetra(coluna), numeroLinha)
			sb.WriteString(`<c r="`)
			sb.WriteString(ref)
			sb.WriteString(`" t="inlineStr"><is><t>`)
			sb.WriteString(escaparTextoXML(valor))
			sb.WriteString(`</t></is></c>`)
		}
		sb.WriteString("</row>")
	}
	sb.WriteString("</sheetData>")
	return sb.String()
}

// colunaParaLetra converte um índice de coluna 0-based em letra (A, B, C,
// ...) — suficiente para as 9 colunas desta planilha, sem precisar do caso
// geral de colunas pós-Z (AA, AB, ...).
func colunaParaLetra(indiceZeroBased int) string {
	return string(rune('A' + indiceZeroBased))
}

// formatarMesExportacao devolve "" para linhas sem mês (Imobilizado) ou a
// data no formato ISO (YYYY-MM-DD) — melhor esforço do Design Notes da
// spec; o formato de data exato esperado pelo robô (texto vs. serial do
// Excel) PRECISA ser confirmado contra o `.bas` real antes de produção.
func formatarMesExportacao(mes sql.NullTime) string {
	if !mes.Valid {
		return ""
	}
	return mes.Time.Format("2006-01-02")
}

// formatarValorExportacao formata valor monetário com 2 casas decimais,
// sempre com ponto decimal (nunca vírgula, independente de locale).
func formatarValorExportacao(valor float64) string {
	return strconv.FormatFloat(valor, 'f', 2, 64)
}

// escaparTextoXML escapa qualquer texto antes de entrar num nó de texto
// XML (AD-12: "todo texto livre do usuário ... passa por função de escape
// ... nunca por substituição de string não-escapada") — aplicado a TODAS
// as colunas (não só nome do solicitante), já que qualquer valor pode
// conter caracteres especiais de XML.
func escaparTextoXML(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
