package handlers

// comentario_anexos.go — F12 da terceira leva (26/11/2026): anexos em comentários e em notas internas.
//   - comentário público (solicitante, quem está em cópia, atendimento): um ou mais anexos, mesmos tipos e checagem
//     de conteúdo do chamado; pode ir só com anexo, sem texto;
//   - nota interna COM anexo só em chamado aberto, em atendimento ou pendente (só texto vale em qualquer status);
//   - anexo de nota interna nunca aparece para o solicitante nem para quem está em cópia (lista e download);
//   - o anexo aparece no próprio comentário e o histórico registra "Anexo adicionado".

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu05/internal/anexos"
)

type consultaQuery interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// anexosDosComentarios: anexo por comentário e o conjunto dos anexos que são de algum comentário.
func anexosDosComentarios(q consultaQuery, solicitacaoID string) (map[string][]anexoDetalhe, map[string]bool) {
	por := map[string][]anexoDetalhe{}
	de := map[string]bool{}
	rows, err := q.Query(`SELECT ca.comentario_id, a.id, a.nome_arquivo, a.content_type, a.tamanho_bytes, a.created_at
		FROM comentario_anexos ca JOIN solicitacao_anexos a ON a.id = ca.anexo_id
		WHERE a.solicitacao_id = $1 ORDER BY a.created_at`, solicitacaoID)
	if err != nil {
		return por, de
	}
	defer rows.Close()
	for rows.Next() {
		var cid string
		var a anexoDetalhe
		var criado time.Time
		if rows.Scan(&cid, &a.ID, &a.NomeArquivo, &a.ContentType, &a.TamanhoBytes, &criado) == nil {
			a.CreatedAt = criado.Format(time.RFC3339)
			por[cid] = append(por[cid], a)
			de[a.ID] = true
		}
	}
	return por, de
}

// anexoInterno: o anexo é de nota interna? (erro de leitura = não)
func anexoInterno(q consultaRowCtx, anexoID string) bool {
	var interno bool
	if q.QueryRow(`SELECT interno FROM solicitacao_anexos WHERE id = $1`, anexoID).Scan(&interno) != nil {
		return false
	}
	return interno
}

type comentarioComAnexosRequest struct {
	Versao     int            `json:"versao"`
	Comentario string         `json:"comentario"`
	Texto      string         `json:"texto"` // nota interna
	Anexos     []anexoRequest `json:"anexos"`
}

// lerComentarioComAnexos: versão (quando exigida), texto e anexos já validados pelo conteúdo. Texto ou anexo.
func lerComentarioComAnexos(w http.ResponseWriter, r *http.Request, exigeVersao bool) (comentarioComAnexosRequest, []anexos.ArquivoDecodificado, bool) {
	var req comentarioComAnexosRequest
	corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 15<<20))
	if err != nil || json.Unmarshal(corpo, &req) != nil {
		jsonErr(w, http.StatusBadRequest, "Os dados enviados estão incompletos ou em formato inválido. Atualize a página e tente de novo.")
		return req, nil, false
	}
	if exigeVersao && req.Versao < 1 {
		jsonErr(w, http.StatusBadRequest, "Atualize a página e tente de novo.")
		return req, nil, false
	}
	if req.Texto != "" && req.Comentario == "" {
		req.Comentario = req.Texto
	}
	req.Comentario = strings.TrimSpace(req.Comentario)
	if len(req.Anexos) > 10 {
		jsonErr(w, http.StatusBadRequest, "Envie no máximo 10 anexos por comentário.")
		return req, nil, false
	}
	arqs, msg := validarAnexos(req.Anexos, false)
	if msg != "" {
		jsonErr(w, http.StatusBadRequest, msg)
		return req, nil, false
	}
	if req.Comentario == "" && len(arqs) == 0 {
		jsonErr(w, http.StatusBadRequest, "Escreva o comentário ou anexe um arquivo.")
		return req, nil, false
	}
	if len([]rune(req.Comentario)) > MaxDescricao {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("O comentário passa do limite de %d caracteres.", MaxDescricao))
		return req, nil, false
	}
	if req.Comentario == "" {
		req.Comentario = "(anexo)"
	}
	return req, arqs, true
}

// gravarAnexosDoComentario grava os anexos ligados ao comentário e o evento no histórico, numa transação.
func gravarAnexosDoComentario(db *sql.DB, solicitacaoID, comentarioID, autorID string, arqs []anexos.ArquivoDecodificado, interno bool) error {
	if len(arqs) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	nomes := make([]string, 0, len(arqs))
	for _, a := range arqs {
		ref, err := anexos.Salvar(tx, solicitacaoID, a.NomeArquivo, a.ContentType, a.Dados)
		if err != nil {
			return err
		}
		if interno {
			if _, err := tx.Exec(`UPDATE solicitacao_anexos SET interno = true WHERE id = $1`, ref.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO comentario_anexos (comentario_id, anexo_id) VALUES ($1, $2)`, comentarioID, ref.ID); err != nil {
			return err
		}
		nomes = append(nomes, a.NomeArquivo)
	}
	tipo := "evento"
	if interno {
		tipo = "nota_interna" // o registro do anexo interno também fica fora da vista do solicitante
	}
	if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, $3, $4)`,
		solicitacaoID, autorID, tipo, "Anexo adicionado: "+strings.Join(nomes, ", ")); err != nil {
		return err
	}
	return tx.Commit()
}

// ultimoComentarioDe: o comentário que acabou de ser gravado por esta pessoa (para ligar os anexos).
func ultimoComentarioDe(q consultaRowCtx, solicitacaoID, autorID, tipo string) (string, error) {
	var id string
	err := q.QueryRow(`SELECT id FROM solicitacao_comentarios WHERE solicitacao_id = $1 AND autor_id = $2 AND tipo = $3 ORDER BY created_at DESC, id DESC LIMIT 1`,
		solicitacaoID, autorID, tipo).Scan(&id)
	return id, err
}

// salvarAnexosComentarioPublico: chamada depois que o comentário foi gravado.
func salvarAnexosComentarioPublico(db *sql.DB, solicitacaoID, autorID string, arqs []anexos.ArquivoDecodificado) error {
	if len(arqs) == 0 {
		return nil
	}
	cid, err := ultimoComentarioDe(db, solicitacaoID, autorID, "comentario")
	if err == nil {
		err = gravarAnexosDoComentario(db, solicitacaoID, cid, autorID, arqs, false)
	}
	if err != nil {
		log.Printf("[Comentario] anexos do comentário em %s não gravados: %v", solicitacaoID, err)
	}
	return err
}
