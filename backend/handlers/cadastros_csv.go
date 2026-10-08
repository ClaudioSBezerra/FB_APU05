package handlers

// cadastros_csv.go — parser/validador CSV genérico para os 7 cadastros
// administráveis (Story 2.1) + decodificação específica de cada tipo (CSV ->
// valores prontos para INSERT, na mesma ordem de cadastroTipo.Colunas).
//
// Formato de entrada: UTF-8 com BOM opcional, separador ';' (Design Notes da
// spec / AD-8). O parser descarta o BOM se presente e usa encoding/csv (lida
// corretamente com aspas e não se confunde com ';' dentro de um campo
// citado) em vez de um split ingênuo por ';'.

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var bomUTF8 = []byte{0xEF, 0xBB, 0xBF}

// linhaCSV é uma linha de dado já validada quanto ao número de campos, com o
// número da linha no arquivo original (1 = cabeçalho; a primeira linha de
// dado é 2) — usado para compor mensagens de erro "linha N: ..." (I/O Matrix
// da spec).
type linhaCSV struct {
	Numero int
	Campos []string
}

// lerCadastroCSV descarta um BOM UTF-8 inicial se presente, faz parse com
// separador ';', valida que o cabeçalho bate EXATAMENTE (mesmo número de
// colunas, mesmo texto, mesma ordem) com cabecalhoEsperado, e devolve as
// linhas de dado (nunca inclui o cabeçalho). Qualquer divergência de
// cabeçalho ou de corpo vazio é um erro só de leitura/formatação — ainda não
// validação de valor por linha (isso é DecodeCSV, chamado pelo handler).
func lerCadastroCSV(corpo []byte, cabecalhoEsperado []string) ([]linhaCSV, error) {
	corpo = bytes.TrimPrefix(corpo, bomUTF8)

	reader := csv.NewReader(bytes.NewReader(corpo))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1 // validamos o tamanho de cada linha nós mesmos, para citar o número da linha no erro

	header, err := reader.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("arquivo CSV vazio — cabeçalho esperado: %s", strings.Join(cabecalhoEsperado, ";"))
	}
	if err != nil {
		return nil, fmt.Errorf("erro ao ler cabeçalho do CSV: %v", err)
	}
	if !cabecalhoBate(header, cabecalhoEsperado) {
		return nil, fmt.Errorf("cabeçalho do CSV não confere — esperado exatamente: %s", strings.Join(cabecalhoEsperado, ";"))
	}

	var linhas []linhaCSV
	numeroLinha := 1 // linha 1 = cabeçalho, já consumido acima
	for {
		numeroLinha++
		campos, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("linha %d: erro ao ler CSV: %v", numeroLinha, err)
		}
		if len(campos) != len(cabecalhoEsperado) {
			return nil, fmt.Errorf("linha %d: esperado %d coluna(s), recebido %d", numeroLinha, len(cabecalhoEsperado), len(campos))
		}
		linhas = append(linhas, linhaCSV{Numero: numeroLinha, Campos: campos})
	}
	return linhas, nil
}

func cabecalhoBate(recebido, esperado []string) bool {
	if len(recebido) != len(esperado) {
		return false
	}
	for i := range esperado {
		if recebido[i] != esperado[i] {
			return false
		}
	}
	return true
}

// --- helpers de parse de campo individual ---

func parseNumeroObrigatorio(valor string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(valor), 64)
	if err != nil {
		return 0, fmt.Errorf("valor numérico inválido: %q", valor)
	}
	return v, nil
}

// parseNumeroOpcional devolve nil quando o campo vem vazio (ex. valor_maximo
// = "sem teto", Design Notes da spec) — nil vira SQL NULL no INSERT e JSON
// null no snapshot.
func parseNumeroOpcional(valor string) (interface{}, error) {
	trimmed := strings.TrimSpace(valor)
	if trimmed == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return nil, fmt.Errorf("valor numérico inválido: %q", valor)
	}
	return v, nil
}

// parseStringOpcional devolve nil quando o campo vem vazio (ex. "filial"
// ainda não cadastrada, ou campo opcional de regras-aprovacao/
// gerentes-aprovacao) — nil vira SQL NULL no INSERT e JSON null no
// snapshot, mesmo padrão de parseNumeroOpcional.
func parseStringOpcional(valor string) interface{} {
	trimmed := strings.TrimSpace(valor)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// parseNumeroOpcionalComDefault é parseNumeroOpcional, mas devolve
// `padrao` em vez de nil quando o campo vem vazio — usado por `teto`
// (gerentes-aprovacao), que tem default 20000 na tabela (FR-10) e deve
// manter esse mesmo default quando a carga CSV não informa o valor.
func parseNumeroOpcionalComDefault(valor string, padrao float64) (float64, error) {
	trimmed := strings.TrimSpace(valor)
	if trimmed == "" {
		return padrao, nil
	}
	return parseNumeroObrigatorio(trimmed)
}

// parseDataISO valida (sem reformatar) uma data no formato YYYY-MM-DD —
// mesma string é usada tanto para o INSERT (Postgres aceita o literal ISO
// para uma coluna DATE) quanto para o snapshot JSONB.
func parseDataISO(valor string) (string, error) {
	trimmed := strings.TrimSpace(valor)
	if _, err := time.Parse("2006-01-02", trimmed); err != nil {
		return "", fmt.Errorf("data %q fora do formato YYYY-MM-DD", trimmed)
	}
	return trimmed, nil
}

func parseBoolCSV(valor string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(valor)) {
	case "true", "1", "sim":
		return true, nil
	case "false", "0", "nao", "não":
		return false, nil
	default:
		return false, fmt.Errorf("valor booleano inválido: %q (use true/false)", valor)
	}
}

func validarPlano(valor string) (string, error) {
	trimmed := strings.TrimSpace(valor)
	if trimmed != "BIFC" && trimmed != "SFC" {
		return "", fmt.Errorf("plano deve ser 'BIFC' ou 'SFC', recebido %q", trimmed)
	}
	return trimmed, nil
}

func campoObrigatorio(valor, nomeCampo string) (string, error) {
	trimmed := strings.TrimSpace(valor)
	if trimmed == "" {
		return "", fmt.Errorf("campo '%s' não pode ser vazio", nomeCampo)
	}
	return trimmed, nil
}

// resolveDivisaoID traduz divisao_codigo (coluna do CSV de centros-custo)
// para divisao_id (coluna da tabela centros_custo) consultando `divisoes`
// dentro da MESMA transação do import — se divisoes ainda não foi carregada
// (ou o código não existe), a linha inteira é rejeitada citando a causa
// (Design Notes da spec: "não encontrado → rejeita a carga citando a
// linha").
func resolveDivisaoID(tx *sql.Tx, codigo string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM divisoes WHERE codigo = $1`, codigo).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("divisão com código %q não encontrada — importe divisoes antes de centros-custo", codigo)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// resolveColaboradorIDPorEmail traduz colaborador_email (coluna do CSV de
// cc-excecao) para colaborador_id (coluna da tabela cc_excecao) consultando
// `usuarios` dentro da MESMA transação do import — comparação
// case-insensitive (`LOWER(email) = LOWER($1)`), mesmo padrão já estabelecido
// em colaboradores.go/auth_sso.go (Boundaries da spec: reaproveitar essa
// convenção, não a comparação case-sensitive original de resolveDivisaoID).
// Não encontrado -> rejeita a linha citando a causa (mesmo padrão atômico de
// centros-custo).
func resolveColaboradorIDPorEmail(tx *sql.Tx, email string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)`, email).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("colaborador com e-mail %q não encontrado", email)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// resolveCentroCustoIDPorCodigo traduz centro_custo_codigo (coluna do CSV de
// cc-excecao) para centro_custo_id consultando `centros_custo` dentro da
// MESMA transação do import — comparação case-insensitive
// (`UPPER(codigo) = UPPER($1)`), reaproveitando a correção já aplicada na
// Story 2.2 (review triage daquela story), não a comparação case-sensitive de
// resolveDivisaoID. Não encontrado -> rejeita a linha citando a causa.
func resolveCentroCustoIDPorCodigo(tx *sql.Tx, codigo string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)`, codigo).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("centro de custo com código %q não encontrado", codigo)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// --- DecodeCSV por tipo — devolvem valores na MESMA ordem de
// cadastroTipo.Colunas (ver registry em cadastros.go) ---

func decodeCSVDivisoes(_ *sql.Tx, linha []string) ([]interface{}, error) {
	codigo, err := campoObrigatorio(linha[0], "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := campoObrigatorio(linha[1], "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{codigo, nome}, nil
}

func decodeCSVCentrosCusto(tx *sql.Tx, linha []string) ([]interface{}, error) {
	codigo, err := campoObrigatorio(linha[0], "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := campoObrigatorio(linha[1], "nome")
	if err != nil {
		return nil, err
	}
	divisaoCodigo, err := campoObrigatorio(linha[2], "divisao_codigo")
	if err != nil {
		return nil, err
	}
	divisaoID, err := resolveDivisaoID(tx, divisaoCodigo)
	if err != nil {
		return nil, err
	}
	// "filial" (Story 3.1): opcional — vazio no CSV vira NULL, não rejeita a
	// linha (Design Notes da spec: carga real é tarefa do negócio).
	filial := parseStringOpcional(linha[3])
	return []interface{}{codigo, nome, divisaoID, filial}, nil
}

func decodeCSVContas(_ *sql.Tx, linha []string) ([]interface{}, error) {
	plano, err := validarPlano(linha[0])
	if err != nil {
		return nil, err
	}
	codigo, err := campoObrigatorio(linha[1], "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := campoObrigatorio(linha[2], "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{plano, codigo, nome}, nil
}

func decodeCSVClassesImobilizado(_ *sql.Tx, linha []string) ([]interface{}, error) {
	codigo, err := campoObrigatorio(linha[0], "codigo")
	if err != nil {
		return nil, err
	}
	nome, err := campoObrigatorio(linha[1], "nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{codigo, nome}, nil
}

func decodeCSVAlcadas(_ *sql.Tx, linha []string) ([]interface{}, error) {
	filial, err := campoObrigatorio(linha[0], "filial")
	if err != nil {
		return nil, err
	}
	centroCustoCodigo, err := campoObrigatorio(linha[1], "centro_custo_codigo")
	if err != nil {
		return nil, err
	}
	valorMinimo, err := parseNumeroObrigatorio(linha[2])
	if err != nil {
		return nil, err
	}
	valorMaximo, err := parseNumeroOpcional(linha[3])
	if err != nil {
		return nil, err
	}
	if err := validarFaixaAlcada(valorMinimo, valorMaximo); err != nil {
		return nil, err
	}
	papelAprovador, err := campoObrigatorio(linha[4], "papel_aprovador")
	if err != nil {
		return nil, err
	}
	ativo, err := parseBoolCSV(linha[5])
	if err != nil {
		return nil, err
	}
	return []interface{}{filial, centroCustoCodigo, valorMinimo, valorMaximo, papelAprovador, ativo}, nil
}

func decodeCSVPapelPessoa(_ *sql.Tx, linha []string) ([]interface{}, error) {
	papel, err := campoObrigatorio(linha[0], "papel")
	if err != nil {
		return nil, err
	}
	pessoaNome, err := campoObrigatorio(linha[1], "pessoa_nome")
	if err != nil {
		return nil, err
	}
	return []interface{}{papel, pessoaNome}, nil
}

func decodeCSVFeriados(_ *sql.Tx, linha []string) ([]interface{}, error) {
	data, err := parseDataISO(linha[0])
	if err != nil {
		return nil, err
	}
	descricao, err := campoObrigatorio(linha[1], "descricao")
	if err != nil {
		return nil, err
	}
	return []interface{}{data, descricao}, nil
}

// decodeCSVCcExcecao resolve colaborador_email -> colaborador_id (usuarios) e
// centro_custo_codigo -> centro_custo_id (centros_custo), ambos
// case-insensitive, dentro da MESMA transação do import — Story 2.3
// (Boundaries da spec: qualquer um não encontrado rejeita a linha citando a
// causa, mesmo padrão atômico de centros-custo, não o best-effort de
// colaboradores.go).
func decodeCSVCcExcecao(tx *sql.Tx, linha []string) ([]interface{}, error) {
	email, err := campoObrigatorio(linha[0], "colaborador_email")
	if err != nil {
		return nil, err
	}
	colaboradorID, err := resolveColaboradorIDPorEmail(tx, email)
	if err != nil {
		return nil, err
	}

	centroCustoCodigo, err := campoObrigatorio(linha[1], "centro_custo_codigo")
	if err != nil {
		return nil, err
	}
	centroCustoID, err := resolveCentroCustoIDPorCodigo(tx, centroCustoCodigo)
	if err != nil {
		return nil, err
	}

	return []interface{}{colaboradorID, centroCustoID}, nil
}

// resolveColaboradorIDPorEmailOpcional é resolveColaboradorIDPorEmail, mas
// devolve nil (sem erro) quando o e-mail vem vazio — "sem pessoa nomeada"
// é um estado válido para regras-aprovacao/gerentes-aprovacao quando a linha
// é identificada por papel_aprovador (cargo) em vez de colaborador
// específico (Design Notes da spec: unifica pessoa/cargo/colegiado numa
// única linha).
func resolveColaboradorIDPorEmailOpcional(tx *sql.Tx, email string) (interface{}, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	id, err := resolveColaboradorIDPorEmail(tx, email)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// decodeCSVRegrasAprovacao (Story 3.1, FR-10) resolve colaborador_email
// opcional -> colaborador_id (usuarios), mesmo padrão case-insensitive de
// cc-excecao. "centro_custo_codigo" é texto livre SEM FK, mesmo padrão já
// adotado por "alcadas" (migration 003).
func decodeCSVRegrasAprovacao(tx *sql.Tx, linha []string) ([]interface{}, error) {
	precedencia, err := parseNumeroObrigatorio(linha[0])
	if err != nil {
		return nil, err
	}
	filial, err := campoObrigatorio(linha[1], "filial")
	if err != nil {
		return nil, err
	}
	centroCustoCodigo, err := campoObrigatorio(linha[2], "centro_custo_codigo")
	if err != nil {
		return nil, err
	}
	valorMinimo, err := parseNumeroObrigatorio(linha[3])
	if err != nil {
		return nil, err
	}
	valorMaximo, err := parseNumeroOpcional(linha[4])
	if err != nil {
		return nil, err
	}
	if err := validarFaixaAlcada(valorMinimo, valorMaximo); err != nil {
		return nil, err
	}
	colaboradorID, err := resolveColaboradorIDPorEmailOpcional(tx, linha[5])
	if err != nil {
		return nil, err
	}
	papelAprovador := parseStringOpcional(linha[6])
	if err := validarAutorRegra(colaboradorID, papelAprovador); err != nil {
		return nil, err
	}
	if err := validarAutorExclusivo(colaboradorID, papelAprovador); err != nil {
		return nil, err
	}
	ativo, err := parseBoolCSV(linha[7])
	if err != nil {
		return nil, err
	}
	return []interface{}{precedencia, filial, centroCustoCodigo, valorMinimo, valorMaximo, colaboradorID, papelAprovador, ativo}, nil
}

// decodeCSVGerentesAprovacao (Story 3.1, FR-10) resolve
// centro_custo_codigo/divisao_codigo/colaborador_email, todos opcionais ->
// centro_custo_id/divisao_id/colaborador_id — os dois primeiros nulos ao
// mesmo tempo = fallback global (Design Notes da spec); nunca os dois
// preenchidos ao mesmo tempo (validarCCXorDivisao).
func decodeCSVGerentesAprovacao(tx *sql.Tx, linha []string) ([]interface{}, error) {
	centroCustoCodigo := strings.TrimSpace(linha[0])
	var centroCustoID interface{}
	if centroCustoCodigo != "" {
		id, err := resolveCentroCustoIDPorCodigo(tx, centroCustoCodigo)
		if err != nil {
			return nil, err
		}
		centroCustoID = id
	}

	divisaoCodigo := strings.TrimSpace(linha[1])
	var divisaoID interface{}
	if divisaoCodigo != "" {
		id, err := resolveDivisaoID(tx, divisaoCodigo)
		if err != nil {
			return nil, err
		}
		divisaoID = id
	}

	if err := validarCCXorDivisao(centroCustoID, divisaoID); err != nil {
		return nil, err
	}

	colaboradorID, err := resolveColaboradorIDPorEmailOpcional(tx, linha[2])
	if err != nil {
		return nil, err
	}
	papelAprovador := parseStringOpcional(linha[3])
	if err := validarAutorRegra(colaboradorID, papelAprovador); err != nil {
		return nil, err
	}
	if err := validarAutorExclusivo(colaboradorID, papelAprovador); err != nil {
		return nil, err
	}

	teto, err := parseNumeroOpcionalComDefault(linha[4], 20000)
	if err != nil {
		return nil, err
	}
	ativo, err := parseBoolCSV(linha[5])
	if err != nil {
		return nil, err
	}

	return []interface{}{centroCustoID, divisaoID, colaboradorID, papelAprovador, teto, ativo}, nil
}

// decodeCSVAutorizadoresFormulario (Story 3.3, FR-6/FR-11) resolve
// colaborador_email -> colaborador_id (usuarios), mesmo padrão
// case-insensitive de cc-excecao/regras-aprovacao — mas aqui com a variante
// OBRIGATÓRIA (resolveColaboradorIDPorEmail, não ...Opcional):
// autorizadores_formulario é SEMPRE pessoa, nunca papel_aprovador
// (Boundaries "Always" da spec 3.3). "centro_custo_codigo" é texto livre SEM
// FK, mesmo padrão já adotado por alcadas/regras-aprovacao.
func decodeCSVAutorizadoresFormulario(tx *sql.Tx, linha []string) ([]interface{}, error) {
	centroCustoCodigo, err := campoObrigatorio(linha[0], "centro_custo_codigo")
	if err != nil {
		return nil, err
	}
	valorMinimo, err := parseNumeroObrigatorio(linha[1])
	if err != nil {
		return nil, err
	}
	valorMaximo, err := parseNumeroOpcional(linha[2])
	if err != nil {
		return nil, err
	}
	if err := validarFaixaAlcada(valorMinimo, valorMaximo); err != nil {
		return nil, err
	}
	email, err := campoObrigatorio(linha[3], "colaborador_email")
	if err != nil {
		return nil, err
	}
	colaboradorID, err := resolveColaboradorIDPorEmail(tx, email)
	if err != nil {
		return nil, err
	}
	ativo, err := parseBoolCSV(linha[4])
	if err != nil {
		return nil, err
	}
	return []interface{}{centroCustoCodigo, valorMinimo, valorMaximo, colaboradorID, ativo}, nil
}
