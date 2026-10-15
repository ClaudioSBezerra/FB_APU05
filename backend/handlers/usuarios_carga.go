package handlers

// usuarios_carga.go — carga em lote (CSV ';' + UTF-8 BOM opcional) de
// usuários já com a identidade Oracle (cod_empresa/numcad, migration 019)
// preenchida — destrava testar o sistema com uma planilha real antes do
// conector ERP Bridge (erp-bridge-colaboradores/) estar rodando em
// produção.
//
// Parser PRÓPRIO (não lerCadastroCSV/cadastros_csv.go): colunas resolvidas
// por NOME no cabeçalho, não por posição fixa — tolera reordenar,
// maiúsculas/minúsculas, e ignora colunas extras (ex. "perfil", presente ou
// não no arquivo, nunca é usada: todo usuário criado nasce 'solicitante').
// Só email/nome/cod_empresa/numcad são obrigatórias; centro_custo_codigo e
// centro_custo_nome são opcionais. Motivo de não reaproveitar
// lerCadastroCSV: aquele exige cabeçalho EXATO (mesmo texto, mesma ordem),
// e essa planilha real já rejeitou por isso mais de uma vez.
//
// UPSERT por e-mail (case-insensitive): usuário novo recebe uma senha
// aleatória gerada aqui e descartada na hora — só o hash bcrypt é gravado,
// o texto puro nunca é persistido, logado ou devolvido na resposta. A
// pessoa define a própria senha via "Esqueci minha senha" (SMTP já
// configurado). Usuário já existente tem nome/identidade/CC atualizados;
// senha e perfil nunca são tocados por este handler (perfil só muda
// individualmente, tela de Gestão de Usuários).
//
// centro_custo_codigo sem cadastro ainda: se a linha também trouxer
// centro_custo_nome, o centro de custo é CRIADO automaticamente aqui
// (preso a uma divisão placeholder "SEM-DIVISAO", criada uma única vez) —
// nunca bloqueia a criação do usuário. Sem nome, vira só um aviso (CC fica
// em branco). Corrigir a divisão real de cada CC depois é
// responsabilidade do administrador, na tela de Cadastros (que já tem
// histórico/restauração).

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type linhaUsuarioCSV struct {
	Numero            int
	Email             string
	Nome              string
	CodEmpresaStr     string
	NumcadStr         string
	CentroCustoCodigo string
	CentroCustoNome   string
}

// colunasObrigatoriasUsuariosCSV — únicas colunas sem as quais o arquivo é
// rejeitado inteiro (cabeçalho mal formado); as demais (centro_custo_codigo,
// centro_custo_nome) são opcionais, e qualquer outra coluna (ex. "perfil")
// é ignorada.
var colunasObrigatoriasUsuariosCSV = []string{"email", "nome", "cod_empresa", "numcad"}

// lerUsuariosCSV faz parse do CSV resolvendo colunas pelo NOME do
// cabeçalho (minúsculas, após TrimSpace) — não pela posição. BOM UTF-8
// opcional, separador ';', mesmo padrão de lerCadastroCSV (cadastros_csv.go)
// quanto a aspas/encoding, mas com cabeçalho flexível (ver comentário de
// topo do arquivo).
// primeiroIndiceEncontrado procura, em ordem, cada nome de coluna aceito
// (sinônimos) no índice de cabeçalho — devolve o primeiro que existir.
// Usado para aceitar mais de um nome para a mesma coluna opcional (ex.
// "centro_custo_nome" ou "nomccu", a convenção que a query real do
// negócio já usa) sem precisar que a pessoa renomeie a planilha.
func primeiroIndiceEncontrado(indice map[string]int, nomes ...string) (int, bool) {
	for _, nome := range nomes {
		if i, ok := indice[nome]; ok {
			return i, true
		}
	}
	return 0, false
}

func lerUsuariosCSV(corpo []byte) ([]linhaUsuarioCSV, error) {
	corpo = bytes.TrimPrefix(corpo, bomUTF8)

	reader := csv.NewReader(bytes.NewReader(corpo))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("arquivo CSV vazio")
	}
	if err != nil {
		return nil, fmt.Errorf("erro ao ler cabeçalho do CSV: %v", err)
	}

	indice := make(map[string]int, len(header))
	for i, campo := range header {
		indice[strings.ToLower(strings.TrimSpace(campo))] = i
	}
	for _, col := range colunasObrigatoriasUsuariosCSV {
		if _, ok := indice[col]; !ok {
			return nil, fmt.Errorf("cabeçalho do CSV precisa ter a coluna '%s' (colunas obrigatórias: %s)", col, strings.Join(colunasObrigatoriasUsuariosCSV, ", "))
		}
	}

	var linhas []linhaUsuarioCSV
	numeroLinha := 1
	for {
		numeroLinha++
		campos, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("linha %d: erro ao ler CSV: %v", numeroLinha, err)
		}
		if len(campos) != len(header) {
			return nil, fmt.Errorf("linha %d: esperado %d coluna(s) (mesmo número do cabeçalho), recebido %d", numeroLinha, len(header), len(campos))
		}

		linha := linhaUsuarioCSV{
			Numero:        numeroLinha,
			Email:         strings.TrimSpace(campos[indice["email"]]),
			Nome:          strings.TrimSpace(campos[indice["nome"]]),
			CodEmpresaStr: strings.TrimSpace(campos[indice["cod_empresa"]]),
			NumcadStr:     strings.TrimSpace(campos[indice["numcad"]]),
		}
		if i, ok := primeiroIndiceEncontrado(indice, "centro_custo_codigo"); ok {
			linha.CentroCustoCodigo = strings.TrimSpace(campos[i])
		}
		// "nomccu" é o nome que a query real do negócio já usa (nome do
		// centro de custo, convenção Senior/Oracle) — aceito como sinônimo
		// de centro_custo_nome em vez de pedir pra renomear a planilha de
		// novo.
		if i, ok := primeiroIndiceEncontrado(indice, "centro_custo_nome", "nomccu"); ok {
			linha.CentroCustoNome = strings.TrimSpace(campos[i])
		}
		linhas = append(linhas, linha)
	}
	return linhas, nil
}

type usuarioCargaResultado struct {
	Criados     int      `json:"criados"`
	Atualizados int      `json:"atualizados"`
	Ignorados   int      `json:"ignorados"`
	Total       int      `json:"total"`
	Erros       []string `json:"erros"`
}

func gerarSenhaAleatoria() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// hashSenhaDescartavel cifra a senha aleatória de uma conta recém-criada por
// esta carga com bcrypt.MinCost, NUNCA o custo 14 de HashPassword (auth.go).
// Diferença deliberada: essa senha tem 128 bits de aleatoriedade, é
// descartada no mesmo instante em que é gerada e nunca é usada para logar de
// verdade (a pessoa define a própria senha via "Esqueci minha senha") — um
// custo alto não protege nada aqui, só multiplica CPU por centena de linhas.
// Causou um incidente real: uma carga de ~500 contas novas gastava minutos
// de CPU só em bcrypt custo 14 sequencial dentro de uma única transação,
// suficiente para travar outras rotas (Gestão de Usuários, ERP Bridge) até o
// gateway dar timeout.
func hashSenhaDescartavel(senha string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.MinCost)
	return string(bytes), err
}

// obterOuCriarDivisaoPadrao devolve o id de uma divisão placeholder
// "SEM-DIVISAO", criando-a na primeira vez que for necessária — usada
// exclusivamente como FK obrigatória (divisao_id NOT NULL,
// migrations/003_cadastros_administraveis.sql) para um centro de custo
// criado automaticamente por esta carga (obterOuCriarCentroCusto). A
// divisão real de cada CC pode ser corrigida depois na tela de Cadastros,
// que já tem histórico/restauração — esta função nunca decide a divisão
// definitiva, só destrava a criação agora.
func obterOuCriarDivisaoPadrao(tx *sql.Tx) (string, error) {
	const codigo = "SEM-DIVISAO"
	var id string
	err := tx.QueryRow(`
		INSERT INTO divisoes (codigo, nome)
		VALUES ($1, 'Sem divisão definida (criada automaticamente pela carga de usuários)')
		ON CONFLICT (codigo) DO UPDATE SET codigo = EXCLUDED.codigo
		RETURNING id
	`, codigo).Scan(&id)
	return id, err
}

// obterOuCriarCentroCusto resolve centro_custo_codigo (mesma busca
// case-insensitive de resolveCentroCustoIDPorCodigo) e, se não existir,
// cria o cadastro na hora — preso à divisão placeholder acima. Nunca
// escreve em cadastro_historico (diferente de ImportarCadastroHandler): é
// um atalho de bootstrap, não o caminho administrativo "de verdade"
// (Cadastros > centros-custo), que continua sendo o lugar certo para
// editar/corrigir esses registros depois.
func obterOuCriarCentroCusto(tx *sql.Tx, codigo, nome string) (string, error) {
	if id, err := resolveCentroCustoIDPorCodigo(tx, codigo); err == nil {
		return id, nil
	}
	divisaoID, err := obterOuCriarDivisaoPadrao(tx)
	if err != nil {
		return "", fmt.Errorf("divisão padrão: %w", err)
	}
	var id string
	err = tx.QueryRow(`
		INSERT INTO centros_custo (codigo, nome, divisao_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (codigo) DO UPDATE SET codigo = EXCLUDED.codigo
		RETURNING id
	`, codigo, nome, divisaoID).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// CarregarUsuariosHandler — POST /api/admin/usuarios/carga (corpo = CSV cru,
// colunasObrigatoriasUsuariosCSV). Registrado atrás de
// RequireAuth(..., "administrador"), mesmo nível de permissão das demais
// cargas administrativas.
func CarregarUsuariosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		linhas, err := lerUsuariosCSV(corpo)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[UsuariosCarga] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		resultado := usuarioCargaResultado{Total: len(linhas), Erros: []string{}}

		// primeiraLinhaPorEmail detecta o mesmo e-mail repetido DENTRO deste
		// arquivo (ex.: sócio que atua em várias empresas, uma linha por
		// empresa) — a identidade (cod_empresa/numcad/cc_proprio_id) só é
		// gravada uma vez, na primeira ocorrência; as demais são rejeitadas
		// com uma mensagem que aponta o caminho certo (cc-excecao), nunca um
		// "last write wins" silencioso que apagaria a empresa/CC lida
		// primeiro sem avisar. Acesso agregado a CCs de outras empresas para
		// a mesma pessoa é Story 2.3 (cc-excecao) — nunca gravado por aqui
		// (mesmo limite de AD-8: esta carga nunca escreve cc_excecao).
		primeiraLinhaPorEmail := make(map[string]int)

		for _, linha := range linhas {
			emailChave := strings.ToLower(linha.Email)
			if primeira, visto := primeiraLinhaPorEmail[emailChave]; visto {
				resultado.Erros = append(resultado.Erros, fmt.Sprintf(
					"linha %d: e-mail repetido (já apareceu na linha %d deste arquivo) — mantida só a primeira ocorrência; para dar acesso a outro centro de custo à mesma pessoa, cadastre uma exceção de CC (Cadastros > cc-excecao)",
					linha.Numero, primeira,
				))
				resultado.Ignorados++
				continue
			}
			primeiraLinhaPorEmail[emailChave] = linha.Numero

			msgErro, codEmpresa, numcad, centroCustoID, avisoCC := validarELerLinhaUsuario(
				tx, linha.Numero, linha.Email, linha.Nome, linha.CodEmpresaStr, linha.NumcadStr,
				linha.CentroCustoCodigo, linha.CentroCustoNome,
			)
			if msgErro != "" {
				resultado.Erros = append(resultado.Erros, msgErro)
				resultado.Ignorados++
				continue
			}
			if avisoCC != "" {
				resultado.Erros = append(resultado.Erros, avisoCC)
			}

			email := linha.Email
			nome := linha.Nome

			var existeID string
			err = tx.QueryRow(`SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)`, email).Scan(&existeID)
			switch {
			case err == sql.ErrNoRows:
				hash, herr := hashSenhaDescartavel(gerarSenhaAleatoria())
				if herr != nil {
					log.Printf("[UsuariosCarga] Erro ao gerar hash de senha (linha %d): %v", linha.Numero, herr)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				_, err = tx.Exec(`
					INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)
					VALUES ($1, $2, 'solicitante', $3, $4, $5, $6)
				`, email, nome, hash, codEmpresa, numcad, centroCustoID)
				if err != nil {
					if isUniqueViolationOnConstraint(err, "idx_usuarios_cod_empresa_numcad") {
						resultado.Erros = append(resultado.Erros, fmt.Sprintf("linha %d: cod_empresa+numcad já usado por outro usuário", linha.Numero))
						resultado.Ignorados++
						continue
					}
					log.Printf("[UsuariosCarga] Erro ao criar usuário (linha %d, %s): %v", linha.Numero, email, err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				resultado.Criados++

			case err != nil:
				log.Printf("[UsuariosCarga] Erro ao buscar usuário (linha %d, %s): %v", linha.Numero, email, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return

			default:
				// Nunca toca "perfil" aqui — promover a administrador
				// continua sendo feito individualmente na tela de Gestão de
				// Usuários. centro_custo_id vazio preserva cc_proprio_id
				// atual (COALESCE).
				_, err = tx.Exec(`
					UPDATE usuarios SET
						nome = $1,
						cod_empresa = $2,
						numcad = $3,
						cc_proprio_id = COALESCE($4, cc_proprio_id),
						updated_at = now()
					WHERE id = $5
				`, nome, codEmpresa, numcad, centroCustoID, existeID)
				if err != nil {
					if isUniqueViolationOnConstraint(err, "idx_usuarios_cod_empresa_numcad") {
						resultado.Erros = append(resultado.Erros, fmt.Sprintf("linha %d: cod_empresa+numcad já usado por outro usuário", linha.Numero))
						resultado.Ignorados++
						continue
					}
					log.Printf("[UsuariosCarga] Erro ao atualizar usuário (linha %d, %s): %v", linha.Numero, email, err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				resultado.Atualizados++
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[UsuariosCarga] Erro ao commitar carga: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		atorID := GetUserIDFromContext(r)
		log.Printf("[UsuariosCarga] %s carregou usuários: %d criados, %d atualizados, %d ignorados (total %d)",
			atorID, resultado.Criados, resultado.Atualizados, resultado.Ignorados, resultado.Total)

		_ = json.NewEncoder(w).Encode(resultado)
	}
}

// validarELerLinhaUsuario valida e converte uma linha estruturalmente (sem
// acesso a banco, exceto a resolução/criação de centro_custo_codigo, que
// precisa rodar na MESMA transação do import). msgErro != "" rejeita só
// esta linha (nunca aborta a carga inteira, mesmo princípio de
// CarregarColaboradoresHandler) — reservado para dado estruturalmente
// inválido (e-mail/nome/cod_empresa/numcad). centro_custo_codigo não
// encontrado NUNCA entra em msgErro: ou é criado na hora (se
// centroCustoNome vier preenchido) ou vira avisoCC — nenhum dos dois
// bloqueia a linha.
func validarELerLinhaUsuario(
	tx *sql.Tx, numeroLinha int, email, nome, codEmpresaStr, numcadStr, centroCustoCodigo, centroCustoNome string,
) (msgErro string, codEmpresa, numcad int, centroCustoID interface{}, avisoCC string) {
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Sprintf("linha %d: e-mail inválido", numeroLinha), 0, 0, nil, ""
	}
	if nome == "" {
		return fmt.Sprintf("linha %d: campo 'nome' obrigatório", numeroLinha), 0, 0, nil, ""
	}
	codEmpresa, convErr := strconv.Atoi(codEmpresaStr)
	if convErr != nil || codEmpresa <= 0 {
		return fmt.Sprintf("linha %d: campo 'cod_empresa' deve ser um número positivo", numeroLinha), 0, 0, nil, ""
	}
	numcad, convErr = strconv.Atoi(numcadStr)
	if convErr != nil || numcad <= 0 {
		return fmt.Sprintf("linha %d: campo 'numcad' deve ser um número positivo", numeroLinha), 0, 0, nil, ""
	}
	if centroCustoCodigo != "" {
		if id, resolveErr := resolveCentroCustoIDPorCodigo(tx, centroCustoCodigo); resolveErr == nil {
			centroCustoID = id
		} else if centroCustoNome != "" {
			novoID, criarErr := obterOuCriarCentroCusto(tx, centroCustoCodigo, centroCustoNome)
			if criarErr != nil {
				avisoCC = fmt.Sprintf("linha %d: centro de custo %q não encontrado e não foi possível criar automaticamente (%v) — usuário criado/atualizado mesmo assim, sem CC definido", numeroLinha, centroCustoCodigo, criarErr)
			} else {
				centroCustoID = novoID
			}
		} else {
			avisoCC = fmt.Sprintf("linha %d: centro de custo %q não encontrado — usuário criado/atualizado mesmo assim, sem CC definido", numeroLinha, centroCustoCodigo)
		}
	}
	return "", codEmpresa, numcad, centroCustoID, avisoCC
}
