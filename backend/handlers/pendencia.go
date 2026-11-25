package handlers

// pendencia.go — Story 4.2 (Pendência e reabertura, Epic 4). Continua o
// Writer path único de internal/fila aberto pela Story 4.1: duas novas
// escritas em `status`/`versao` de `solicitacoes` (MarcarPendenciaHandler,
// ComentarSolicitacaoHandler) — mesma linha/lock do AD-4, mesma conexão
// PRIVILEGIADA (withPrivilegedDB). ObterSolicitacaoHandler é a única rota
// nova desta story que é só leitura (withDB) — expõe o histórico de
// `solicitacao_comentarios` e a `versao` atual a quem é dono da
// solicitação (solicitante ou administrador), para o cliente montar o
// próximo `{"versao":N,...}` de MarcarPendencia/Comentar/Assumir.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu05/internal/anexos"
	"fb_apu05/internal/fila"
)

// pendenciaOuComentarioRequest é o corpo compartilhado por POST
// /api/solicitacoes/{id}/pendencia e POST /api/solicitacoes/{id}/comentarios
// — `administrador_id`/`solicitante_id` (autor) NUNCA vem do corpo
// (Boundaries "Always" da spec: sempre de GetUserIDFromContext, mesmo
// princípio de assumirSolicitacaoRequest).
type pendenciaOuComentarioRequest struct {
	Versao     int    `json:"versao"`
	Comentario string `json:"comentario"`
}

// lerPendenciaOuComentarioRequest lê e valida o corpo compartilhado pelos
// 2 handlers de escrita desta story: `versao>=1` (mesmo achado de revisão
// da Story 4.1 — sem isso, versao=0 decairia para 409 conflito_versao em
// vez de um 400 de validação claro) e `comentario` não-vazio após
// TrimSpace (Boundaries "Never" da spec: sem CHECK de tamanho no banco, só
// esta validação no handler). Devolve o comentário já com TrimSpace
// aplicado — nunca o texto bruto do corpo.
func lerPendenciaOuComentarioRequest(w http.ResponseWriter, r *http.Request) (versao int, comentario string, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return 0, "", false
	}

	var req pendenciaOuComentarioRequest
	if err := json.Unmarshal(corpo, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "Os dados enviados estão incompletos ou em formato inválido. Atualize a página e tente de novo.")
		return 0, "", false
	}

	if req.Versao < 1 {
		jsonErr(w, http.StatusBadRequest, "campo 'versao' é obrigatório e deve ser >= 1")
		return 0, "", false
	}

	comentarioLimpo := strings.TrimSpace(req.Comentario)
	if comentarioLimpo == "" {
		jsonErr(w, http.StatusBadRequest, "campo 'comentario' é obrigatório e não pode ser vazio")
		return 0, "", false
	}

	return req.Versao, comentarioLimpo, true
}

// responderSolicitacaoFila escreve o mesmo formato de resposta 200 já
// usado por AssumirSolicitacaoHandler (Story 4.1) — reaproveitado por
// MarcarPendenciaHandler e ComentarSolicitacaoHandler, já que os 3
// devolvem a mesma projeção fila.Solicitacao. `administrador_id` vira JSON
// `null` quando vazio — só Comentar pode devolver isso (solicitante
// comentando numa solicitação `aberta` que nenhum administrador assumiu
// ainda, internal/fila.Comentar); Assumir/MarcarPendencia sempre devolvem
// um administrador_id preenchido, então o comportamento deles não muda.
func responderSolicitacaoFila(w http.ResponseWriter, resultado fila.Solicitacao) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":               resultado.ID,
		"tipo_solicitacao": resultado.TipoSolicitacao,
		"status":           resultado.Status,
		"versao":           resultado.Versao,
		"administrador_id": administradorIDOuNil(resultado.AdministradorID),
	})
}

// administradorIDOuNil converte a string-sentinela "" (sem administrador
// atribuído — só possível em Comentar, ver fila.Comentar) em `nil`, para o
// encoder JSON devolver `null` em vez de string vazia — delega a
// nullStringOuNil (fila.go), já que "" vazio equivale a um
// sql.NullString{Valid: false}.
func administradorIDOuNil(administradorID string) interface{} {
	return nullStringOuNil(sql.NullString{String: administradorID, Valid: administradorID != ""})
}

// tratarErroEscritaFila converte os 3 sentinelas de internal/fila
// (ErrNaoAutorizado, ErrConflitoVersao, ErrNaoEncontrada) no envelope HTTP
// correto — compartilhado por MarcarPendenciaHandler e
// ComentarSolicitacaoHandler. Devolve `true` quando já escreveu uma
// resposta de erro (o chamador deve retornar imediatamente); `false` quando
// `err` não é nenhum dos sentinelas conhecidos (o chamador trata como 500).
func tratarErroEscritaFila(w http.ResponseWriter, err error) bool {
	if errors.Is(err, fila.ErrNaoAutorizado) {
		jsonErr(w, http.StatusForbidden, "você não tem permissão para esta ação nesta solicitação")
		return true
	}
	var conflito *fila.ErrConflitoVersao
	if errors.As(err, &conflito) {
		jsonErrConflitoVersao(w, conflito.VersaoAtual)
		return true
	}
	if errors.Is(err, fila.ErrNaoEncontrada) {
		jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
		return true
	}
	return false
}

// MarcarPendenciaHandler — POST /api/solicitacoes/{id}/pendencia. Registrado
// em main.go atrás de RequireAuth(withPrivilegedDB(...), "administrador") —
// delega inteiramente a internal/fila.MarcarPendencia, nunca monta o UPDATE
// aqui (Boundaries "Always" da spec, mesmo princípio de
// AssumirSolicitacaoHandler).
func MarcarPendenciaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			// Formato de UUID inválido nunca poderia casar com uma linha
			// real — mesma resposta de "não encontrada" do padrão já
			// estabelecido em fila.go:126 (I/O Matrix da spec).
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		versao, comentario, ok := lerPendenciaOuComentarioRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)

		resultado, err := fila.MarcarPendencia(db, id, versao, administradorID, comentario)
		if err != nil {
			if tratarErroEscritaFila(w, err) {
				return
			}
			log.Printf("[Fila] Erro ao marcar pendência em %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Fila] %s marcou pendência em %s (versao=%d)", administradorID, resultado.ID, resultado.Versao)
		responderSolicitacaoFila(w, resultado)
	}
}

// ComentarSolicitacaoHandler — POST /api/solicitacoes/{id}/comentarios.
// Registrado em main.go atrás de RequireAuth(withPrivilegedDB(...), "") —
// qualquer perfil autenticado pode chamar a rota, mas só o solicitante dono
// da linha (`solicitante_id`) tem o comentário aceito (fila.ErrNaoAutorizado
// -> 403 para qualquer outro, Boundaries "Always" da spec) — delega
// inteiramente a internal/fila.Comentar.
func ComentarSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		versao, comentario, ok := lerPendenciaOuComentarioRequest(w, r)
		if !ok {
			return
		}

		solicitanteID := GetUserIDFromContext(r)

		resultado, err := fila.Comentar(db, id, versao, solicitanteID, comentario)
		if errors.Is(err, fila.ErrNaoAutorizado) && perfilAtende(GetPerfilFromContext(r)) {
			// atendente/administrador comentando no chamado de outra pessoa: não reabre nem muda o status
			resultado, err = fila.ComentarAtendimento(db, id, versao, solicitanteID, comentario)
		}
		if err != nil {
			if tratarErroEscritaFila(w, err) {
				return
			}
			log.Printf("[Fila] Erro ao comentar em %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Fila] %s comentou em %s (versao=%d, status=%s)", solicitanteID, resultado.ID, resultado.Versao, resultado.Status)
		responderSolicitacaoFila(w, resultado)
	}
}

// comentarioDetalhe é um item do histórico devolvido por
// ObterSolicitacaoHandler — projeção mínima de `solicitacao_comentarios`
// (sem `solicitacao_id`: já implícito no recurso pai).
type comentarioDetalhe struct {
	ID        string    `json:"id"`
	AutorID   string    `json:"autor_id"`
	AutorNome string    `json:"autor_nome"`
	Tipo      string    `json:"tipo"`
	Texto     string    `json:"texto"`
	CreatedAt time.Time `json:"created_at"`
}

// anexoDetalhe (20/10) é o metadado de um anexo em GET /api/solicitacoes/{id}
// — nunca o conteúdo (ver internal/anexos.AnexoListado/Recuperar). CreatedAt
// já formatado (RFC3339) para o mesmo padrão simples de outras respostas
// JSON deste handler.
type anexoDetalhe struct {
	ID           string `json:"id"`
	NomeArquivo  string `json:"nome_arquivo"`
	ContentType  string `json:"content_type"`
	TamanhoBytes int64  `json:"tamanho_bytes"`
	CreatedAt    string `json:"created_at"`
}

// ObterSolicitacaoHandler — GET /api/solicitacoes/{id}. Registrado em
// main.go atrás de RequireAuth(withDB(...), "") — só leitura (conexão
// GERAL, fb_apu05_app), qualquer perfil autenticado pode chamar a rota, mas
// só quem é `solicitante_id` OU `administrador_id` da linha recebe 200
// (403 para qualquer outro). SELECT direto de `solicitacoes` +
// `solicitacao_comentarios` (nenhuma escrita aqui — Boundaries "Always" da
// spec: escritas sempre por internal/fila).
func ObterSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		var tipoSolicitacao, status, solicitanteID string
		var administradorID sql.NullString
		var fornecedor, descricao, fornecedorCNPJ sql.NullString
		var fornecedorNaoCadastrado bool
		var quantidadeMaterial sql.NullInt64
		var versao int
		err := db.QueryRow(`
			SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id, fornecedor, descricao, fornecedor_cnpj, fornecedor_nao_cadastrado, quantidade_material
			FROM solicitacoes WHERE id = $1
		`, id).Scan(&tipoSolicitacao, &status, &versao, &solicitanteID, &administradorID, &fornecedor, &descricao, &fornecedorCNPJ, &fornecedorNaoCadastrado, &quantidadeMaterial)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if err != nil {
			log.Printf("[Fila] Erro ao buscar solicitação %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		chamadorID := GetUserIDFromContext(r)
		souAdministrador := administradorID.Valid && administradorID.String == chamadorID
		if chamadorID != solicitanteID && !souAdministrador && !perfilAtende(GetPerfilFromContext(r)) && !compartilhadoCom(db, id, chamadorID) {
			// Mesmo padrão de jsonErr(w, http.StatusForbidden, ...) de
			// handlers/solicitacoes.go:289 — terceiro sem relação com a
			// solicitação nunca vê nem o histórico.
			jsonErr(w, http.StatusForbidden, "você não tem permissão para ver esta solicitação")
			return
		}

		rows, err := db.Query(`
			SELECT c.id, c.autor_id, COALESCE(u.nome, ''), c.tipo, c.texto, c.created_at
			FROM solicitacao_comentarios c LEFT JOIN usuarios u ON u.id = c.autor_id
			WHERE c.solicitacao_id = $1
			ORDER BY c.created_at ASC, c.id ASC
		`, id)
		if err != nil {
			log.Printf("[Fila] Erro ao buscar comentários de %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		comentarios := make([]comentarioDetalhe, 0)
		for rows.Next() {
			var c comentarioDetalhe
			if err := rows.Scan(&c.ID, &c.AutorID, &c.AutorNome, &c.Tipo, &c.Texto, &c.CreatedAt); err != nil {
				log.Printf("[Fila] Erro ao ler comentário de %s: %v", id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			comentarios = append(comentarios, c)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Fila] Erro ao iterar comentários de %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		// Anexos (20/10): só metadado (nunca o conteúdo) — download é uma
		// rota própria (GET /api/solicitacoes/anexos/{anexoId}), mesma
		// checagem de permissão desta rota.
		anexosMeta, err := anexos.Listar(db, id)
		if err != nil {
			log.Printf("[Fila] Erro ao listar anexos de %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		anexosResposta := make([]anexoDetalhe, len(anexosMeta))
		for i, a := range anexosMeta {
			anexosResposta[i] = anexoDetalhe{
				ID:           a.ID,
				NomeArquivo:  a.NomeArquivo,
				ContentType:  a.ContentType,
				TamanhoBytes: a.TamanhoBytes,
				CreatedAt:    a.CreatedAt.Format(time.RFC3339),
			}
		}

		extras := detalheExtras(db, id)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"extras":                    extras,
			"id":                        id,
			"tipo_solicitacao":          tipoSolicitacao,
			"status":                    status,
			"versao":                    versao,
			"solicitante_id":            solicitanteID,
			"administrador_id":          nullStringOuNil(administradorID),
			"fornecedor":                nullStringOuNil(fornecedor),
			"fornecedor_cnpj":           nullStringOuNil(fornecedorCNPJ),
			"fornecedor_nao_cadastrado": fornecedorNaoCadastrado,
			"quantidade_material":       nullInt64OuNil(quantidadeMaterial),
			"descricao":                 nullStringOuNil(descricao),
			"anexos":                    anexosResposta,
			"comentarios":               comentarios,
		})
	}
}

// BaixarAnexoHandler — GET /api/solicitacoes/anexos/{anexoId} (20/10).
// Mesma checagem de permissão de ObterSolicitacaoHandler (solicitante dono
// OU administrador responsável pela solicitação-mãe) — quem não pode ver a
// solicitação também não pode baixar nada anexado a ela. Conteúdo via
// internal/anexos.Recuperar (AD-13) — nenhuma outra leitura de
// `solicitacao_anexos` aqui.
func BaixarAnexoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		anexoID := r.PathValue("anexoId")
		if !uuidFormatRegexp.MatchString(anexoID) {
			jsonErr(w, http.StatusNotFound, "anexo não encontrado")
			return
		}

		var solicitanteID string
		var administradorID sql.NullString
		err := db.QueryRow(`
			SELECT s.solicitante_id, s.administrador_id
			FROM solicitacao_anexos sa
			JOIN solicitacoes s ON s.id = sa.solicitacao_id
			WHERE sa.id = $1
		`, anexoID).Scan(&solicitanteID, &administradorID)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "anexo não encontrado")
			return
		}
		if err != nil {
			log.Printf("[Fila] Erro ao checar dono do anexo %s: %v", anexoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		chamadorID := GetUserIDFromContext(r)
		souAdministrador := administradorID.Valid && administradorID.String == chamadorID
		var solicitacaoDoAnexo string
		_ = db.QueryRow(`SELECT solicitacao_id FROM solicitacao_anexos WHERE id = $1`, anexoID).Scan(&solicitacaoDoAnexo)
		if chamadorID != solicitanteID && !souAdministrador && !perfilAtende(GetPerfilFromContext(r)) && !compartilhadoCom(db, solicitacaoDoAnexo, chamadorID) {
			jsonErr(w, http.StatusForbidden, "você não tem permissão para baixar este anexo")
			return
		}

		conteudo, meta, err := anexos.Recuperar(db, anexoID)
		if errors.Is(err, anexos.ErrAnexoNaoEncontrado) {
			jsonErr(w, http.StatusNotFound, "anexo não encontrado")
			return
		}
		if err != nil {
			log.Printf("[Fila] Erro ao recuperar anexo %s: %v", anexoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		contentType := meta.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.NomeArquivo))
		_, _ = io.Copy(w, conteudo)
	}
}

// detalheExtras reúne o que a página do chamado mostra além do histórico: quem abriu, centro de custo, como
// foi preenchido (lançamentos) e o aprovador. É complementar: se alguma consulta falhar, o detalhe básico
// continua válido (só é registrado no log).
func detalheExtras(db *sql.DB, id string) map[string]interface{} {
	out := map[string]interface{}{}
	var solNome, solEmail, admNome, ccCod, ccNome sql.NullString
	var criada sql.NullTime
	var snap []byte
	var numero int64
	if err := db.QueryRow(`
		SELECT u.nome, u.email, a.nome, cc.codigo, cc.nome, s.created_at, s.aprovador_snapshot, s.numero
		FROM solicitacoes s
		LEFT JOIN usuarios u ON u.id = s.solicitante_id
		LEFT JOIN usuarios a ON a.id = s.administrador_id
		LEFT JOIN centros_custo cc ON cc.id = s.centro_custo_id
		WHERE s.id = $1`, id).Scan(&solNome, &solEmail, &admNome, &ccCod, &ccNome, &criada, &snap, &numero); err != nil {
		log.Printf("[Fila] detalhe da solicitação %s: dados do cabeçalho indisponíveis: %v", id, err)
		return out
	}
	out["codigo"] = CodigoChamado(numero)
	out["solicitante_nome"] = nullStringOuNil(solNome)
	out["solicitante_email"] = nullStringOuNil(solEmail)
	out["atendente_nome"] = nullStringOuNil(admNome)
	out["centro_custo_codigo"] = nullStringOuNil(ccCod)
	out["centro_custo_nome"] = nullStringOuNil(ccNome)
	if criada.Valid {
		out["created_at"] = criada.Time
	}
	if len(snap) > 0 {
		var m map[string]interface{}
		if json.Unmarshal(snap, &m) == nil {
			out["aprovador"] = map[string]interface{}{"tipo": m["tipo"], "nome": m["nome"], "motivo": m["motivo"]}
		}
	}
	rows, err := db.Query(`
		SELECT l.lado, cc.codigo, cc.nome, COALESCE(c.codigo, ''), COALESCE(c.nome, ''), COALESCE(ci.nome, ''),
		       COALESCE(to_char(l.mes, 'YYYY-MM'), ''), l.valor::float8
		FROM solicitacao_lancamentos l
		JOIN centros_custo cc ON cc.id = l.centro_custo_id
		LEFT JOIN contas c ON c.id = l.conta_id
		LEFT JOIN classes_imobilizado ci ON ci.id = l.classe_imobilizado_id
		WHERE l.solicitacao_id = $1
		ORDER BY l.lado DESC, l.mes, l.created_at`, id)
	if err != nil {
		log.Printf("[Fila] detalhe da solicitação %s: lançamentos indisponíveis: %v", id, err)
		return out
	}
	defer rows.Close()
	lanc := make([]map[string]interface{}, 0)
	for rows.Next() {
		var lado, ccC, ccN, contaC, contaN, classe, mes string
		var valor float64
		if err := rows.Scan(&lado, &ccC, &ccN, &contaC, &contaN, &classe, &mes, &valor); err != nil {
			log.Printf("[Fila] detalhe da solicitação %s: lançamento ilegível: %v", id, err)
			break
		}
		lanc = append(lanc, map[string]interface{}{
			"lado": lado, "centro_custo": ccC + " — " + ccN, "conta": contaC, "conta_nome": contaN,
			"classe_imobilizado": classe, "mes": mes, "valor": valor,
		})
	}
	out["lancamentos"] = lanc

	// Obras: as linhas ficam em outra tabela (local, subgrupo, ordem, lado, valor) e a classificação no cabeçalho.
	var classificacao sql.NullString
	var anoOrcamento sql.NullInt64
	_ = db.QueryRow(`SELECT classificacao, ano_orcamento FROM solicitacoes WHERE id = $1`, id).Scan(&classificacao, &anoOrcamento)
	out["classificacao"] = nullStringOuNil(classificacao)
	if anoOrcamento.Valid {
		out["ano_orcamento"] = anoOrcamento.Int64
	}
	orows, err := db.Query(`
		SELECT COALESCE(ol.lado, ''), lo.nome, sg.nome, ol.ordem_investimento, ol.valor::float8
		FROM solicitacao_obras_linhas ol
		JOIN locais_obra lo ON lo.id = ol.local_obra_id
		JOIN subgrupos_despesa sg ON sg.id = ol.subgrupo_despesa_id
		WHERE ol.solicitacao_id = $1
		ORDER BY ol.lado, ol.created_at`, id)
	if err != nil {
		log.Printf("[Fila] detalhe da solicitação %s: linhas de obra indisponíveis: %v", id, err)
		return out
	}
	defer orows.Close()
	obras := make([]map[string]interface{}, 0)
	for orows.Next() {
		var lado, local, subgrupo, ordem string
		var valor float64
		if err := orows.Scan(&lado, &local, &subgrupo, &ordem, &valor); err != nil {
			log.Printf("[Fila] detalhe da solicitação %s: linha de obra ilegível: %v", id, err)
			break
		}
		obras = append(obras, map[string]interface{}{"lado": lado, "local": local, "subgrupo": subgrupo, "ordem": ordem, "valor": valor})
	}
	out["obras_linhas"] = obras
	return out
}
