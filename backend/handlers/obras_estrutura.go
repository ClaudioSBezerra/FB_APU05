package handlers

// obras_estrutura.go — F7 e F8 da segunda leva (26/11/2026), como na versão de referência (FORM-OBRAS campos 4 e 5,
// ESTRUTURA-ORDEM-INVESTIMENTO §4):
//   F7 o local de obra define a FAMÍLIA da estrutura; o subgrupo só vale se for dessa família. Obra sem estrutura
//      (CD Jaboatão) aceita qualquer subgrupo e a ordem é digitada (sem "CRIAR").
//   F8 a ordem é resolvida pelo grão (local, subgrupo, tipo de despesa — que sai da classificação). Existe → vem
//      preenchida e travada, "CRIAR" não é opção (e o servidor recusa com a ordem existente na mensagem). Não existe,
//      obra com estrutura e classificação que permite → "CRIAR" automático. Retirada e origem da transferência sem
//      ordem → bloqueia ("sem ordem cadastrada, não há saldo a retirar"). Duas ordens no mesmo grão (Fortaleza,
//      direto × sub-contratada) → o usuário escolhe entre as duas, vendo o faturamento de cada uma.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"fb_apu05/internal/exportacao"

	"github.com/lib/pq"
)

var reOrdemDigitada = regexp.MustCompile(`^[0-9]{4,20}$`)

type localObraOpcao struct {
	ID            string `json:"id"`
	Codigo        string `json:"codigo"`
	Nome          string `json:"nome"`
	TipoEstrutura string `json:"tipo_estrutura"` // "" = obra sem estrutura carregada
}

type subgrupoOpcao struct {
	ID       string   `json:"id"`
	Codigo   string   `json:"codigo"`
	Nome     string   `json:"nome"`
	Familias []string `json:"familias"`
}

func listarLocaisObra(db *sql.DB) ([]localObraOpcao, error) {
	rows, err := db.Query(`SELECT id, codigo, nome, COALESCE(tipo_estrutura, '') FROM locais_obra ORDER BY nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []localObraOpcao{}
	for rows.Next() {
		var l localObraOpcao
		if err := rows.Scan(&l.ID, &l.Codigo, &l.Nome, &l.TipoEstrutura); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func listarSubgrupos(db *sql.DB) ([]subgrupoOpcao, error) {
	rows, err := db.Query(`SELECT id, codigo, nome, COALESCE(familias, '{}') FROM subgrupos_despesa ORDER BY codigo, nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []subgrupoOpcao{}
	for rows.Next() {
		var s subgrupoOpcao
		var fams pq.StringArray
		if err := rows.Scan(&s.ID, &s.Codigo, &s.Nome, &fams); err != nil {
			return nil, err
		}
		s.Familias = []string(fams)
		if s.Familias == nil {
			s.Familias = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ordemCandidata é uma ordem do cadastro para o grão, com o que a tela mostra.
type ordemCandidata struct {
	Ordem          string `json:"ordem"`
	TipoDespesa    string `json:"tipo_despesa"`
	TipoFaturament string `json:"tipo_faturamento"`
	Colunas        string `json:"colunas"`
	IEA            string `json:"iea"`
	CodigoNivel    string `json:"codigo_nivel"`
}

// grao: o que se sabe da linha (obra × subgrupo × classificação).
type grao struct {
	Familia       string // família da obra ("" = sem estrutura)
	SubgrupoNome  string
	NaFamilia     bool // o subgrupo vale para a família da obra (sempre true sem estrutura)
	Despesa       string
	Candidatas    []ordemCandidata
	PermiteCriar  bool
	SemEstrutura  bool
	SubgrupoAchou bool
}

type consultaGrao interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// resolverGrao consulta família, subgrupo e ordens do grão.
func resolverGrao(q consultaGrao, localID, subgrupoID, classificacao, lado string) (grao, error) {
	g := grao{Despesa: exportacao.DespesaDaClassificacao(classificacao)}
	if err := q.QueryRow(`SELECT COALESCE(tipo_estrutura, '') FROM locais_obra WHERE id = $1`, localID).Scan(&g.Familia); err != nil {
		return g, err
	}
	g.SemEstrutura = g.Familia == ""
	var fams pq.StringArray
	err := q.QueryRow(`SELECT nome, COALESCE(familias, '{}') FROM subgrupos_despesa WHERE id = $1`, subgrupoID).Scan(&g.SubgrupoNome, &fams)
	if err != nil && err != sql.ErrNoRows {
		return g, err
	}
	g.SubgrupoAchou = err == nil
	g.NaFamilia = g.SemEstrutura
	for _, f := range fams {
		if f == g.Familia {
			g.NaFamilia = true
		}
	}
	rows, err := q.Query(`
		SELECT numero_ordem, COALESCE(tipo_despesa, ''), COALESCE(tipo_faturamento, ''), COALESCE(imobilizado_iea, ''), COALESCE(codigo_nivel, '')
		FROM obra_ordens WHERE local_obra_id = $1 AND subgrupo_despesa_id = $2 AND (tipo_despesa = $3 OR tipo_despesa IS NULL)
		ORDER BY numero_ordem`, localID, subgrupoID, g.Despesa)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ordemCandidata
		if err := rows.Scan(&c.Ordem, &c.TipoDespesa, &c.TipoFaturament, &c.IEA, &c.CodigoNivel); err != nil {
			return g, err
		}
		c.Colunas = exportacao.RotuloFaturamento(c.TipoFaturament)
		g.Candidatas = append(g.Candidatas, c)
	}
	// "CRIAR" só onde a ordem nasce (Inclusão, FL, lado de inclusão da transferência de saldo) e só em obra com estrutura
	g.PermiteCriar = !g.SemEstrutura && classificacao != "retirada_saldo" && !(classificacao == classificacaoTransferenciaSaldo && lado == "retirada")
	return g, rows.Err()
}

// ObraOrdensHandler — GET /api/formulario/obra-ordens?local_obra_id=&subgrupo_despesa_id=&classificacao=&lado=
// Devolve a situação do grão e o texto de ajuda que vai sob o campo da ordem (como na referência).
func ObraOrdensHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		qs := r.URL.Query()
		localID, subgrupoID := qs.Get("local_obra_id"), qs.Get("subgrupo_despesa_id")
		if !uuidFormatRegexp.MatchString(localID) || !uuidFormatRegexp.MatchString(subgrupoID) {
			jsonErr(w, http.StatusBadRequest, "parâmetros 'local_obra_id' e 'subgrupo_despesa_id' devem ser UUIDs válidos")
			return
		}
		classificacao := qs.Get("classificacao")
		if classificacao == "" {
			classificacao = "inclusao"
		}
		g, err := resolverGrao(db, localID, subgrupoID, classificacao, qs.Get("lado"))
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "local de obra não encontrado")
			return
		} else if err != nil {
			log.Printf("[Formulario] Erro ao consultar ordens de obra: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		tipo := exportacao.RotuloDespesa(g.Despesa)
		var situacao, ordem, dica string
		switch {
		case g.SemEstrutura:
			situacao, dica = "sem_estrutura", "A estrutura desta obra ainda não foi cadastrada — informe a ordem."
		case !g.NaFamilia:
			situacao, dica = "fora_da_familia", "Este subgrupo não pertence à estrutura desta obra ("+g.Familia+")."
		case len(g.Candidatas) == 1:
			c := g.Candidatas[0]
			situacao, ordem = "existe", c.Ordem
			dica = "Ordem da estrutura"
			if c.CodigoNivel != "" {
				dica += " · " + c.CodigoNivel
			}
			dica += " · " + tipo
			if c.IEA != "" {
				dica += " · imobilizado " + c.IEA
			}
		case len(g.Candidatas) > 1:
			situacao, dica = "ambigua", "Há mais de uma ordem de "+tipo+" para este subgrupo; escolha pela coluna de faturamento."
		case g.PermiteCriar:
			situacao, ordem = "criar", "CRIAR"
			dica = "Ainda não existe ordem de " + tipo + " para este subgrupo: será criada no SAP."
		default:
			situacao, dica = "exige_existente", "Esta classificação exige uma ordem que já exista — este subgrupo ainda não tem ordem de "+tipo+"."
		}
		items := make([]string, 0, len(g.Candidatas))
		for _, c := range g.Candidatas {
			items = append(items, c.Ordem)
		}
		cands := g.Candidatas
		if cands == nil {
			cands = []ordemCandidata{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": items, "situacao": situacao, "ordem": ordem, "dica": dica, "candidatas": cands,
			"tipo_despesa": g.Despesa, "familia": g.Familia,
		})
	}
}

// conferirGraoObras é trocável nos testes do handler (os de sqlmock não simulam a estrutura).
var conferirGraoObras = verificarGraoObras

// verificarGraoObras (servidor, abertura de Obras): subgrupo da família da obra; "CRIAR" recusado se o grão já tem
// ordem (citando qual), em obra sem estrutura e onde a ordem não nasce; ordem digitada tem de ser a do grão (com
// estrutura) ou ter de 4 a 20 dígitos (sem estrutura); retirada sem ordem bloqueia.
func verificarGraoObras(tx *sql.Tx, classificacao string, linhas []obraLinhaValidado) (string, error) {
	for i, l := range linhas {
		n := i + 1
		lado := ""
		if l.Lado.Valid {
			lado = l.Lado.String
		}
		g, err := resolverGrao(tx, l.LocalObraID, l.SubgrupoDespesaID, classificacao, lado)
		if err == sql.ErrNoRows {
			return fmt.Sprintf("linha %d: local de obra não encontrado", n), nil
		} else if err != nil {
			return "", err
		}
		if !g.NaFamilia {
			return fmt.Sprintf("linha %d: o subgrupo %q não pertence à estrutura desta obra (%s).", n, g.SubgrupoNome, g.Familia), nil
		}
		tipo := exportacao.RotuloDespesa(g.Despesa)
		if l.OrdemInvestimento == "CRIAR" {
			switch {
			case g.SemEstrutura:
				return fmt.Sprintf("linha %d: a estrutura desta obra ainda não foi cadastrada — informe a ordem (\"CRIAR\" não vale).", n), nil
			case len(g.Candidatas) > 0:
				return fmt.Sprintf("linha %d: já existe a ordem %s para este subgrupo (%s); use-a em vez de \"CRIAR\".", n, g.Candidatas[0].Ordem, tipo), nil
			}
			continue // retirada/origem com CRIAR já é recusada por validarCriarObras
		}
		if g.SemEstrutura {
			if !reOrdemDigitada.MatchString(l.OrdemInvestimento) {
				return fmt.Sprintf("linha %d: a ordem de investimento tem de 4 a 20 dígitos.", n), nil
			}
			continue
		}
		if len(g.Candidatas) == 0 {
			if !g.PermiteCriar {
				return fmt.Sprintf("linha %d: este subgrupo não tem ordem de %s cadastrada — sem ordem, não há saldo a retirar.", n, tipo), nil
			}
			return fmt.Sprintf("linha %d: não há ordem de %s cadastrada para este subgrupo; use \"CRIAR\".", n, tipo), nil
		}
		ok := false
		var nums []string
		for _, c := range g.Candidatas {
			nums = append(nums, c.Ordem)
			ok = ok || c.Ordem == l.OrdemInvestimento
		}
		if !ok {
			return fmt.Sprintf("linha %d: a ordem %s não é a deste subgrupo para %s; use %s.", n, l.OrdemInvestimento, tipo, strings.Join(nums, " ou ")), nil
		}
	}
	return "", nil
}
