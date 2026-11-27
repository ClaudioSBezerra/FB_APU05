package exportacao

// xlsm_robo.go — preenchimento dos modelos dos ROBÔS do SAP (27/11/2026): "Robo KO22 Obras (novo).xlsm" (aba
// `incluir`) e "Robo KP06 Despesa (novo).xlsm" (abas `Chamados` e `Operacoes SAP`). Diferente do layout antigo
// ("SAP Export" reescrita inteira), aqui só as CÉLULAS indicadas mudam: o resto da aba (cabeçalho, estilos, botões,
// fórmulas fora da área de dados) fica como está, e o estilo (`s=`) de cada célula preenchida é mantido.
//
// O vbaProject.bin é copiado byte a byte, o calcChain.xml é removido e o workbook passa a recalcular ao abrir
// (fullCalcOnLoad), para as fórmulas das linhas novas aparecerem calculadas.

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// celulaRobo: o conteúdo de uma célula. Só um dos três é usado: Formula > Numero > Texto. Vazia = apaga o valor.
type celulaRobo struct {
	Texto   string
	Numero  *float64
	Formula string
}

func num(v float64) celulaRobo    { return celulaRobo{Numero: &v} }
func txt(s string) celulaRobo     { return celulaRobo{Texto: s} }
func formula(f string) celulaRobo { return celulaRobo{Formula: f} }

// edicaoAba: aba + células (por referência "A6") + linhas a limpar (valores apagados, estilo mantido).
type edicaoAba struct {
	Aba          string
	Celulas      map[string]celulaRobo
	LimparLinhas [2]int // [de, ate] inclusive; {0,0} = não limpa
}

var (
	rowRegexp      = regexp.MustCompile(`(?s)<row\b[^>]*?\br="(\d+)"[^>]*?(?:/>|>.*?</row>)`)
	rowAbreRegexp  = regexp.MustCompile(`^<row\b[^>]*?(/?)>`)
	celRegexp      = regexp.MustCompile(`(?s)<c\b[^>]*?\br="([A-Z]+)(\d+)"[^>]*?(?:/>|>.*?</c>)`)
	celEstiloRegex = regexp.MustCompile(`\bs="(\d+)"`)
	refRegexp      = regexp.MustCompile(`^([A-Z]+)(\d+)$`)
	calcPrRegexp   = regexp.MustCompile(`<calcPr\b[^>]*?/>`)
)

func indiceColuna(col string) int {
	n := 0
	for _, r := range col {
		n = n*26 + int(r-'A'+1)
	}
	return n
}

func letraColuna(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

func celulaXML(ref, estilo string, c celulaRobo) string {
	attr := ` r="` + ref + `"`
	if estilo != "" {
		attr += ` s="` + estilo + `"`
	}
	switch {
	case c.Formula != "":
		return `<c` + attr + `><f>` + escaparTextoXML(c.Formula) + `</f></c>`
	case c.Numero != nil:
		return `<c` + attr + `><v>` + strconv.FormatFloat(*c.Numero, 'f', -1, 64) + `</v></c>`
	case c.Texto != "":
		return `<c` + attr + ` t="inlineStr"><is><t xml:space="preserve">` + escaparTextoXML(c.Texto) + `</t></is></c>`
	}
	return `<c` + attr + `/>`
}

// editarSheetData aplica as células e a limpeza ao XML de uma aba.
func editarSheetData(xml string, ed edicaoAba) (string, error) {
	loc := sheetDataRegexp.FindStringIndex(xml)
	if loc == nil {
		return "", fmt.Errorf("xlsm: aba %q sem <sheetData>", ed.Aba)
	}
	sd := xml[loc[0]:loc[1]]

	// células por linha
	porLinha := map[int]map[int]celulaRobo{}
	for ref, c := range ed.Celulas {
		m := refRegexp.FindStringSubmatch(ref)
		if m == nil {
			return "", fmt.Errorf("xlsm: referência inválida %q", ref)
		}
		l, _ := strconv.Atoi(m[2])
		if porLinha[l] == nil {
			porLinha[l] = map[int]celulaRobo{}
		}
		porLinha[l][indiceColuna(m[1])] = c
	}
	limpar := func(l int) bool { return ed.LimparLinhas[0] > 0 && l >= ed.LimparLinhas[0] && l <= ed.LimparLinhas[1] }

	type linha struct {
		n   int
		xml string
	}
	var linhas []linha
	vistas := map[int]bool{}
	for _, m := range rowRegexp.FindAllStringSubmatchIndex(sd, -1) {
		bloco := sd[m[0]:m[1]]
		n, _ := strconv.Atoi(sd[m[2]:m[3]])
		vistas[n] = true
		if porLinha[n] == nil && !limpar(n) {
			linhas = append(linhas, linha{n, bloco})
			continue
		}
		linhas = append(linhas, linha{n, reescreverLinha(bloco, n, porLinha[n], limpar(n))})
	}
	for n, cels := range porLinha {
		if !vistas[n] {
			linhas = append(linhas, linha{n, reescreverLinha(fmt.Sprintf(`<row r="%d"/>`, n), n, cels, false)})
		}
	}
	sort.SliceStable(linhas, func(i, j int) bool { return linhas[i].n < linhas[j].n })

	var b strings.Builder
	abre := regexp.MustCompile(`^<sheetData\b[^>]*?/?>`).FindString(sd)
	b.WriteString(strings.TrimSuffix(abre, "/>"))
	if strings.HasSuffix(abre, "/>") {
		b.WriteString(">")
	}
	for _, l := range linhas {
		b.WriteString(l.xml)
	}
	b.WriteString("</sheetData>")
	return xml[:loc[0]] + b.String() + xml[loc[1]:], nil
}

func reescreverLinha(bloco string, n int, novas map[int]celulaRobo, limpar bool) string {
	abre := rowAbreRegexp.FindStringSubmatch(bloco)
	cab := strings.TrimSuffix(abre[0], "/>")
	if abre[1] == "/" {
		cab += ">"
	}
	// as células existentes, por coluna (guardando o estilo)
	type cel struct {
		col    int
		xml    string
		estilo string
	}
	var cels []cel
	existe := map[int]bool{}
	for _, m := range celRegexp.FindAllStringSubmatch(bloco, -1) {
		col := indiceColuna(m[1])
		existe[col] = true
		estilo := ""
		if e := celEstiloRegex.FindStringSubmatch(strings.SplitN(m[0], ">", 2)[0]); e != nil {
			estilo = e[1]
		}
		x := m[0]
		if c, ok := novas[col]; ok {
			x = celulaXML(letraColuna(col)+strconv.Itoa(n), estilo, c)
		} else if limpar {
			x = celulaXML(letraColuna(col)+strconv.Itoa(n), estilo, celulaRobo{})
		}
		cels = append(cels, cel{col, x, estilo})
	}
	for col, c := range novas {
		if !existe[col] {
			cels = append(cels, cel{col, celulaXML(letraColuna(col)+strconv.Itoa(n), "", c), ""})
		}
	}
	sort.SliceStable(cels, func(i, j int) bool { return cels[i].col < cels[j].col })
	var b strings.Builder
	b.WriteString(cab)
	for _, c := range cels {
		b.WriteString(c.xml)
	}
	b.WriteString("</row>")
	return b.String()
}

// montarXLSMRobo aplica as edições ao modelo do robô.
func montarXLSMRobo(templateBytes []byte, edicoes []edicaoAba) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return nil, fmt.Errorf("xlsm: abrir template: %w", err)
	}
	arquivos := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		arquivos[f.Name] = f
	}
	porCaminho := map[string]edicaoAba{}
	for _, ed := range edicoes {
		caminho, err := caminhoDaAba(arquivos, ed.Aba)
		if err != nil {
			return nil, err
		}
		porCaminho[caminho] = ed
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	escrever := func(f *zip.File, conteudo string) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, conteudo)
		return err
	}
	for _, f := range zr.File {
		switch ed, editar := porCaminho[f.Name]; {
		case f.Name == "xl/calcChain.xml":
			continue
		case editar:
			x, err := lerConteudoDoArquivo(arquivos, f.Name)
			if err != nil {
				return nil, err
			}
			novo, err := editarSheetData(x, ed)
			if err != nil {
				return nil, err
			}
			if err := escrever(f, novo); err != nil {
				return nil, fmt.Errorf("xlsm: escrever %s: %w", f.Name, err)
			}
		case f.Name == "xl/workbook.xml":
			x, err := lerConteudoDoArquivo(arquivos, f.Name)
			if err != nil {
				return nil, err
			}
			if calcPrRegexp.MatchString(x) {
				x = calcPrRegexp.ReplaceAllStringFunc(x, func(t string) string {
					if strings.Contains(t, "fullCalcOnLoad") {
						return t
					}
					return strings.TrimSuffix(t, "/>") + ` fullCalcOnLoad="1"/>`
				})
			} else {
				x = strings.Replace(x, "</workbook>", `<calcPr fullCalcOnLoad="1"/></workbook>`, 1)
			}
			if err := escrever(f, x); err != nil {
				return nil, fmt.Errorf("xlsm: escrever workbook: %w", err)
			}
		case f.Name == "[Content_Types].xml":
			x, err := lerConteudoDoArquivo(arquivos, f.Name)
			if err != nil {
				return nil, err
			}
			x = regexp.MustCompile(`<Override [^>]*PartName="/xl/calcChain.xml"[^>]*/>`).ReplaceAllString(x, "")
			if err := escrever(f, x); err != nil {
				return nil, err
			}
		case f.Name == "xl/_rels/workbook.xml.rels":
			x, err := lerConteudoDoArquivo(arquivos, f.Name)
			if err != nil {
				return nil, err
			}
			x = regexp.MustCompile(`<Relationship [^>]*Target="calcChain.xml"[^>]*/>`).ReplaceAllString(x, "")
			if err := escrever(f, x); err != nil {
				return nil, err
			}
		default:
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

func caminhoDaAba(arquivos map[string]*zip.File, aba string) (string, error) {
	wb, err := lerConteudoDoArquivo(arquivos, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	rid, err := extrairRIDDaAba(wb, aba)
	if err != nil {
		return "", err
	}
	rels, err := lerConteudoDoArquivo(arquivos, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}
	t, err := extrairTargetDoRelationship(rels, rid)
	if err != nil {
		return "", err
	}
	return "xl/" + strings.TrimPrefix(t, "/"), nil
}

// templateTemAba: o modelo é de robô novo quando tem a aba indicada.
func templateTemAba(templateBytes []byte, aba string) bool {
	zr, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return false
	}
	arquivos := map[string]*zip.File{}
	for _, f := range zr.File {
		arquivos[f.Name] = f
	}
	_, err = caminhoDaAba(arquivos, aba)
	return err == nil
}
