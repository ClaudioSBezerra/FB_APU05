package handlers

// usuarios_carga.go — carga em lote (CSV ';' + UTF-8 BOM opcional, mesmo
// parser de cadastros_csv.go) de usuários já com a identidade Oracle
// (cod_empresa/numcad, migration 019) preenchida — destrava testar o
// sistema com uma planilha real antes do conector ERP Bridge
// (erp-bridge-colaboradores/) estar rodando em produção.
//
// UPSERT por e-mail (case-insensitive): usuário novo recebe uma senha
// aleatória gerada aqui e descartada na hora — só o hash bcrypt é gravado,
// o texto puro nunca é persistido, logado ou devolvido na resposta. A
// pessoa define a própria senha via "Esqueci minha senha" (SMTP já
// configurado). Usuário já existente tem nome/perfil/identidade/CC
// atualizados; senha nunca é tocada por este handler.

import (
	"crypto/rand"
	"database/sql"
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

var cabecalhoUsuariosCSV = []string{"email", "nome", "perfil", "cod_empresa", "numcad", "centro_custo_codigo"}

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

// CarregarUsuariosHandler — POST /api/admin/usuarios/carga (corpo = CSV cru,
// cabecalhoUsuariosCSV). Registrado atrás de RequireAuth(..., "administrador"),
// mesmo nível de permissão das demais cargas administrativas.
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

		linhas, err := lerCadastroCSV(corpo, cabecalhoUsuariosCSV)
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
			email := strings.TrimSpace(linha.Campos[0])
			nome := strings.TrimSpace(linha.Campos[1])
			perfil := strings.TrimSpace(linha.Campos[2])
			codEmpresaStr := strings.TrimSpace(linha.Campos[3])
			numcadStr := strings.TrimSpace(linha.Campos[4])
			centroCustoCodigo := strings.TrimSpace(linha.Campos[5])

			emailChave := strings.ToLower(email)
			if primeira, visto := primeiraLinhaPorEmail[emailChave]; visto {
				resultado.Erros = append(resultado.Erros, fmt.Sprintf(
					"linha %d: e-mail repetido (já apareceu na linha %d deste arquivo) — mantida só a primeira ocorrência; para dar acesso a outro centro de custo à mesma pessoa, cadastre uma exceção de CC (Cadastros > cc-excecao)",
					linha.Numero, primeira,
				))
				resultado.Ignorados++
				continue
			}
			primeiraLinhaPorEmail[emailChave] = linha.Numero

			msgErro, codEmpresa, numcad, centroCustoID := validarELerLinhaUsuario(
				tx, linha.Numero, email, nome, perfil, codEmpresaStr, numcadStr, centroCustoCodigo,
			)
			if msgErro != "" {
				resultado.Erros = append(resultado.Erros, msgErro)
				resultado.Ignorados++
				continue
			}

			var existeID string
			err = tx.QueryRow(`SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)`, email).Scan(&existeID)
			switch {
			case err == sql.ErrNoRows:
				perfilNovo := perfil
				if perfilNovo == "" {
					perfilNovo = "solicitante"
				}
				hash, herr := hashSenhaDescartavel(gerarSenhaAleatoria())
				if herr != nil {
					log.Printf("[UsuariosCarga] Erro ao gerar hash de senha (linha %d): %v", linha.Numero, herr)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				_, err = tx.Exec(`
					INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
				`, email, nome, perfilNovo, hash, codEmpresa, numcad, centroCustoID)
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
				// perfil vazio preserva o atual (NULLIF($2,'') -> NULL ->
				// COALESCE devolve o valor já gravado); centro_custo_id vazio
				// preserva cc_proprio_id atual, mesmo princípio.
				_, err = tx.Exec(`
					UPDATE usuarios SET
						nome = $1,
						perfil = COALESCE(NULLIF($2, ''), perfil),
						cod_empresa = $3,
						numcad = $4,
						cc_proprio_id = COALESCE($5, cc_proprio_id),
						updated_at = now()
					WHERE id = $6
				`, nome, perfil, codEmpresa, numcad, centroCustoID, existeID)
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
// acesso a banco, exceto a resolução opcional de centro_custo_codigo, que
// precisa rodar na MESMA transação do import) — devolve (mensagem de erro,
// "", "", nil, nil) para rejeitar só esta linha, nunca abortar a carga
// inteira, mesmo princípio de CarregarColaboradoresHandler.
func validarELerLinhaUsuario(
	tx *sql.Tx, numeroLinha int, email, nome, perfil, codEmpresaStr, numcadStr, centroCustoCodigo string,
) (msgErro string, codEmpresa, numcad int, centroCustoID interface{}) {
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Sprintf("linha %d: e-mail inválido", numeroLinha), 0, 0, nil
	}
	if nome == "" {
		return fmt.Sprintf("linha %d: campo 'nome' obrigatório", numeroLinha), 0, 0, nil
	}
	if perfil != "" && perfil != "administrador" && perfil != "solicitante" {
		return fmt.Sprintf("linha %d: perfil deve ser 'administrador' ou 'solicitante' (ou vazio)", numeroLinha), 0, 0, nil
	}
	codEmpresa, convErr := strconv.Atoi(codEmpresaStr)
	if convErr != nil || codEmpresa <= 0 {
		return fmt.Sprintf("linha %d: campo 'cod_empresa' deve ser um número positivo", numeroLinha), 0, 0, nil
	}
	numcad, convErr = strconv.Atoi(numcadStr)
	if convErr != nil || numcad <= 0 {
		return fmt.Sprintf("linha %d: campo 'numcad' deve ser um número positivo", numeroLinha), 0, 0, nil
	}
	if centroCustoCodigo != "" {
		// resolveCentroCustoIDPorCodigo (cadastros_csv.go) já devolve uma
		// mensagem pronta para "não encontrado" — mesmo padrão que todo
		// decodeCSV* já segue (repassa o erro como rejeição da linha, nunca
		// como falha de servidor).
		id, resolveErr := resolveCentroCustoIDPorCodigo(tx, centroCustoCodigo)
		if resolveErr != nil {
			return fmt.Sprintf("linha %d: %v", numeroLinha, resolveErr), 0, 0, nil
		}
		centroCustoID = id
	}
	return "", codEmpresa, numcad, centroCustoID
}
