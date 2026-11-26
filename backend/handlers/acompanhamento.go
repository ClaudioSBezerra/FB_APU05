package handlers

// acompanhamento.go — resposta da Controladoria de 26/11/2026:
//   §2  nota interna: só atendimento/administração escrevem e veem; entra no histórico como "Nota adicionada";
//   §9  visualizadores (pessoas em cópia) e GRUPOS de pessoas (com gestor) que acompanham o chamado: veem dados,
//       anexos, andamento e comentários (nunca a nota interna), recebem os avisos e podem comentar;
//   avisos por e-mail ao solicitante e aos visualizadores nos eventos do chamado (assumido, pendência, finalizado,
//   reaberto, editado).
// Decisões da TI sobre as perguntas de §9.3: o membro precisa ter usuário no sistema (o aviso leva ao chamado e o
// acesso é conferido pelo login); o canal dos avisos é o e-mail; grupo não contém grupo; listas de distribuição do
// e-mail não são importadas (o cadastro é feito aqui).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"fb_apu05/services"

	"github.com/lib/pq"
)

// sqlAcompanha: condição SQL "o usuário (parâmetro p) acompanha o chamado s" — compartilhado direto com ele ou membro
// (ou gestor) de um grupo ATIVO incluído no chamado. O grupo é referência: vale quem está nele hoje.
func sqlAcompanha(p string) string {
	return `(EXISTS (SELECT 1 FROM solicitacao_compartilhamentos sc WHERE sc.solicitacao_id = s.id AND sc.usuario_id = ` + p + `)
	 OR EXISTS (SELECT 1 FROM solicitacao_grupos sg JOIN grupos g ON g.id = sg.grupo_id AND g.ativo
	            WHERE sg.solicitacao_id = s.id AND (g.gestor_id = ` + p + ` OR EXISTS (SELECT 1 FROM grupo_membros gm WHERE gm.grupo_id = g.id AND gm.usuario_id = ` + p + `))))`
}

// acompanha: o usuário acompanha o chamado (direto ou por grupo)?
func acompanha(db *sql.DB, solicitacaoID, usuarioID string) bool {
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM solicitacoes s WHERE s.id = $1 AND `+sqlAcompanha("$2")+`)`, solicitacaoID, usuarioID).Scan(&ok); err != nil {
		log.Printf("[Acompanhamento] erro ao checar acesso: %v", err)
		return false
	}
	return ok
}

// ---------------------------------------------------------------- avisos por e-mail

var enviarAvisoChamado = services.SendHTMLEmail

// avisarAcompanhantes é trocável nos testes (os de sqlmock não simulam o envio).
var avisarAcompanhantes = avisarAcompanhantesImpl

// avisarAcompanhantesImpl manda o aviso ao solicitante e a quem acompanha (pessoas e membros dos grupos), sem repetir
// e-mail e sem mandar para quem fez a ação. Roda em goroutine; falha só vai para o log.
func avisarAcompanhantesImpl(db *sql.DB, solicitacaoID, autorID, assunto, corpoHTML string) {
	rows, err := db.Query(`
		SELECT u.id, u.nome, u.email FROM solicitacoes s JOIN usuarios u ON u.id = s.solicitante_id WHERE s.id = $1
		UNION
		SELECT u.id, u.nome, u.email FROM solicitacao_compartilhamentos sc JOIN usuarios u ON u.id = sc.usuario_id WHERE sc.solicitacao_id = $1
		UNION
		SELECT u.id, u.nome, u.email FROM solicitacao_grupos sg JOIN grupos g ON g.id = sg.grupo_id AND g.ativo
		  JOIN grupo_membros gm ON gm.grupo_id = g.id JOIN usuarios u ON u.id = gm.usuario_id WHERE sg.solicitacao_id = $1
		UNION
		SELECT u.id, u.nome, u.email FROM solicitacao_grupos sg JOIN grupos g ON g.id = sg.grupo_id AND g.ativo
		  JOIN usuarios u ON u.id = g.gestor_id WHERE sg.solicitacao_id = $1`, solicitacaoID)
	if err != nil {
		log.Printf("[Avisos] destinatários de %s: %v", solicitacaoID, err)
		return
	}
	type dest struct{ nome, email string }
	vistos := map[string]dest{}
	for rows.Next() {
		var id, nome, email string
		if rows.Scan(&id, &nome, &email) == nil && id != autorID && strings.Contains(email, "@") {
			vistos[strings.ToLower(email)] = dest{nome, email}
		}
	}
	rows.Close()
	var numero int64
	_ = db.QueryRow(`SELECT numero FROM solicitacoes WHERE id = $1`, solicitacaoID).Scan(&numero)
	codigo := CodigoChamado(numero)
	link := strings.TrimRight(os.Getenv("APP_URL"), "/") + "/solicitacoes/" + solicitacaoID
	for _, d := range vistos {
		corpo := fmt.Sprintf(`<p>Olá, %s.</p>%s<p><a href="%s">Abrir o chamado %s</a></p><p style="color:#666;font-size:12px">Você recebe este aviso porque abriu ou acompanha este chamado no Sistema de Orçamento.</p>`,
			html.EscapeString(d.nome), corpoHTML, html.EscapeString(link), html.EscapeString(codigo))
		if err := enviarAvisoChamado([]string{d.email}, "Chamado "+codigo+": "+assunto, corpo); err != nil {
			log.Printf("[Avisos] e-mail não enviado para %s: %v", d.email, err)
		}
	}
}

// avisarEvento: aviso simples (assumido, pendência, finalizado, reaberto). `detalhe` é texto puro.
func avisarEvento(db *sql.DB, solicitacaoID, autorID, assunto, detalhe string) {
	var nome string
	_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, autorID).Scan(&nome)
	corpo := fmt.Sprintf(`<p><strong>%s</strong>%s.</p>`, html.EscapeString(assunto), map[bool]string{true: " por " + html.EscapeString(nome), false: ""}[nome != ""])
	if strings.TrimSpace(detalhe) != "" {
		corpo += `<blockquote style="border-left:3px solid #ccc;margin:0;padding-left:8px;white-space:pre-wrap">` + html.EscapeString(detalhe) + `</blockquote>`
	}
	avisarAcompanhantes(db, solicitacaoID, autorID, assunto, corpo)
}

// avisarInclusaoEmCopia (26/11, retorno da Wanessa): quem entra em cópia — direto ou por grupo — recebe o e-mail com
// o link do chamado também quando é incluído na ABERTURA (antes só recebia quem era incluído depois, no detalhe).
// Trocável nos testes.
var avisarInclusaoEmCopia = func(db *sql.DB, solicitacaoID, autorID string, usuarios, grupos []string) {
	if len(usuarios) == 0 && len(grupos) == 0 {
		return
	}
	var quemNome string
	var numero int64
	_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, autorID).Scan(&quemNome)
	_ = db.QueryRow(`SELECT numero FROM solicitacoes WHERE id = $1`, solicitacaoID).Scan(&numero)
	ids := unicos(usuarios)
	gs := unicos(grupos)
	rows, err := db.Query(`
		SELECT u.id, u.nome, u.email FROM usuarios u WHERE u.ativo AND u.id::text = ANY($1)
		UNION
		SELECT u.id, u.nome, u.email FROM grupos g JOIN grupo_membros gm ON gm.grupo_id = g.id JOIN usuarios u ON u.id = gm.usuario_id
		 WHERE g.ativo AND u.ativo AND g.id::text = ANY($2)
		UNION
		SELECT u.id, u.nome, u.email FROM grupos g JOIN usuarios u ON u.id = g.gestor_id WHERE g.ativo AND u.ativo AND g.id::text = ANY($2)`,
		pq.Array(ids), pq.Array(gs))
	if err != nil {
		log.Printf("[Avisos] destinatários da cópia de %s: %v", solicitacaoID, err)
		return
	}
	defer rows.Close()
	vistos := map[string]bool{}
	for rows.Next() {
		var id, nome, email string
		if rows.Scan(&id, &nome, &email) != nil || id == autorID || vistos[strings.ToLower(email)] || !strings.Contains(email, "@") {
			continue
		}
		vistos[strings.ToLower(email)] = true
		avisarCompartilhamento(email, nome, quemNome, CodigoChamado(numero), solicitacaoID)
	}
}

// ---------------------------------------------------------------- nota interna (§2)

// NotaInternaHandler — POST /api/chamados/{id}/notas (atendente/administrador). Não muda status nem versão.
func NotaInternaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		var req struct {
			Texto string `json:"texto"`
		}
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if json.Unmarshal(corpo, &req) != nil || strings.TrimSpace(req.Texto) == "" {
			jsonErr(w, http.StatusBadRequest, "Escreva a nota.")
			return
		}
		if utf8.RuneCountInString(req.Texto) > MaxDescricao {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("A nota passa do limite de %d caracteres.", MaxDescricao))
			return
		}
		res, err := db.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto)
			SELECT id, $2, 'nota_interna', $3 FROM solicitacoes WHERE id = $1`, id, GetUserIDFromContext(r), strings.TrimSpace(req.Texto))
		if err != nil {
			log.Printf("[Nota] %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// ---------------------------------------------------------------- visualizadores na abertura

// gravarVisualizadores (na MESMA transação da abertura): pessoas ativas e grupos ativos em cópia, com o evento no
// histórico. Devolve (mensagem 400, erro de servidor).
func gravarVisualizadores(tx *sql.Tx, solicitacaoID, autorID string, usuarios, grupos []string) (string, error) {
	if len(usuarios) > 30 || len(grupos) > 10 {
		return "Inclua no máximo 30 pessoas e 10 grupos em cópia.", nil
	}
	for _, u := range unicos(usuarios) {
		if !uuidFormatRegexp.MatchString(u) {
			return "Pessoa em cópia inválida.", nil
		}
		if u == autorID {
			continue
		}
		var nome string
		if err := tx.QueryRow(`SELECT nome FROM usuarios WHERE id = $1 AND ativo`, u).Scan(&nome); err == sql.ErrNoRows {
			return "Pessoa em cópia não encontrada ou inativa.", nil
		} else if err != nil {
			return "", err
		}
		if _, err := tx.Exec(`INSERT INTO solicitacao_compartilhamentos (solicitacao_id, usuario_id, compartilhado_por) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, solicitacaoID, u, autorID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, solicitacaoID, autorID, "Incluiu "+nome+" em cópia (visualizador)."); err != nil {
			return "", err
		}
	}
	for _, g := range unicos(grupos) {
		if !uuidFormatRegexp.MatchString(g) {
			return "Grupo em cópia inválido.", nil
		}
		var nome string
		if err := tx.QueryRow(`SELECT nome FROM grupos WHERE id = $1 AND ativo`, g).Scan(&nome); err == sql.ErrNoRows {
			return "Grupo em cópia não encontrado ou inativo.", nil
		} else if err != nil {
			return "", err
		}
		if _, err := tx.Exec(`INSERT INTO solicitacao_grupos (solicitacao_id, grupo_id, incluido_por) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, solicitacaoID, g, autorID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, solicitacaoID, autorID, "Incluiu o grupo "+nome+" em cópia."); err != nil {
			return "", err
		}
	}
	return "", nil
}

func unicos(v []string) []string {
	visto := map[string]bool{}
	out := make([]string, 0, len(v))
	for _, x := range v {
		x = strings.ToLower(strings.TrimSpace(x))
		if x != "" && !visto[x] {
			visto[x] = true
			out = append(out, x)
		}
	}
	return out
}

// ---------------------------------------------------------------- grupos no chamado (§9.1)

func listarGruposDoChamado(db *sql.DB, id string) ([]map[string]string, error) {
	rows, err := db.Query(`SELECT g.id, g.nome, g.ativo FROM solicitacao_grupos sg JOIN grupos g ON g.id = sg.grupo_id WHERE sg.solicitacao_id = $1 ORDER BY sg.created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var gid, nome string
		var ativo bool
		if err := rows.Scan(&gid, &nome, &ativo); err != nil {
			return nil, err
		}
		if !ativo {
			nome += " (inativo)"
		}
		out = append(out, map[string]string{"id": gid, "nome": nome})
	}
	return out, rows.Err()
}

// podeGerirVisualizadores: o dono do chamado ou o atendimento (Controladoria).
func podeGerirVisualizadores(db *sql.DB, r *http.Request, id string) (ok bool, existe bool, err error) {
	dono, _, existe, err := donoDoChamado(db, id)
	if err != nil || !existe {
		return false, existe, err
	}
	return dono == GetUserIDFromContext(r) || perfilAtende(GetPerfilFromContext(r)), true, nil
}

// IncluirGrupoNoChamadoHandler — POST /api/compartilhamentos/{id}/grupos {grupo_id}
func IncluirGrupoNoChamadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		var req struct {
			GrupoID string `json:"grupo_id"`
		}
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if json.Unmarshal(corpo, &req) != nil || !uuidFormatRegexp.MatchString(req.GrupoID) {
			jsonErr(w, http.StatusBadRequest, "Escolha o grupo.")
			return
		}
		pode, existe, err := podeGerirVisualizadores(db, r, id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !existe {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if !pode {
			jsonErr(w, http.StatusForbidden, "Só quem abriu o chamado ou o atendimento inclui visualizadores.")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		if msg, err := gravarVisualizadores(tx, id, GetUserIDFromContext(r), nil, []string{req.GrupoID}); err != nil {
			log.Printf("[Acompanhamento] grupo no chamado: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		} else if msg != "" {
			jsonErr(w, http.StatusBadRequest, msg)
			return
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		go avisarInclusaoEmCopia(db, id, GetUserIDFromContext(r), nil, []string{req.GrupoID})
		responderVisualizadores(w, db, id)
	}
}

// RemoverGrupoDoChamadoHandler — DELETE /api/compartilhamentos/{id}/grupos/{grupoId}
func RemoverGrupoDoChamadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, gid := r.PathValue("id"), r.PathValue("grupoId")
		if !uuidFormatRegexp.MatchString(id) || !uuidFormatRegexp.MatchString(gid) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		pode, existe, err := podeGerirVisualizadores(db, r, id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !existe {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if !pode {
			jsonErr(w, http.StatusForbidden, "Só quem abriu o chamado ou o atendimento retira visualizadores.")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		var nome string
		_ = tx.QueryRow(`SELECT nome FROM grupos WHERE id = $1`, gid).Scan(&nome)
		res, err := tx.Exec(`DELETE FROM solicitacao_grupos WHERE solicitacao_id = $1 AND grupo_id = $2`, id, gid)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, id, GetUserIDFromContext(r), "Retirou o grupo "+nome+" da cópia."); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		responderVisualizadores(w, db, id)
	}
}

func responderVisualizadores(w http.ResponseWriter, db *sql.DB, id string) {
	pessoas, _ := listarCompartilhamentos(db, id)
	grupos, _ := listarGruposDoChamado(db, id)
	if pessoas == nil {
		pessoas = []map[string]string{}
	}
	if grupos == nil {
		grupos = []map[string]string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": pessoas, "grupos": grupos})
}

// ---------------------------------------------------------------- cadastro de grupos (§9.2)

type grupoReq struct {
	Nome      string   `json:"nome"`
	Descricao string   `json:"descricao"`
	GestorID  string   `json:"gestor_id"`
	Membros   []string `json:"membros"`
	Ativo     *bool    `json:"ativo"`
}

type pessoaRef struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Email string `json:"email"`
}

type grupoResp struct {
	ID        string      `json:"id"`
	Nome      string      `json:"nome"`
	Descricao string      `json:"descricao"`
	Gestor    pessoaRef   `json:"gestor"`
	Ativo     bool        `json:"ativo"`
	Membros   []pessoaRef `json:"membros"`
}

func carregarGrupos(db *sql.DB, filtro string, args ...any) ([]grupoResp, error) {
	rows, err := db.Query(`SELECT g.id, g.nome, COALESCE(g.descricao, ''), g.ativo, u.id, u.nome, u.email
		FROM grupos g JOIN usuarios u ON u.id = g.gestor_id `+filtro+` ORDER BY lower(g.nome)`, args...)
	if err != nil {
		return nil, err
	}
	out := []grupoResp{}
	idx := map[string]int{}
	for rows.Next() {
		var g grupoResp
		if err := rows.Scan(&g.ID, &g.Nome, &g.Descricao, &g.Ativo, &g.Gestor.ID, &g.Gestor.Nome, &g.Gestor.Email); err != nil {
			rows.Close()
			return nil, err
		}
		g.Membros = []pessoaRef{}
		idx[g.ID] = len(out)
		out = append(out, g)
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	mrows, err := db.Query(`SELECT gm.grupo_id, u.id, u.nome, u.email FROM grupo_membros gm JOIN usuarios u ON u.id = gm.usuario_id ORDER BY lower(u.nome)`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var gid string
		var p pessoaRef
		if err := mrows.Scan(&gid, &p.ID, &p.Nome, &p.Email); err != nil {
			return nil, err
		}
		if i, ok := idx[gid]; ok {
			out[i].Membros = append(out[i].Membros, p)
		}
	}
	return out, mrows.Err()
}

// GruposOpcoesHandler — GET /api/grupos/opcoes: grupos ATIVOS (id, nome, descrição) para escolher no formulário.
func GruposOpcoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, nome, COALESCE(descricao, '') FROM grupos WHERE ativo ORDER BY lower(nome)`)
		if err != nil {
			log.Printf("[Grupos] opções: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var id, nome, desc string
			if rows.Scan(&id, &nome, &desc) == nil {
				out = append(out, map[string]string{"id": id, "nome": nome, "descricao": desc})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}
}

// ListarGruposHandler — GET /api/grupos: o atendimento vê todos; os demais, só os grupos de que são gestores ("Meus grupos").
func ListarGruposHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var gs []grupoResp
		var err error
		if perfilAtende(GetPerfilFromContext(r)) {
			gs, err = carregarGrupos(db, "")
		} else {
			gs, err = carregarGrupos(db, "WHERE g.gestor_id = $1", GetUserIDFromContext(r))
		}
		if err != nil {
			log.Printf("[Grupos] listar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": gs, "pode_criar": perfilAtende(GetPerfilFromContext(r))})
	}
}

func nomeUsuarioAtivo(q interface {
	QueryRow(string, ...any) *sql.Row
}, id string) (string, bool) {
	var nome string
	if !uuidFormatRegexp.MatchString(id) || q.QueryRow(`SELECT nome FROM usuarios WHERE id = $1 AND ativo`, id).Scan(&nome) != nil {
		return "", false
	}
	return nome, true
}

// CriarGrupoHandler — POST /api/grupos (atendimento): a Controladoria cria e SEMPRE define o gestor.
func CriarGrupoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req grupoReq
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if json.Unmarshal(corpo, &req) != nil {
			jsonErr(w, http.StatusBadRequest, "Dados do grupo inválidos.")
			return
		}
		nome := strings.TrimSpace(req.Nome)
		if nome == "" || utf8.RuneCountInString(nome) > 120 {
			jsonErr(w, http.StatusBadRequest, "Informe o nome do grupo (até 120 caracteres).")
			return
		}
		if utf8.RuneCountInString(req.Descricao) > 500 {
			jsonErr(w, http.StatusBadRequest, "A descrição do grupo passa de 500 caracteres.")
			return
		}
		gestorNome, ok := nomeUsuarioAtivo(db, req.GestorID)
		if !ok {
			jsonErr(w, http.StatusBadRequest, "Grupo sem gestor não pode ser salvo. Escolha o gestor da área (usuário ativo).")
			return
		}
		autor := GetUserIDFromContext(r)
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		ativo := req.Ativo == nil || *req.Ativo
		var gid string
		if err := tx.QueryRow(`INSERT INTO grupos (nome, descricao, gestor_id, ativo, criado_por) VALUES ($1, NULLIF($2, ''), $3, $4, $5) RETURNING id`,
			nome, strings.TrimSpace(req.Descricao), req.GestorID, ativo, autor).Scan(&gid); err != nil {
			if strings.Contains(err.Error(), "uq_grupos_nome") {
				jsonErr(w, http.StatusConflict, "Já existe um grupo com esse nome.")
				return
			}
			log.Printf("[Grupos] criar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		hist := []string{fmt.Sprintf("Criou o grupo, com %s como gestor.", gestorNome)}
		for _, m := range unicos(req.Membros) {
			n, ok := nomeUsuarioAtivo(tx, m)
			if !ok {
				jsonErr(w, http.StatusBadRequest, "Membro não encontrado ou inativo.")
				return
			}
			if _, err := tx.Exec(`INSERT INTO grupo_membros (grupo_id, usuario_id, incluido_por) VALUES ($1, $2, $3)`, gid, m, autor); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			hist = append(hist, "Incluiu "+n+".")
		}
		for _, h := range hist {
			if _, err := tx.Exec(`INSERT INTO grupo_historico (grupo_id, autor_id, texto) VALUES ($1, $2, $3)`, gid, autor, h); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		gs, _ := carregarGrupos(db, "WHERE g.id = $1", gid)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(gs)
	}
}

// AtualizarGrupoHandler — PUT /api/grupos/{id}: o atendimento muda tudo (inclusive o gestor); o GESTOR do grupo muda
// só os membros. Cada mudança vai para o histórico do grupo.
func AtualizarGrupoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gid := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(gid) {
			jsonErr(w, http.StatusNotFound, "Grupo não encontrado.")
			return
		}
		var req grupoReq
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if json.Unmarshal(corpo, &req) != nil {
			jsonErr(w, http.StatusBadRequest, "Dados do grupo inválidos.")
			return
		}
		autor := GetUserIDFromContext(r)
		controladoria := perfilAtende(GetPerfilFromContext(r))
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		var nome, desc, gestorID string
		var ativo bool
		if err := tx.QueryRow(`SELECT nome, COALESCE(descricao, ''), gestor_id, ativo FROM grupos WHERE id = $1 FOR UPDATE`, gid).Scan(&nome, &desc, &gestorID, &ativo); err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "Grupo não encontrado.")
			return
		} else if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !controladoria && gestorID != autor {
			jsonErr(w, http.StatusForbidden, "Só o gestor do grupo ou a Controladoria editam o grupo.")
			return
		}
		var hist []string
		if controladoria {
			novoNome := strings.TrimSpace(req.Nome)
			if novoNome == "" || utf8.RuneCountInString(novoNome) > 120 {
				jsonErr(w, http.StatusBadRequest, "Informe o nome do grupo (até 120 caracteres).")
				return
			}
			novoGestorNome, ok := nomeUsuarioAtivo(tx, req.GestorID)
			if !ok {
				jsonErr(w, http.StatusBadRequest, "Grupo sem gestor não pode ser salvo. Escolha o gestor da área (usuário ativo).")
				return
			}
			novoAtivo := req.Ativo == nil || *req.Ativo
			if novoNome != nome {
				hist = append(hist, fmt.Sprintf("Renomeou o grupo de %q para %q.", nome, novoNome))
			}
			if strings.TrimSpace(req.Descricao) != desc {
				hist = append(hist, "Alterou a descrição.")
			}
			if !strings.EqualFold(req.GestorID, gestorID) {
				antigo, _ := nomeUsuarioAtivo(tx, gestorID)
				hist = append(hist, fmt.Sprintf("Trocou o gestor de %s para %s.", antigo, novoGestorNome))
			}
			if novoAtivo != ativo {
				hist = append(hist, map[bool]string{true: "Reativou o grupo.", false: "Inativou o grupo."}[novoAtivo])
			}
			if _, err := tx.Exec(`UPDATE grupos SET nome = $2, descricao = NULLIF($3, ''), gestor_id = $4, ativo = $5, updated_at = now() WHERE id = $1`,
				gid, novoNome, strings.TrimSpace(req.Descricao), req.GestorID, novoAtivo); err != nil {
				if strings.Contains(err.Error(), "uq_grupos_nome") {
					jsonErr(w, http.StatusConflict, "Já existe um grupo com esse nome.")
					return
				}
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		// membros: diferença entre o que está gravado e o que veio
		atuais := map[string]bool{}
		rows, err := tx.Query(`SELECT usuario_id FROM grupo_membros WHERE grupo_id = $1`, gid)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		for rows.Next() {
			var u string
			if rows.Scan(&u) == nil {
				atuais[strings.ToLower(u)] = true
			}
		}
		rows.Close()
		novos := map[string]bool{}
		for _, m := range unicos(req.Membros) {
			novos[m] = true
		}
		incluir, retirar := []string{}, []string{}
		for m := range novos {
			if !atuais[m] {
				incluir = append(incluir, m)
			}
		}
		for m := range atuais {
			if !novos[m] {
				retirar = append(retirar, m)
			}
		}
		sort.Strings(incluir)
		sort.Strings(retirar)
		for _, m := range incluir {
			n, ok := nomeUsuarioAtivo(tx, m)
			if !ok {
				jsonErr(w, http.StatusBadRequest, "Membro não encontrado ou inativo.")
				return
			}
			if _, err := tx.Exec(`INSERT INTO grupo_membros (grupo_id, usuario_id, incluido_por) VALUES ($1, $2, $3)`, gid, m, autor); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			hist = append(hist, "Incluiu "+n+".")
		}
		for _, m := range retirar {
			var n string
			_ = tx.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, m).Scan(&n)
			if _, err := tx.Exec(`DELETE FROM grupo_membros WHERE grupo_id = $1 AND usuario_id = $2`, gid, m); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			hist = append(hist, "Removeu "+n+".")
		}
		for _, h := range hist {
			if _, err := tx.Exec(`INSERT INTO grupo_historico (grupo_id, autor_id, texto) VALUES ($1, $2, $3)`, gid, autor, h); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		gs, _ := carregarGrupos(db, "WHERE g.id = $1", gid)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"grupo": gs, "mudancas": len(hist)})
	}
}

// HistoricoGrupoHandler — GET /api/grupos/{id}/historico (atendimento ou gestor do grupo).
func HistoricoGrupoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gid := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(gid) {
			jsonErr(w, http.StatusNotFound, "Grupo não encontrado.")
			return
		}
		var gestorID string
		if err := db.QueryRow(`SELECT gestor_id FROM grupos WHERE id = $1`, gid).Scan(&gestorID); err != nil {
			jsonErr(w, http.StatusNotFound, "Grupo não encontrado.")
			return
		}
		if !perfilAtende(GetPerfilFromContext(r)) && gestorID != GetUserIDFromContext(r) {
			jsonErr(w, http.StatusForbidden, "Sem permissão para ver este grupo.")
			return
		}
		rows, err := db.Query(`SELECT h.texto, COALESCE(u.nome, ''), h.created_at FROM grupo_historico h LEFT JOIN usuarios u ON u.id = h.autor_id WHERE h.grupo_id = $1 ORDER BY h.created_at DESC, h.id`, gid)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var texto, autor string
			var quando sql.NullTime
			if rows.Scan(&texto, &autor, &quando) == nil {
				out = append(out, map[string]any{"texto": texto, "autor": autor, "quando": quando.Time})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}
}
