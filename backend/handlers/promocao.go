package handlers

// promocao.go — "Solicitar promoção" (17/11/2026).
//
// Todo usuário novo entra como "solicitante". Para virar "atendente" ele pede em Conta > Solicitar
// promoção; todos os administradores ATIVOS recebem um e-mail e quem decide é um administrador, em
// Gestão de Usuários (a promoção nunca é automática). Rotas (qualquer usuário autenticado):
//
//	GET  /api/me/promocao             — situação: pode pedir? quando foi o último pedido?
//	POST /api/me/solicitar-promocao   — registra o pedido e avisa os administradores por e-mail
//
// Trava anti-spam: um pedido a cada 24 h por usuário (usuarios_promocao_pedidos, migration 028). O
// registro é gravado ANTES do envio, num único INSERT condicional (sem corrida entre dois cliques), e
// removido se nenhum e-mail sair — assim uma falha de SMTP não "gasta" o pedido do usuário.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu05/services"
)

const (
	janelaPedidoPromocao = 24 * time.Hour
	maxMensagemPromocao  = 500
)

// enviarPedidoPromocao é trocável nos testes (o envio real fala com um servidor SMTP).
var enviarPedidoPromocao = services.SendPromotionRequestEmail

type promocaoStatus struct {
	PodeSolicitar bool       `json:"pode_solicitar"`
	Perfil        string     `json:"perfil"`
	UltimoPedido  *time.Time `json:"ultimo_pedido,omitempty"`
	ProximoPedido *time.Time `json:"proximo_pedido,omitempty"`
}

// PromocaoStatusHandler — GET /api/me/promocao.
func PromocaoStatusHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := GetUserIDFromContext(r)
		var perfil string
		if err := db.QueryRow(`SELECT perfil FROM usuarios WHERE id = $1 AND ativo = true`, userID).Scan(&perfil); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonErr(w, http.StatusUnauthorized, "Usuário não encontrado ou inativo")
				return
			}
			log.Printf("[Promocao] Erro ao ler perfil de %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		st := promocaoStatus{Perfil: perfil}
		var ultimo sql.NullTime
		err := db.QueryRow(`SELECT max(created_at) FROM usuarios_promocao_pedidos WHERE usuario_id = $1`, userID).Scan(&ultimo)
		if err != nil {
			log.Printf("[Promocao] Erro ao ler último pedido de %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		st.PodeSolicitar = perfil == "solicitante"
		if ultimo.Valid {
			t := ultimo.Time
			st.UltimoPedido = &t
			if proximo := t.Add(janelaPedidoPromocao); proximo.After(time.Now()) {
				st.PodeSolicitar = false
				st.ProximoPedido = &proximo
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(st)
	}
}

// SolicitarPromocaoHandler — POST /api/me/solicitar-promocao. Corpo opcional: {"mensagem": "..."}.
func SolicitarPromocaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := GetUserIDFromContext(r)

		var req struct {
			Mensagem string `json:"mensagem"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
				jsonErr(w, http.StatusBadRequest, "Corpo inválido")
				return
			}
		}
		// O texto vira parte de um e-mail: sem < >, controles e invisíveis; no máximo 500 caracteres.
		mensagem := SanitizarNome(req.Mensagem, maxMensagemPromocao)

		var nome, email, perfil string
		err := db.QueryRow(`SELECT nome, email, perfil FROM usuarios WHERE id = $1 AND ativo = true`, userID).Scan(&nome, &email, &perfil)
		if errors.Is(err, sql.ErrNoRows) {
			jsonErr(w, http.StatusUnauthorized, "Usuário não encontrado ou inativo")
			return
		}
		if err != nil {
			log.Printf("[Promocao] Erro ao ler usuário %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if perfil != "solicitante" {
			jsonErr(w, http.StatusConflict, "Seu perfil já é "+perfil+" — não há promoção a solicitar")
			return
		}

		// Administradores ativos que vão receber o aviso.
		rows, err := db.Query(`SELECT email FROM usuarios WHERE perfil = 'administrador' AND ativo = true AND email <> '' ORDER BY email`)
		if err != nil {
			log.Printf("[Promocao] Erro ao listar administradores: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		var admins []string
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err == nil {
				admins = append(admins, e)
			}
		}
		_ = rows.Close()
		// e-mails extras do parâmetro (Configurações), sem repetir quem já é administrador
		jaTem := map[string]bool{}
		for _, a := range admins {
			jaTem[strings.ToLower(a)] = true
		}
		for _, extra := range EmailsExtraPromocao(db) {
			if !jaTem[extra] {
				admins = append(admins, extra)
				jaTem[extra] = true
			}
		}
		if len(admins) == 0 {
			jsonErr(w, http.StatusConflict, "Não há administrador ativo nem e-mail de promoção configurado — fale com a Controladoria")
			return
		}

		// Registra o pedido só se não houver outro nas últimas 24 h (um único INSERT: sem corrida).
		var pedidoID int64
		err = db.QueryRow(`
			INSERT INTO usuarios_promocao_pedidos (usuario_id, perfil_atual, perfil_solicitado, mensagem, destinatarios)
			SELECT $1::uuid, $2, 'atendente', $3, $4
			WHERE NOT EXISTS (
				SELECT 1 FROM usuarios_promocao_pedidos
				WHERE usuario_id = $1::uuid AND created_at > now() - ($5 * interval '1 second')
			)
			RETURNING id`, userID, perfil, mensagem, len(admins), int(janelaPedidoPromocao.Seconds())).Scan(&pedidoID)
		if errors.Is(err, sql.ErrNoRows) {
			jsonErr(w, http.StatusTooManyRequests, "Você já enviou um pedido nas últimas 24 horas. Aguarde a resposta dos administradores.")
			return
		}
		if err != nil {
			log.Printf("[Promocao] Erro ao registrar pedido de %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		enviados, err := enviarPedidoPromocao(admins, services.PedidoPromocao{
			Nome: nome, Email: email, PerfilAtual: perfil, PerfilSolicitado: "atendente", Mensagem: mensagem,
		})
		if err != nil {
			// nenhum e-mail saiu: devolve o pedido ao usuário (pode tentar de novo)
			if _, delErr := db.Exec(`DELETE FROM usuarios_promocao_pedidos WHERE id = $1`, pedidoID); delErr != nil {
				log.Printf("[Promocao] Erro ao desfazer pedido %d: %v", pedidoID, delErr)
			}
			jsonErr(w, http.StatusBadGateway, "Não foi possível enviar o e-mail aos administradores agora. Tente novamente mais tarde.")
			return
		}
		if enviados != len(admins) {
			if _, uerr := db.Exec(`UPDATE usuarios_promocao_pedidos SET destinatarios = $1 WHERE id = $2`, enviados, pedidoID); uerr != nil {
				log.Printf("[Promocao] Erro ao atualizar destinatários do pedido %d: %v", pedidoID, uerr)
			}
		}
		log.Printf("[Promocao] %s (%s) pediu promoção a atendente; %d/%d administradores avisados", email, userID, enviados, len(admins))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"enviado": true, "administradores": enviados})
	}
}
