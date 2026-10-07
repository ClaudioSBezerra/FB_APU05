package handlers

// cadastros.go — Story 2.1 (Carga inicial e cadastros administráveis, FR-16).
//
// Um único registry {tipo} -> tabela/colunas + 5 handlers "Handler Factory"
// (mesmo padrão de backend/handlers/admin.go) servem os 7 cadastros mestres
// do Epic 2 (divisões, centros de custo, contas, classes de imobilizado,
// alçadas, papel×pessoa, feriados de SLA). Comportamento idêntico entre os 7
// tipos por construção — uma única implementação Go, não 7 cópias (Intent da
// spec).
//
// Toda rota é registrada em main.go atrás de
// RequireAuth(..., "administrador") — nenhuma checagem de perfil acontece
// aqui dentro (mesmo padrão da Story 1.3).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// colKind descreve o tipo Go/SQL de uma coluna editável de um cadastro —
// usado tanto para escanear uma linha da tabela (listagem) quanto para
// montar o snapshot JSONB gravado em cadastro_historico.
type colKind int

const (
	colString colKind = iota
	colUUID
	colNumerico
	colNumericoNulo
	colBool
	colData
)

// colunaDef é uma coluna editável (nunca id/created_at/updated_at, que nunca
// vêm do CSV nem do corpo de PUT — Design Notes da spec).
type colunaDef struct {
	Nome string
	Kind colKind
}

// cadastroTipo é a entrada do registry para um {tipo} de rota. DecodeCSV e
// DecodeJSON devolvem valores na MESMA ordem de Colunas — essa ordem é usada
// tanto para montar o INSERT/UPDATE quanto para montar o snapshot JSONB.
type cadastroTipo struct {
	Tabela       string
	CSVCabecalho []string
	Colunas      []colunaDef
	DecodeCSV    func(tx *sql.Tx, linha []string) ([]interface{}, error)
	DecodeJSON   func(corpo map[string]interface{}) ([]interface{}, error)
}

// cadastroRegistry é a ÚNICA fonte de verdade de quais {tipo} existem — um
// {tipo} fora daqui é sempre 404 "tipo de cadastro desconhecido", em
// qualquer uma das 5 rotas.
var cadastroRegistry = map[string]cadastroTipo{
	"divisoes": {
		Tabela:       "divisoes",
		CSVCabecalho: []string{"codigo", "nome"},
		Colunas: []colunaDef{
			{Nome: "codigo", Kind: colString},
			{Nome: "nome", Kind: colString},
		},
		DecodeCSV:  decodeCSVDivisoes,
		DecodeJSON: decodeJSONDivisoes,
	},
	"centros-custo": {
		Tabela:       "centros_custo",
		CSVCabecalho: []string{"codigo", "nome", "divisao_codigo"},
		Colunas: []colunaDef{
			{Nome: "codigo", Kind: colString},
			{Nome: "nome", Kind: colString},
			{Nome: "divisao_id", Kind: colUUID},
		},
		DecodeCSV:  decodeCSVCentrosCusto,
		DecodeJSON: decodeJSONCentrosCusto,
	},
	"contas": {
		Tabela:       "contas",
		CSVCabecalho: []string{"plano", "codigo", "nome"},
		Colunas: []colunaDef{
			{Nome: "plano", Kind: colString},
			{Nome: "codigo", Kind: colString},
			{Nome: "nome", Kind: colString},
		},
		DecodeCSV:  decodeCSVContas,
		DecodeJSON: decodeJSONContas,
	},
	"classes-imobilizado": {
		Tabela:       "classes_imobilizado",
		CSVCabecalho: []string{"codigo", "nome"},
		Colunas: []colunaDef{
			{Nome: "codigo", Kind: colString},
			{Nome: "nome", Kind: colString},
		},
		DecodeCSV:  decodeCSVClassesImobilizado,
		DecodeJSON: decodeJSONClassesImobilizado,
	},
	"alcadas": {
		Tabela:       "alcadas",
		CSVCabecalho: []string{"filial", "centro_custo_codigo", "valor_minimo", "valor_maximo", "papel_aprovador", "ativo"},
		Colunas: []colunaDef{
			{Nome: "filial", Kind: colString},
			{Nome: "centro_custo_codigo", Kind: colString},
			{Nome: "valor_minimo", Kind: colNumerico},
			{Nome: "valor_maximo", Kind: colNumericoNulo},
			{Nome: "papel_aprovador", Kind: colString},
			{Nome: "ativo", Kind: colBool},
		},
		DecodeCSV:  decodeCSVAlcadas,
		DecodeJSON: decodeJSONAlcadas,
	},
	"papel-pessoa": {
		Tabela:       "papel_pessoa",
		CSVCabecalho: []string{"papel", "pessoa_nome"},
		Colunas: []colunaDef{
			{Nome: "papel", Kind: colString},
			{Nome: "pessoa_nome", Kind: colString},
		},
		DecodeCSV:  decodeCSVPapelPessoa,
		DecodeJSON: decodeJSONPapelPessoa,
	},
	"feriados": {
		Tabela:       "feriados",
		CSVCabecalho: []string{"data", "descricao"},
		Colunas: []colunaDef{
			{Nome: "data", Kind: colData},
			{Nome: "descricao", Kind: colString},
		},
		DecodeCSV:  decodeCSVFeriados,
		DecodeJSON: decodeJSONFeriados,
	},
}

// --- DecodeJSON por tipo (corpo de PUT .../{id}) — mesma ordem de Colunas.
// Diferente do CSV, o corpo de PUT usa os nomes de COLUNA diretamente (ex.
// "divisao_id", não "divisao_codigo" — a resolução código->id é uma
// particularidade da carga CSV, não do estado já persistido).

func decodeJSONDivisoes(corpo map[string]interface{}) ([]interface{}, error) {
	codigo, err := extractString(corpo, "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := extractString(corpo, "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{codigo, nome}, nil
}

func decodeJSONCentrosCusto(corpo map[string]interface{}) ([]interface{}, error) {
	codigo, err := extractString(corpo, "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := extractString(corpo, "nome")
	if err != nil {
		return nil, err
	}
	divisaoID, err := extractString(corpo, "divisao_id")
	if err != nil {
		return nil, err
	}
	if !uuidFormatRegexp.MatchString(divisaoID) {
		return nil, fmt.Errorf("campo 'divisao_id' inválido")
	}
	return []interface{}{codigo, nome, divisaoID}, nil
}

func decodeJSONContas(corpo map[string]interface{}) ([]interface{}, error) {
	planoBruto, err := extractString(corpo, "plano")
	if err != nil {
		return nil, err
	}
	plano, err := validarPlano(planoBruto)
	if err != nil {
		return nil, err
	}
	codigo, err := extractString(corpo, "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := extractString(corpo, "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{plano, codigo, nome}, nil
}

func decodeJSONClassesImobilizado(corpo map[string]interface{}) ([]interface{}, error) {
	codigo, err := extractString(corpo, "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := extractString(corpo, "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{codigo, nome}, nil
}

// validarFaixaAlcada rejeita uma faixa de alçada invertida/nonsensical —
// valorMaximo, quando presente (não-nil), nunca pode ser menor que
// valorMinimo. valorMaximo nil (= "sem teto") não é validado aqui.
func validarFaixaAlcada(valorMinimo float64, valorMaximo interface{}) error {
	if valorMaximo == nil {
		return nil
	}
	vmax, ok := valorMaximo.(float64)
	if !ok {
		return fmt.Errorf("valor_maximo deve ser numérico ou nulo")
	}
	if vmax < valorMinimo {
		return fmt.Errorf("valor_maximo (%v) não pode ser menor que valor_minimo (%v)", vmax, valorMinimo)
	}
	return nil
}

func decodeJSONAlcadas(corpo map[string]interface{}) ([]interface{}, error) {
	filial, err := extractString(corpo, "filial")
	if err != nil {
		return nil, err
	}
	centroCustoCodigo, err := extractString(corpo, "centro_custo_codigo")
	if err != nil {
		return nil, err
	}
	valorMinimo, err := extractFloat(corpo, "valor_minimo")
	if err != nil {
		return nil, err
	}
	valorMaximo, err := extractFloatOpcional(corpo, "valor_maximo")
	if err != nil {
		return nil, err
	}
	if err := validarFaixaAlcada(valorMinimo, valorMaximo); err != nil {
		return nil, err
	}
	papelAprovador, err := extractString(corpo, "papel_aprovador")
	if err != nil {
		return nil, err
	}
	ativo, err := extractBool(corpo, "ativo")
	if err != nil {
		return nil, err
	}
	return []interface{}{filial, centroCustoCodigo, valorMinimo, valorMaximo, papelAprovador, ativo}, nil
}

func decodeJSONPapelPessoa(corpo map[string]interface{}) ([]interface{}, error) {
	papel, err := extractString(corpo, "papel")
	if err != nil {
		return nil, err
	}
	pessoaNome, err := extractString(corpo, "pessoa_nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{papel, pessoaNome}, nil
}

func decodeJSONFeriados(corpo map[string]interface{}) ([]interface{}, error) {
	dataBruta, err := extractString(corpo, "data")
	if err != nil {
		return nil, err
	}
	data, err := parseDataISO(dataBruta)
	if err != nil {
		return nil, err
	}
	descricao, err := extractString(corpo, "descricao")
	if err != nil {
		return nil, err
	}
	return []interface{}{data, descricao}, nil
}

// --- extract helpers (corpo de PUT, JSON genérico) ---

func extractString(corpo map[string]interface{}, campo string) (string, error) {
	v, ok := corpo[campo]
	if !ok {
		return "", fmt.Errorf("campo '%s' é obrigatório", campo)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("campo '%s' deve ser texto", campo)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("campo '%s' não pode ser vazio", campo)
	}
	return s, nil
}

func extractFloat(corpo map[string]interface{}, campo string) (float64, error) {
	v, ok := corpo[campo]
	if !ok {
		return 0, fmt.Errorf("campo '%s' é obrigatório", campo)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("campo '%s' deve ser numérico", campo)
	}
	return f, nil
}

// extractFloatOpcional devolve nil quando o campo veio EXPLICITAMENTE `null`
// (equivalente a "sem teto" em valor_maximo) — a chave precisa estar
// presente no corpo; se estiver ausente, é um erro (ex. typo do cliente), não
// um "sem teto" silencioso.
func extractFloatOpcional(corpo map[string]interface{}, campo string) (interface{}, error) {
	v, ok := corpo[campo]
	if !ok {
		return nil, fmt.Errorf("campo '%s' é obrigatório (use null para 'sem teto')", campo)
	}
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok {
		return nil, fmt.Errorf("campo '%s' deve ser numérico ou nulo", campo)
	}
	return f, nil
}

func extractBool(corpo map[string]interface{}, campo string) (bool, error) {
	v, ok := corpo[campo]
	if !ok {
		return false, fmt.Errorf("campo '%s' é obrigatório", campo)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("campo '%s' deve ser booleano", campo)
	}
	return b, nil
}

// --- helpers genéricos de SQL/scan/snapshot ---

func nomesColunas(colunas []colunaDef) []string {
	nomes := make([]string, len(colunas))
	for i, c := range colunas {
		nomes[i] = c.Nome
	}
	return nomes
}

// montarInsertSQL/montarUpdateSQL concatenam nome de tabela/coluna vindos
// EXCLUSIVAMENTE do registry estático acima (nunca de {tipo}, que só é usado
// como chave de lookup no map) — sem risco de injeção por entrada do
// usuário.
func montarInsertSQL(tabela string, colunas []string) string {
	placeholders := make([]string, len(colunas))
	for i := range colunas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id", tabela, strings.Join(colunas, ", "), strings.Join(placeholders, ", "))
}

func montarUpdateSQL(tabela string, colunas []string) string {
	sets := make([]string, len(colunas))
	for i, c := range colunas {
		sets[i] = fmt.Sprintf("%s = $%d", c, i+1)
	}
	return fmt.Sprintf("UPDATE %s SET %s, updated_at = now() WHERE id = $%d", tabela, strings.Join(sets, ", "), len(colunas)+1)
}

// montarSnapshot pareia colunas com valores (mesma ordem) para formar o
// `dados` JSONB gravado em cadastro_historico.
func montarSnapshot(colunas []colunaDef, valores []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(colunas))
	for i, c := range colunas {
		m[c.Nome] = valores[i]
	}
	return m
}

// destinoParaKind aloca o ponteiro de destino certo para dar Scan numa
// coluna, evitando o "gotcha" de escanear em *interface{} (lib/pq devolveria
// []byte cru para texto, que o encoding/json serializaria como base64 em vez
// de string).
func destinoParaKind(k colKind) interface{} {
	switch k {
	case colNumerico:
		return new(float64)
	case colNumericoNulo:
		return new(sql.NullFloat64)
	case colBool:
		return new(bool)
	case colData:
		return new(time.Time)
	default: // colString, colUUID
		return new(string)
	}
}

// dataISOFormato é o formato YYYY-MM-DD usado em toda a superfície de
// cadastros (CSV, corpo de PUT, snapshot de histórico) — uma coluna colData
// escaneada do banco precisa sair no MESMO formato, nunca no RFC3339Nano que
// database/sql usaria por padrão para um *string ligado a uma coluna DATE.
const dataISOFormato = "2006-01-02"

func valorDeDestino(k colKind, dest interface{}) interface{} {
	switch k {
	case colNumerico:
		return *dest.(*float64)
	case colNumericoNulo:
		nf := dest.(*sql.NullFloat64)
		if nf.Valid {
			return nf.Float64
		}
		return nil
	case colBool:
		return *dest.(*bool)
	case colData:
		return dest.(*time.Time).Format(dataISOFormato)
	default:
		return *dest.(*string)
	}
}

// escanearLinha lê uma linha de `SELECT id, <colunas...>, created_at,
// updated_at FROM <tabela>` (ordem fixa usada por ListarCadastroHandler).
func escanearLinha(rows *sql.Rows, colunas []colunaDef) (map[string]interface{}, error) {
	var id string
	destinos := make([]interface{}, 0, len(colunas)+3)
	destinos = append(destinos, &id)

	valoresPtr := make([]interface{}, len(colunas))
	for i, c := range colunas {
		d := destinoParaKind(c.Kind)
		valoresPtr[i] = d
		destinos = append(destinos, d)
	}

	var createdAt, updatedAt time.Time
	destinos = append(destinos, &createdAt, &updatedAt)

	if err := rows.Scan(destinos...); err != nil {
		return nil, err
	}

	item := map[string]interface{}{"id": id, "created_at": createdAt, "updated_at": updatedAt}
	for i, c := range colunas {
		item[c.Nome] = valorDeDestino(c.Kind, valoresPtr[i])
	}
	return item, nil
}

// errorMsgAmigavel traduz um erro de constraint do Postgres numa mensagem
// segura de expor ao cliente (400) — qualquer outro erro continua sendo
// tratado como 500 pelo chamador.
func errorMsgAmigavel(err error) string {
	if pqErr, ok := err.(*pq.Error); ok {
		switch pqErr.Code.Name() {
		case "unique_violation":
			return "registro duplicado (violação de chave única)"
		case "foreign_key_violation":
			return "referência inválida (chave estrangeira não encontrada)"
		case "check_violation":
			return "valor não atende às regras deste cadastro"
		}
	}
	return "valor inválido para este cadastro"
}

func ehViolacaoDeConstraint(err error) bool {
	_, ok := err.(*pq.Error)
	return ok
}

// nullableAtor evita gravar string vazia numa coluna UUID nullable
// (ator_id) — GetUserIDFromContext não deveria devolver "" numa rota atrás
// de RequireAuth, mas o handler não assume isso silenciosamente.
func nullableAtor(atorID string) interface{} {
	if atorID == "" {
		return nil
	}
	return atorID
}

func escreverRegistroJSON(w http.ResponseWriter, id string, snapshot map[string]interface{}) {
	out := make(map[string]interface{}, len(snapshot)+1)
	for k, v := range snapshot {
		out[k] = v
	}
	out["id"] = id
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// paginaMaxima evita que um valor extremo de "pagina" (ex. perto de
// math.MaxInt64) overflowe a multiplicação (pagina-1)*tamanho num OFFSET
// negativo, que o Postgres rejeitaria com 500 em vez de um 400 limpo — o
// clamp aqui tem a mesma função do clamp de `tamanho` logo abaixo.
const paginaMaxima = 1_000_000

func paginacaoDaQuery(r *http.Request) (pagina, tamanho int) {
	pagina = parseIntDefault(r.URL.Query().Get("pagina"), 1)
	tamanho = parseIntDefault(r.URL.Query().Get("tamanho"), 20)
	if pagina < 1 {
		pagina = 1
	}
	if pagina > paginaMaxima {
		pagina = paginaMaxima
	}
	if tamanho < 1 {
		tamanho = 1
	}
	if tamanho > 100 {
		tamanho = 100
	}
	return pagina, tamanho
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// --- 5 handlers genéricos ---

// ListarCadastroHandler — GET /api/admin/cadastros/{tipo}?pagina=&tamanho=
// (AD-14: {items, pagina, tamanho}, parâmetros 1–100).
func ListarCadastroHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.PathValue("tipo")
		def, ok := cadastroRegistry[tipo]
		if !ok {
			jsonErr(w, http.StatusNotFound, "tipo de cadastro desconhecido")
			return
		}

		pagina, tamanho := paginacaoDaQuery(r)

		colunas := nomesColunas(def.Colunas)
		query := fmt.Sprintf(
			"SELECT id, %s, created_at, updated_at FROM %s ORDER BY created_at, id LIMIT $1 OFFSET $2",
			strings.Join(colunas, ", "), def.Tabela,
		)

		rows, err := db.Query(query, tamanho, (pagina-1)*tamanho)
		if err != nil {
			log.Printf("[Cadastros] Erro ao listar %s: %v", tipo, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		items := make([]map[string]interface{}, 0)
		for rows.Next() {
			item, err := escanearLinha(rows, def.Colunas)
			if err != nil {
				log.Printf("[Cadastros] Erro ao ler linha de %s: %v", tipo, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Cadastros] Erro ao iterar %s: %v", tipo, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": items, "pagina": pagina, "tamanho": tamanho,
		})
	}
}

// ImportarCadastroHandler — POST /api/admin/cadastros/{tipo} (corpo = CSV
// cru, UTF-8 BOM opcional, separador ';'). Só roda contra a tabela do {tipo}
// ainda vazia; cabeçalho e cada linha são validados antes de qualquer
// INSERT; tudo numa única transação (atômico) — qualquer erro desfaz a carga
// inteira (Boundaries "Always" da spec).
func ImportarCadastroHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.PathValue("tipo")
		def, ok := cadastroRegistry[tipo]
		if !ok {
			jsonErr(w, http.StatusNotFound, "tipo de cadastro desconhecido")
			return
		}

		// 20MB: alçadas sozinha hoje tem milhares de faixas ativas (AD-14) —
		// margem generosa para o maior dos 7 CSVs de carga inicial.
		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		atorID := GetUserIDFromContext(r)

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Cadastros] Erro ao iniciar transação (importar %s): %v", tipo, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		// Advisory lock por {tipo}: serializa duas importações concorrentes do
		// MESMO tipo (duas abas do admin, por exemplo) — sem isso, as duas
		// podiam ler "tabela vazia" ao mesmo tempo e as duas tentarem
		// popular.
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "cadastro_import_"+tipo); err != nil {
			log.Printf("[Cadastros] Erro ao adquirir advisory lock (importar %s): %v", tipo, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var jaTemDados bool
		if err := tx.QueryRow(fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s)", def.Tabela)).Scan(&jaTemDados); err != nil {
			log.Printf("[Cadastros] Erro ao checar tabela %s: %v", def.Tabela, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if jaTemDados {
			jsonErr(w, http.StatusConflict, "cadastro já possui dados")
			return
		}

		linhas, err := lerCadastroCSV(corpo, def.CSVCabecalho)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}

		colunas := nomesColunas(def.Colunas)
		insertSQL := montarInsertSQL(def.Tabela, colunas)

		importados := 0
		for _, linha := range linhas {
			valores, err := def.DecodeCSV(tx, linha.Campos)
			if err != nil {
				jsonErr(w, http.StatusBadRequest, fmt.Sprintf("linha %d: %v", linha.Numero, err))
				return
			}

			var registroID string
			if err := tx.QueryRow(insertSQL, valores...).Scan(&registroID); err != nil {
				msg := "valor inválido para este cadastro"
				if ehViolacaoDeConstraint(err) {
					msg = errorMsgAmigavel(err)
				} else {
					log.Printf("[Cadastros] Erro ao inserir linha %d de %s: %v", linha.Numero, tipo, err)
				}
				jsonErr(w, http.StatusBadRequest, fmt.Sprintf("linha %d: %s", linha.Numero, msg))
				return
			}

			snapshot := montarSnapshot(def.Colunas, valores)
			dadosJSON, err := json.Marshal(snapshot)
			if err != nil {
				log.Printf("[Cadastros] Erro ao serializar snapshot (linha %d, %s): %v", linha.Numero, tipo, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}

			if _, err := tx.Exec(`
				INSERT INTO cadastro_historico (tipo_cadastro, registro_id, versao, acao, dados, ator_id)
				VALUES ($1, $2, 1, 'criado', $3, $4)
			`, tipo, registroID, string(dadosJSON), nullableAtor(atorID)); err != nil {
				log.Printf("[Cadastros] Erro ao gravar histórico (linha %d, %s): %v", linha.Numero, tipo, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}

			importados++
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Cadastros] Erro ao commitar importação de %s: %v", tipo, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Cadastros] %s importou %d linha(s) em %s", atorID, importados, tipo)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"importados": importados})
	}
}

// AtualizarCadastroHandler — PUT /api/admin/cadastros/{tipo}/{id} (corpo JSON
// com todas as colunas editáveis do tipo). Grava a mudança e a nova linha de
// histórico na MESMA transação (Boundaries "Always" da spec).
func AtualizarCadastroHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.PathValue("tipo")
		def, ok := cadastroRegistry[tipo]
		if !ok {
			jsonErr(w, http.StatusNotFound, "tipo de cadastro desconhecido")
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusBadRequest, "id inválido")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var corpo map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&corpo); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		valores, err := def.DecodeJSON(corpo)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}

		atorID := GetUserIDFromContext(r)

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Cadastros] Erro ao iniciar transação (atualizar %s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var existeID string
		err = tx.QueryRow(fmt.Sprintf("SELECT id FROM %s WHERE id = $1 FOR UPDATE", def.Tabela), id).Scan(&existeID)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "registro não encontrado")
			return
		} else if err != nil {
			log.Printf("[Cadastros] Erro ao buscar %s/%s: %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var proximaVersao int
		err = tx.QueryRow(`
			SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2
		`, tipo, id).Scan(&proximaVersao)
		if err != nil {
			log.Printf("[Cadastros] Erro ao calcular próxima versão (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		colunas := nomesColunas(def.Colunas)
		updateSQL := montarUpdateSQL(def.Tabela, colunas)
		args := append(append([]interface{}{}, valores...), id)
		if _, err := tx.Exec(updateSQL, args...); err != nil {
			msg := "valor inválido para este cadastro"
			if ehViolacaoDeConstraint(err) {
				msg = errorMsgAmigavel(err)
			} else {
				log.Printf("[Cadastros] Erro ao atualizar %s/%s: %v", tipo, id, err)
			}
			jsonErr(w, http.StatusBadRequest, msg)
			return
		}

		snapshot := montarSnapshot(def.Colunas, valores)
		dadosJSON, err := json.Marshal(snapshot)
		if err != nil {
			log.Printf("[Cadastros] Erro ao serializar snapshot (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if _, err := tx.Exec(`
			INSERT INTO cadastro_historico (tipo_cadastro, registro_id, versao, acao, dados, ator_id)
			VALUES ($1, $2, $3, 'atualizado', $4, $5)
		`, tipo, id, proximaVersao, string(dadosJSON), nullableAtor(atorID)); err != nil {
			log.Printf("[Cadastros] Erro ao gravar histórico (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Cadastros] Erro ao commitar atualização (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Cadastros] %s atualizou %s/%s (v%d)", atorID, tipo, id, proximaVersao)

		escreverRegistroJSON(w, id, snapshot)
	}
}

// HistoricoCadastroHandler — GET /api/admin/cadastros/{tipo}/{id}/historico
// (também paginado — AD-14 vale para toda listagem).
func HistoricoCadastroHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.PathValue("tipo")
		def, ok := cadastroRegistry[tipo]
		if !ok {
			jsonErr(w, http.StatusNotFound, "tipo de cadastro desconhecido")
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusBadRequest, "id inválido")
			return
		}

		var existe bool
		if err := db.QueryRow(fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)", def.Tabela), id).Scan(&existe); err != nil {
			log.Printf("[Cadastros] Erro ao checar existência (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !existe {
			jsonErr(w, http.StatusNotFound, "registro não encontrado")
			return
		}

		pagina, tamanho := paginacaoDaQuery(r)

		rows, err := db.Query(`
			SELECT versao, acao, dados, ator_id, created_at
			FROM cadastro_historico
			WHERE tipo_cadastro = $1 AND registro_id = $2
			ORDER BY versao DESC
			LIMIT $3 OFFSET $4
		`, tipo, id, tamanho, (pagina-1)*tamanho)
		if err != nil {
			log.Printf("[Cadastros] Erro ao listar histórico (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		items := make([]map[string]interface{}, 0)
		for rows.Next() {
			var versao int
			var acao string
			var dadosRaw []byte
			var atorID sql.NullString
			var createdAt time.Time
			if err := rows.Scan(&versao, &acao, &dadosRaw, &atorID, &createdAt); err != nil {
				log.Printf("[Cadastros] Erro ao ler linha de histórico (%s/%s): %v", tipo, id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			var dados map[string]interface{}
			if err := json.Unmarshal(dadosRaw, &dados); err != nil {
				log.Printf("[Cadastros] Erro ao decodificar snapshot de histórico (%s/%s v%d): %v", tipo, id, versao, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			item := map[string]interface{}{
				"versao": versao, "acao": acao, "dados": dados, "created_at": createdAt,
			}
			if atorID.Valid {
				item["ator_id"] = atorID.String
			} else {
				item["ator_id"] = nil
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Cadastros] Erro ao iterar histórico (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": items, "pagina": pagina, "tamanho": tamanho,
		})
	}
}

// restaurarCadastroRequest é o corpo de POST .../{id}/restaurar.
type restaurarCadastroRequest struct {
	Versao *int `json:"versao"`
}

// RestaurarCadastroHandler — POST /api/admin/cadastros/{tipo}/{id}/restaurar
// (corpo {"versao": V}). NUNCA deleta/edita a linha de histórico da versão
// alvo — só lê `dados`, aplica no registro vivo e grava uma versão NOVA
// (acao='restaurado'), nunca reaproveitando o número de versão antigo
// (Design Notes da spec).
func RestaurarCadastroHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.PathValue("tipo")
		def, ok := cadastroRegistry[tipo]
		if !ok {
			jsonErr(w, http.StatusNotFound, "tipo de cadastro desconhecido")
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusBadRequest, "id inválido")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req restaurarCadastroRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if req.Versao == nil || *req.Versao < 1 {
			jsonErr(w, http.StatusBadRequest, "informe 'versao' (inteiro >= 1)")
			return
		}

		atorID := GetUserIDFromContext(r)

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Cadastros] Erro ao iniciar transação (restaurar %s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var existeID string
		err = tx.QueryRow(fmt.Sprintf("SELECT id FROM %s WHERE id = $1 FOR UPDATE", def.Tabela), id).Scan(&existeID)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "registro não encontrado")
			return
		} else if err != nil {
			log.Printf("[Cadastros] Erro ao buscar %s/%s: %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var dadosRaw []byte
		err = tx.QueryRow(`
			SELECT dados FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2 AND versao = $3
		`, tipo, id, *req.Versao).Scan(&dadosRaw)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "versão não encontrada no histórico deste registro")
			return
		} else if err != nil {
			log.Printf("[Cadastros] Erro ao buscar versão %d (%s/%s): %v", *req.Versao, tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var dados map[string]interface{}
		if err := json.Unmarshal(dadosRaw, &dados); err != nil {
			log.Printf("[Cadastros] Erro ao decodificar snapshot alvo (%s/%s v%d): %v", tipo, id, *req.Versao, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		valores := make([]interface{}, len(def.Colunas))
		for i, c := range def.Colunas {
			valores[i] = dados[c.Nome]
		}

		var proximaVersao int
		err = tx.QueryRow(`
			SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2
		`, tipo, id).Scan(&proximaVersao)
		if err != nil {
			log.Printf("[Cadastros] Erro ao calcular próxima versão (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		colunas := nomesColunas(def.Colunas)
		updateSQL := montarUpdateSQL(def.Tabela, colunas)
		args := append(append([]interface{}{}, valores...), id)
		if _, err := tx.Exec(updateSQL, args...); err != nil {
			log.Printf("[Cadastros] Erro ao restaurar %s/%s para v%d: %v", tipo, id, *req.Versao, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		// Reusa os bytes exatos do snapshot lido (não re-serializa `dados`) —
		// preserva byte a byte o JSON da versão restaurada na nova linha de
		// histórico.
		if _, err := tx.Exec(`
			INSERT INTO cadastro_historico (tipo_cadastro, registro_id, versao, acao, dados, ator_id)
			VALUES ($1, $2, $3, 'restaurado', $4, $5)
		`, tipo, id, proximaVersao, string(dadosRaw), nullableAtor(atorID)); err != nil {
			log.Printf("[Cadastros] Erro ao gravar histórico de restauração (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Cadastros] Erro ao commitar restauração (%s/%s): %v", tipo, id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Cadastros] %s restaurou %s/%s para v%d (nova v%d)", atorID, tipo, id, *req.Versao, proximaVersao)

		escreverRegistroJSON(w, id, dados)
	}
}
