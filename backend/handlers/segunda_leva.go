package handlers

// segunda_leva.go — segunda leva de ajustes da Controladoria (26/11/2026, fim do dia):
//   F3 cancelar chamado (a reserva de saldo sai junto, porque a reserva só conta chamados vivos);
//   F4 Tipo da Transferência (FL / Externo) só no DESTINO, filtrado pelas marcas da divisão; tela própria para
//      a Controladoria manter as marcas FL e Externo de cada divisão (com histórico).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------- F4: tipo do destino

// validarTipoTransferencia: "FL" ou "Externo" (obrigatório na Transferência).
func validarTipoTransferencia(v string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "fl":
		return "FL", ""
	case "externo", "externa":
		return "Externo", ""
	}
	return "", "Selecione o tipo da transferência (FL ou Externo)."
}

// conferirTipoDestino é trocável nos testes do handler.
var conferirTipoDestino = verificarTipoDestino

type consultaRowCtx interface {
	QueryRow(query string, args ...any) *sql.Row
}

// verificarTipoDestino: toda divisão de DESTINO precisa ter a marca do tipo escolhido. A origem não é validada.
func verificarTipoDestino(q consultaRowCtx, linhas []lancamentoValidado, tipo string) (string, error) {
	vistos := map[string]bool{}
	for _, l := range linhas {
		if l.Lado != "destino" || vistos[l.DivisaoID] {
			continue
		}
		vistos[l.DivisaoID] = true
		var codigo string
		var fl, externo bool
		if err := q.QueryRow(`SELECT codigo, faz_fl, faz_externo FROM divisoes WHERE id = $1`, l.DivisaoID).Scan(&codigo, &fl, &externo); err == sql.ErrNoRows {
			return "Divisão do destino não encontrada.", nil
		} else if err != nil {
			return "", err
		}
		if tipo == "FL" && !fl {
			return fmt.Sprintf("A divisão %s não faz FL. Com Tipo FL, escolha um destino de uma divisão que faz FL.", codigo), nil
		}
		if tipo == "Externo" && !externo {
			return fmt.Sprintf("A divisão %s não faz compra externa. Com Tipo Externo, escolha um destino de uma divisão que faz compra externa.", codigo), nil
		}
	}
	return "", nil
}

// divisaoOpcao: divisão para os formulários, com as marcas e o nome da filial (vem dos Centros/Plant do SAP).
type divisaoOpcao struct {
	ID         string `json:"id"`
	Codigo     string `json:"codigo"`
	Nome       string `json:"nome"`
	FazFL      bool   `json:"faz_fl"`
	FazExterno bool   `json:"faz_externo"`
}

// listarDivisoesComMarcas: o nome da divisão no cadastro costuma ser o próprio código; quando a filial (Centro do
// SAP) tem o mesmo código, mostra o nome e a cidade dela.
func listarDivisoesComMarcas(db *sql.DB) ([]divisaoOpcao, error) {
	rows, err := db.Query(`
		SELECT d.id, d.codigo,
		       CASE WHEN c.nome IS NOT NULL AND (d.nome = d.codigo OR d.nome = '') THEN c.nome || COALESCE(' — ' || initcap(lower(c.cidade)) || '/' || c.uf, '')
		            ELSE d.nome END,
		       d.faz_fl, d.faz_externo
		FROM divisoes d LEFT JOIN centros c ON c.codigo = d.codigo
		ORDER BY d.codigo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []divisaoOpcao{}
	for rows.Next() {
		var d divisaoOpcao
		if err := rows.Scan(&d.ID, &d.Codigo, &d.Nome, &d.FazFL, &d.FazExterno); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DivisoesMarcasHandler — GET /api/divisoes/marcas (atendimento): divisões com as marcas FL e Externo.
func DivisoesMarcasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ds, err := listarDivisoesComMarcas(db)
		if err != nil {
			log.Printf("[Divisoes] listar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": ds})
	}
}

// AtualizarDivisaoMarcasHandler — PUT /api/divisoes/{id}/marcas {faz_fl, faz_externo} (atendimento). Grava no
// histórico do cadastro (cadastro_historico, tipo "divisoes"), com quem e quando.
func AtualizarDivisaoMarcasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "Divisão não encontrada.")
			return
		}
		var req struct {
			FazFL      *bool `json:"faz_fl"`
			FazExterno *bool `json:"faz_externo"`
		}
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<12))
		if json.Unmarshal(corpo, &req) != nil || (req.FazFL == nil && req.FazExterno == nil) {
			jsonErr(w, http.StatusBadRequest, "Informe a marca FL e/ou Externo.")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		var codigo, nome string
		var fl, externo bool
		if err := tx.QueryRow(`SELECT codigo, nome, faz_fl, faz_externo FROM divisoes WHERE id = $1 FOR UPDATE`, id).Scan(&codigo, &nome, &fl, &externo); err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "Divisão não encontrada.")
			return
		} else if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if req.FazFL != nil {
			fl = *req.FazFL
		}
		if req.FazExterno != nil {
			externo = *req.FazExterno
		}
		if _, err := tx.Exec(`UPDATE divisoes SET faz_fl = $2, faz_externo = $3, updated_at = now() WHERE id = $1`, id, fl, externo); err != nil {
			log.Printf("[Divisoes] atualizar %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		dados, _ := json.Marshal(map[string]any{"codigo": codigo, "nome": nome, "faz_fl": fl, "faz_externo": externo})
		if _, err := tx.Exec(`INSERT INTO cadastro_historico (tipo_cadastro, registro_id, versao, acao, dados, ator_id)
			VALUES ('divisoes', $1, (SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = 'divisoes' AND registro_id = $1), 'atualizado', $2, $3)`,
			id, string(dados), GetUserIDFromContext(r)); err != nil {
			log.Printf("[Divisoes] histórico %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "codigo": codigo, "faz_fl": fl, "faz_externo": externo})
	}
}

// ---------------------------------------------------------------- F3: cancelar chamado

// CancelarChamadoHandler — POST /api/chamados/{id}/cancelar {versao, motivo}. Quem abriu cancela enquanto o
// chamado não foi assumido (aberta, pendente ou aguardando aprovação); o atendimento cancela qualquer chamado vivo.
// A reserva de saldo sai junto (só chamados vivos reservam). Conexão privilegiada (UPDATE de status).
func CancelarChamadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		var req struct {
			Versao int    `json:"versao"`
			Motivo string `json:"motivo"`
		}
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<14))
		if json.Unmarshal(corpo, &req) != nil || req.Versao < 1 {
			jsonErr(w, http.StatusBadRequest, "Os dados enviados estão incompletos. Atualize a página e tente de novo.")
			return
		}
		motivo := strings.TrimSpace(req.Motivo)
		if motivo == "" || utf8.RuneCountInString(motivo) > 1000 {
			jsonErr(w, http.StatusBadRequest, "Informe o motivo do cancelamento (até 1000 caracteres).")
			return
		}
		quem := GetUserIDFromContext(r)
		atende := perfilAtende(GetPerfilFromContext(r))
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		var status, dono string
		var versao int
		if err := tx.QueryRow(`SELECT status, solicitante_id, versao FROM solicitacoes WHERE id = $1 FOR UPDATE`, id).Scan(&status, &dono, &versao); err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		} else if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if versao != req.Versao {
			jsonErrConflitoVersao(w, versao)
			return
		}
		podeDono := dono == quem && (status == "aberta" || status == "pendente" || status == "aguardando_aprovacao")
		podeAtendimento := atende && (status == "aberta" || status == "em_atendimento" || status == "pendente" || status == "aguardando_aprovacao")
		if !podeDono && !podeAtendimento {
			if dono == quem && status == "em_atendimento" {
				jsonErr(w, http.StatusConflict, "O chamado já está em atendimento: peça o cancelamento por comentário à Controladoria.")
				return
			}
			jsonErr(w, http.StatusConflict, "Este chamado não pode ser cancelado (já foi encerrado ou você não tem permissão).")
			return
		}
		if _, err := tx.Exec(`UPDATE solicitacoes SET status = 'cancelada', versao = versao + 1 WHERE id = $1`, id); err != nil {
			log.Printf("[Cancelar] %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`,
			id, quem, "Cancelou o chamado. Motivo: "+motivo); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		log.Printf("[Cancelar] %s cancelou %s (status anterior %s)", quem, id, status)
		go avisarEvento(db, id, quem, "chamado cancelado", motivo)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "cancelada", "versao": versao + 1})
	}
}

// ---------------------------------------------------------------- F9 (terceira leva): fornecedor

// conferirNaoCadastrado é trocável nos testes do handler.
var conferirNaoCadastrado = verificarNaoCadastrado

// verificarNaoCadastrado: "Fornecedor não cadastrado" junto com um CNPJ que ESTÁ no cadastro de fornecedores é
// recusado (a tela trava a caixa depois de escolher um cadastrado; o servidor confere de novo).
func verificarNaoCadastrado(q consultaRowCtx, cnpj sql.NullString, naoCadastrado bool) (string, error) {
	if !naoCadastrado || !cnpj.Valid {
		return "", nil
	}
	var existe bool
	if err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM fornecedores WHERE regexp_replace(cgc, '[^0-9]', '', 'g') = $1)`, cnpj.String).Scan(&existe); err != nil {
		return "", err
	}
	if existe {
		return "Este CNPJ está no cadastro de fornecedores: escolha o fornecedor cadastrado ou use \"Trocar fornecedor\" antes de marcar \"Fornecedor não cadastrado\".", nil
	}
	return "", nil
}
