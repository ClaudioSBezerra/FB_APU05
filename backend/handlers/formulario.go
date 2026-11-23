package handlers

// formulario.go — opções de formulário para abrir solicitação (Epic 3).
// Lê cadastros mestres do Epic 2 em modo somente-leitura, para qualquer
// perfil autenticado (mesmo nível de permissão de AbrirSolicitacaoHandler,
// RequireAuth(..., "")) — nenhuma rota aqui grava nada; cada uma só devolve
// dados que o próprio AbrirSolicitacaoHandler já valida no servidor
// (solicitacoes.go), para popular selects/combos no front-end sem o
// solicitante precisar saber UUIDs de cor.
//
// centrosCustoAutorizados replica a MESMA regra de
// solicitanteAutorizadoParaCC (solicitacoes.go): cc_proprio_id do usuário
// OU qualquer CC em cc_excecao — nunca uma lista maior que isso, mesmo para
// administrador (a rota de abertura de solicitação não dá tratamento
// especial a administrador).

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"fb_apu05/internal/sapodata"
)

type opcaoItem struct {
	ID     string `json:"id"`
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

type centroCustoOpcao struct {
	ID        string `json:"id"`
	Codigo    string `json:"codigo"`
	Nome      string `json:"nome"`
	DivisaoID string `json:"divisao_id"`
}

type autorizadorOpcao struct {
	ColaboradorID string `json:"colaborador_id"`
	Nome          string `json:"nome"`
}

func listarOpcoes(db *sql.DB, query string) ([]opcaoItem, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	itens := make([]opcaoItem, 0)
	for rows.Next() {
		var item opcaoItem
		if err := rows.Scan(&item.ID, &item.Codigo, &item.Nome); err != nil {
			return nil, err
		}
		itens = append(itens, item)
	}
	return itens, rows.Err()
}

// todosCentrosCusto lista todos os CCs (destino de Transferência).
func todosCentrosCusto(db *sql.DB) ([]centroCustoOpcao, error) {
	rows, err := db.Query(`SELECT id, codigo, nome, divisao_id FROM centros_custo ORDER BY codigo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	itens := make([]centroCustoOpcao, 0)
	for rows.Next() {
		var item centroCustoOpcao
		if err := rows.Scan(&item.ID, &item.Codigo, &item.Nome, &item.DivisaoID); err != nil {
			return nil, err
		}
		itens = append(itens, item)
	}
	return itens, rows.Err()
}

// origemLivre: fase de testes — na transferência a origem pode ser qualquer centro de custo
// (configuração teste_origem_todos_cc, ligada/desligada em Configurações).
func origemLivre(q interface {
	QueryRow(query string, args ...any) *sql.Row
}) bool {
	var v string
	if err := q.QueryRow(`SELECT valor FROM configuracoes WHERE chave = 'teste_origem_todos_cc'`).Scan(&v); err != nil {
		return false
	}
	return v == "true"
}

func centrosCustoAutorizados(db *sql.DB, usuarioID string) ([]centroCustoOpcao, error) {
	rows, err := db.Query(`
		SELECT cc.id, cc.codigo, cc.nome, cc.divisao_id
		FROM centros_custo cc
		WHERE cc.id = (SELECT cc_proprio_id FROM usuarios WHERE id = $1)

		UNION

		SELECT cc.id, cc.codigo, cc.nome, cc.divisao_id
		FROM centros_custo cc
		JOIN cc_excecao e ON e.centro_custo_id = cc.id
		WHERE e.colaborador_id = $1

		ORDER BY nome
	`, usuarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	itens := make([]centroCustoOpcao, 0)
	for rows.Next() {
		var item centroCustoOpcao
		if err := rows.Scan(&item.ID, &item.Codigo, &item.Nome, &item.DivisaoID); err != nil {
			return nil, err
		}
		itens = append(itens, item)
	}
	return itens, rows.Err()
}

// OpcoesFormularioHandler — GET /api/formulario/opcoes. Agrega, numa única
// chamada, todo cadastro mestre que um formulário de abertura de
// solicitação (qualquer um dos 5 tipos) precisa para popular combos:
// centros de custo autorizados do usuário logado, divisões, contas (do plano de
// contas do SAP), classes de imobilizado, locais
// de obra e subgrupos de despesa.
func OpcoesFormularioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		usuarioID := GetUserIDFromContext(r)

		centrosCusto, err := centrosCustoAutorizados(db, usuarioID)
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar centros de custo autorizados: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		divisoes, err := listarOpcoes(db, "SELECT id, codigo, nome FROM divisoes ORDER BY nome")
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar divisões: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		contas, err := listarOpcoes(db, "SELECT id, codigo, nome FROM contas WHERE plano = '"+sapodata.PlanoContas()+"' ORDER BY nome")
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar contas: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		classesImobilizado, err := listarOpcoes(db, "SELECT id, codigo, nome FROM classes_imobilizado ORDER BY nome")
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar classes de imobilizado: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		locaisObra, err := listarOpcoes(db, "SELECT id, codigo, nome FROM locais_obra ORDER BY nome")
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar locais de obra: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		subgruposDespesa, err := listarOpcoes(db, "SELECT id, codigo, nome FROM subgrupos_despesa ORDER BY nome")
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar subgrupos de despesa: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		// Destino de Transferência (11/11, item 2): pode ser QUALQUER CC, não
		// só os autorizados ao solicitante (a permissão vale para a origem).
		centrosCustoOrigem := centrosCusto
		if origemLivre(db) {
			if centrosCustoOrigem, err = todosCentrosCusto(db); err != nil {
				log.Printf("[Formulario] Erro ao consultar centros de custo de origem: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		centrosCustoDestino, err := todosCentrosCusto(db)
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar centros de custo de destino: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"centros_custo":         centrosCusto,
			"centros_custo_destino": centrosCustoDestino,
			"centros_custo_origem":  centrosCustoOrigem,
			"divisoes":              divisoes,
			"contas":                contas,
			"classes_imobilizado":   classesImobilizado,
			"locais_obra":           locaisObra,
			"subgrupos_despesa":     subgruposDespesa,
		})
	}
}

// AutorizadoresHandler — GET /api/formulario/autorizadores?tipo=&centro_custo_codigo=&valor=.
// Lista os MESMOS candidatos que AutorizadorNominal.Resolve aceitaria
// (internal/aprovacao/nominal.go) — nunca uma lista mais ampla: a consulta
// abaixo é deliberadamente idêntica à de lá (duas implementações da mesma
// regra seria fonte de divergência). `tipo=obras` ignora
// centro_custo_codigo (Obras não tem CC) e usa aprovadores_obra em vez de
// autorizadores_formulario, mesmo branch tipo-aware de resolveObras.
func AutorizadoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tipo := r.URL.Query().Get("tipo")
		if tipo != "inclusao" && tipo != "imobilizado" && tipo != "obras" {
			jsonErr(w, http.StatusBadRequest, "parâmetro 'tipo' deve ser 'inclusao', 'imobilizado' ou 'obras'")
			return
		}

		valor, err := strconv.ParseFloat(r.URL.Query().Get("valor"), 64)
		if err != nil || valor <= 0 {
			jsonErr(w, http.StatusBadRequest, "parâmetro 'valor' deve ser um número positivo")
			return
		}

		// Em ambos os branches, inclui também (via UNION) qualquer usuário
		// marcado usuarios.aprovador=true (Gestão de Usuários, 20/10) —
		// esses aparecem na lista independente de CC/faixa/teto, pois são
		// reconhecidos como aprovadores em qualquer solicitação (ex.
		// diretoria). UNION (não UNION ALL) evita duplicar quem já esteja
		// nominalmente cadastrado E marcado como aprovador global.
		var rows *sql.Rows
		if tipo == "obras" {
			rows, err = db.Query(`
				SELECT u.id, u.nome
				FROM aprovadores_obra ao
				JOIN usuarios u ON u.id = ao.colaborador_id
				WHERE ao.ativo = true AND ao.teto >= $1
				UNION
				SELECT u.id, u.nome
				FROM usuarios u
				WHERE u.aprovador = true
				ORDER BY nome
			`, valor)
		} else {
			centroCustoCodigo := r.URL.Query().Get("centro_custo_codigo")
			if centroCustoCodigo == "" {
				jsonErr(w, http.StatusBadRequest, "parâmetro 'centro_custo_codigo' é obrigatório para este tipo")
				return
			}
			rows, err = db.Query(`
				SELECT u.id, u.nome
				FROM autorizadores_formulario af
				JOIN usuarios u ON u.id = af.colaborador_id
				WHERE af.ativo = true
				  AND af.centro_custo_codigo = $1
				  AND af.valor_minimo <= $2
				  AND (af.valor_maximo IS NULL OR af.valor_maximo >= $2)
				UNION
				SELECT u.id, u.nome
				FROM usuarios u
				WHERE u.aprovador = true
				ORDER BY nome
			`, centroCustoCodigo, valor)
		}
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar autorizadores: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		itens := make([]autorizadorOpcao, 0)
		for rows.Next() {
			var item autorizadorOpcao
			if err := rows.Scan(&item.ColaboradorID, &item.Nome); err != nil {
				log.Printf("[Formulario] Erro ao ler linha de autorizadores: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			itens = append(itens, item)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Formulario] Erro ao iterar autorizadores: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": itens})
	}
}

// ObraOrdensHandler — GET /api/formulario/obra-ordens?local_obra_id=&subgrupo_despesa_id=.
// Lista as ordens de investimento já existentes no SAP para o par
// local/subgrupo escolhido — o literal "CRIAR" (Story 3.5) é sempre uma
// opção válida no front-end e nunca vem desta listagem (só nasce de fato na
// finalização, Epic 4).
func ObraOrdensHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		localObraID := r.URL.Query().Get("local_obra_id")
		subgrupoDespesaID := r.URL.Query().Get("subgrupo_despesa_id")
		if !uuidFormatRegexp.MatchString(localObraID) || !uuidFormatRegexp.MatchString(subgrupoDespesaID) {
			jsonErr(w, http.StatusBadRequest, "parâmetros 'local_obra_id' e 'subgrupo_despesa_id' devem ser UUIDs válidos")
			return
		}

		rows, err := db.Query(`
			SELECT numero_ordem
			FROM obra_ordens
			WHERE local_obra_id = $1 AND subgrupo_despesa_id = $2
			ORDER BY numero_ordem
		`, localObraID, subgrupoDespesaID)
		if err != nil {
			log.Printf("[Formulario] Erro ao consultar ordens de obra: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		ordens := make([]string, 0)
		for rows.Next() {
			var numero string
			if err := rows.Scan(&numero); err != nil {
				log.Printf("[Formulario] Erro ao ler linha de ordens de obra: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			ordens = append(ordens, numero)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Formulario] Erro ao iterar ordens de obra: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": ordens})
	}
}

type fornecedorOpcao struct {
	ID     string `json:"id"`
	CGC    string `json:"cgc"`
	Razsoc string `json:"razsoc"`
	Fantas string `json:"fantas"`
	UF     string `json:"uf"`
}

// fornecedoresLimiteBusca é o teto de resultados de FornecedoresHandler —
// mesmo espírito de CampoReferenciaEntidade (frontend/CadastrosMestres.tsx):
// uma busca list box para o formulário de solicitação, não a listagem
// paginada de administração (essa é GET /api/admin/cadastros/fornecedores,
// que entrou no cadastroRegistry em 20/10 — handlers/cadastros.go).
const fornecedoresLimiteBusca = 20

// FornecedoresHandler — GET /api/formulario/fornecedores?busca=. Busca por
// cgc/razsoc/fantas (ILIKE, case-insensitive) — sem `busca`, devolve vazio
// (nunca a tabela inteira: pensado para autocomplete, não para listagem).
func FornecedoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		busca := r.URL.Query().Get("busca")
		itens := make([]fornecedorOpcao, 0)
		if busca != "" {
			padrao := "%" + busca + "%"
			rows, err := db.Query(`
				SELECT id, cgc, razsoc, COALESCE(fantas, ''), COALESCE(uf, '')
				FROM fornecedores
				WHERE cgc ILIKE $1 OR razsoc ILIKE $1 OR fantas ILIKE $1
				ORDER BY razsoc
				LIMIT $2
			`, padrao, fornecedoresLimiteBusca)
			if err != nil {
				log.Printf("[Formulario] Erro ao consultar fornecedores: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			defer rows.Close()

			for rows.Next() {
				var item fornecedorOpcao
				if err := rows.Scan(&item.ID, &item.CGC, &item.Razsoc, &item.Fantas, &item.UF); err != nil {
					log.Printf("[Formulario] Erro ao ler linha de fornecedores: %v", err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				itens = append(itens, item)
			}
			if err := rows.Err(); err != nil {
				log.Printf("[Formulario] Erro ao iterar fornecedores: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": itens})
	}
}
