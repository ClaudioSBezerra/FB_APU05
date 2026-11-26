package handlers

// fila_andamento.go — F14 da terceira leva (26/11/2026):
//   - "Em andamento": todos os chamados em atendimento e pendentes, de TODOS os atendentes, com quem atende, desde
//     quando, prioridade (alta = reaberto por comentário) e o destaque "respondido pelo solicitante" (F12);
//   - atribuir em lote: vários chamados abertos para mim ou para outra pessoa do atendimento; reatribuir chamado já
//     em atendimento/pendente com motivo no histórico ("De X para Y"). O resultado diz item a item o que deu certo.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu05/internal/sla"
)

// FilaAndamentoHandler — GET /api/fila/andamento (atendente/administrador).
func FilaAndamentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendario, err := sla.NovoCalendario(db)
		if err != nil {
			log.Printf("[Fila] calendário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		rows, err := db.Query(`
			SELECT s.id, s.numero, s.tipo_solicitacao, s.status, s.versao, s.created_at, s.prioridade, s.respondido,
			       cc.codigo, cc.nome, COALESCE(a.id::text, ''), COALESCE(a.nome, ''),
			       (SELECT MAX(c.created_at) FROM solicitacao_comentarios c WHERE c.solicitacao_id = s.id AND c.tipo = 'evento'
			          AND (c.texto LIKE 'Assumiu%' OR c.texto LIKE 'Atribuído%' OR c.texto LIKE 'Reatribuído%')),
			       (SELECT lo.nome FROM solicitacao_obras_linhas ol JOIN locais_obra lo ON lo.id = ol.local_obra_id WHERE ol.solicitacao_id = s.id ORDER BY ol.created_at LIMIT 1)
			FROM solicitacoes s
			LEFT JOIN centros_custo cc ON cc.id = s.centro_custo_id
			LEFT JOIN usuarios a ON a.id = s.administrador_id
			WHERE s.status IN ('em_atendimento', 'pendente')
			ORDER BY (s.prioridade = 'alta') DESC, s.respondido DESC, s.created_at ASC
			LIMIT 500`)
		if err != nil {
			log.Printf("[Fila] andamento: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, tipo, status, prioridade, atID, atNome string
			var numero int64
			var versao int
			var criado time.Time
			var respondido bool
			var ccCod, ccNome, local sql.NullString
			var assumido sql.NullTime
			if err := rows.Scan(&id, &numero, &tipo, &status, &versao, &criado, &prioridade, &respondido, &ccCod, &ccNome, &atID, &atNome, &assumido, &local); err != nil {
				log.Printf("[Fila] andamento (linha): %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			it := map[string]any{
				"id": id, "codigo": CodigoChamado(numero), "tipo_solicitacao": tipo, "status": status, "versao": versao,
				"created_at": criado, "prioridade": prioridade, "respondido": respondido,
				"centro_custo_codigo": nullStringOuNil(ccCod), "centro_custo_nome": nullStringOuNil(ccNome), "local_obra": nullStringOuNil(local),
				"atendente_id": atID, "atendente_nome": atNome,
				"prazo_limite": calendario.PrazoLimite(criado), "atrasada": calendario.EstaAtrasado(criado),
			}
			if assumido.Valid {
				it["assumido_em"] = assumido.Time
			}
			items = append(items, it)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}

// AtendentesHandler — GET /api/fila/atendentes: quem pode receber chamados (atendente e administrador ativos).
func AtendentesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`SELECT id, nome FROM usuarios WHERE ativo AND perfil IN ('atendente', 'administrador') ORDER BY nome`)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var id, nome string
			if rows.Scan(&id, &nome) == nil {
				out = append(out, map[string]string{"id": id, "nome": nome})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}
}

type itemAtribuir struct {
	ID     string `json:"id"`
	Versao int    `json:"versao"`
}

// AtribuirHandler — POST /api/fila/atribuir {itens:[{id, versao}], atendente_id, motivo} (atendimento; conexão
// privilegiada). Aberto → passa a em atendimento com a pessoa escolhida; em atendimento/pendente → reatribuído
// (motivo obrigatório). Cada item tem a sua transação; o resultado lista ok e falha com o motivo.
func AtribuirHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Itens       []itemAtribuir `json:"itens"`
			AtendenteID string         `json:"atendente_id"`
			Motivo      string         `json:"motivo"`
		}
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if json.Unmarshal(corpo, &req) != nil || len(req.Itens) == 0 || len(req.Itens) > 100 {
			jsonErr(w, http.StatusBadRequest, "Escolha de 1 a 100 chamados.")
			return
		}
		quem := GetUserIDFromContext(r)
		destino := strings.TrimSpace(req.AtendenteID)
		if destino == "" {
			destino = quem
		}
		var destNome, destEmail, destPerfil string
		var ativo bool
		if !uuidFormatRegexp.MatchString(destino) || db.QueryRow(`SELECT nome, email, perfil, ativo FROM usuarios WHERE id = $1`, destino).Scan(&destNome, &destEmail, &destPerfil, &ativo) != nil ||
			!ativo || !perfilAtende(destPerfil) {
			jsonErr(w, http.StatusBadRequest, "Escolha uma pessoa do atendimento (atendente ou administrador ativo).")
			return
		}
		var quemNome string
		_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, quem).Scan(&quemNome)
		motivo := strings.TrimSpace(req.Motivo)

		type resultado struct {
			ID     string `json:"id"`
			Codigo string `json:"codigo"`
			OK     bool   `json:"ok"`
			Motivo string `json:"motivo,omitempty"`
		}
		var out []resultado
		var codigosOK []string
		for _, it := range req.Itens {
			res := resultado{ID: it.ID}
			msg, codigo, err := atribuirUm(db, it, destino, destNome, quem, quemNome, motivo)
			res.Codigo = codigo
			if err != nil {
				log.Printf("[Fila] atribuir %s: %v", it.ID, err)
				msg = "erro no servidor"
			}
			if msg == "" {
				res.OK = true
				codigosOK = append(codigosOK, codigo)
			} else {
				res.Motivo = msg
			}
			out = append(out, res)
		}
		if len(codigosOK) > 0 && destino != quem {
			go func() {
				corpo := fmt.Sprintf(`<p>Olá, %s.</p><p>%s atribuiu a você %d chamado(s) no Sistema de Orçamento: <strong>%s</strong>.</p><p>Eles estão em "Comigo" na Fila de atendimento.</p>`,
					html.EscapeString(destNome), html.EscapeString(quemNome), len(codigosOK), html.EscapeString(strings.Join(codigosOK, ", ")))
				if err := enviarAvisoChamado([]string{destEmail}, "Chamados atribuídos a você: "+strings.Join(codigosOK, ", "), corpo); err != nil {
					log.Printf("[Fila] aviso de atribuição para %s: %v", destEmail, err)
				}
			}()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"resultados": out})
	}
}

func atribuirUm(db *sql.DB, it itemAtribuir, destino, destNome, quem, quemNome, motivo string) (string, string, error) {
	if !uuidFormatRegexp.MatchString(it.ID) {
		return "chamado inválido", "", nil
	}
	tx, err := db.Begin()
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	var versao int
	var numero int64
	var atual sql.NullString
	if err := tx.QueryRow(`SELECT status, versao, numero, administrador_id FROM solicitacoes WHERE id = $1 FOR UPDATE`, it.ID).Scan(&status, &versao, &numero, &atual); err == sql.ErrNoRows {
		return "chamado não encontrado", "", nil
	} else if err != nil {
		return "", "", err
	}
	codigo := CodigoChamado(numero)
	if versao != it.Versao {
		return "o chamado mudou desde que a lista foi aberta (atualize e tente de novo)", codigo, nil
	}
	var texto string
	switch status {
	case "aberta":
		if _, err := tx.Exec(`UPDATE solicitacoes SET status = 'em_atendimento', administrador_id = $2, versao = versao + 1 WHERE id = $1`, it.ID, destino); err != nil {
			return "", codigo, err
		}
		texto = "Assumiu o atendimento."
		if destino != quem {
			texto = fmt.Sprintf("Atribuído a %s por %s.", destNome, quemNome)
		}
	case "em_atendimento", "pendente":
		if atual.Valid && atual.String == destino {
			return "já está com " + destNome, codigo, nil
		}
		if motivo == "" {
			return "informe o motivo da reatribuição", codigo, nil
		}
		var deNome string
		if atual.Valid {
			_ = tx.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, atual.String).Scan(&deNome)
		}
		if _, err := tx.Exec(`UPDATE solicitacoes SET administrador_id = $2, versao = versao + 1 WHERE id = $1`, it.ID, destino); err != nil {
			return "", codigo, err
		}
		texto = fmt.Sprintf("Reatribuído de %s para %s por %s. Motivo: %s", textoOuTraco(deNome), destNome, quemNome, motivo)
	default:
		return "o chamado não está aberto nem em atendimento", codigo, nil
	}
	if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, it.ID, quem, texto); err != nil {
		return "", codigo, err
	}
	return "", codigo, tx.Commit()
}
