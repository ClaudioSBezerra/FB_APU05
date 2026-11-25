package handlers

// busca_solicitacoes.go — tela "Solicitações" (retorno da homologação, itens 20 e 21): consulta de TODOS os chamados
// (abertos e finalizados) para quem atende, e dos PRÓPRIOS chamados para o solicitante.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reCodigoChamado = regexp.MustCompile(`(?i)^\s*(?:GO-?)?(\d{1,12})\s*$`)

var statusBusca = map[string]bool{
	"aguardando_aprovacao": true, "aberta": true, "em_atendimento": true, "pendente": true,
	"finalizada_sucesso": true, "finalizada_erro": true, "rejeitada": true,
}

var tiposBusca = map[string]bool{"transferencia": true, "inclusao": true, "inclusao_sfc": true, "imobilizado": true, "obras": true}

func escapaLike(t string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(t)
}

// BuscarSolicitacoesHandler — GET /api/solicitacoes/busca?q=&status=&tipo=&de=AAAA-MM-DD&ate=AAAA-MM-DD&pagina=&tamanho=
// `q` procura por código (GO-12 ou 12), centro de custo (código/nome), conta (código/nome), solicitante (nome/e-mail),
// fornecedor e local de obra. Quem não atende só vê os chamados que abriu.
func BuscarSolicitacoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pagina, tamanho := paginacaoDaQuery(r)
		if r.URL.Query().Get("tamanho") == "" {
			tamanho = 25
		}
		qs := r.URL.Query()

		var onde []string
		var args []any
		arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }

		if !perfilAtende(GetPerfilFromContext(r)) {
			onde = append(onde, "s.solicitante_id = "+arg(GetUserIDFromContext(r)))
		}
		if st := qs.Get("status"); st != "" {
			if !statusBusca[st] {
				jsonErr(w, http.StatusBadRequest, "Status inválido no filtro.")
				return
			}
			onde = append(onde, "s.status = "+arg(st))
		}
		if tp := qs.Get("tipo"); tp != "" {
			if !tiposBusca[tp] {
				jsonErr(w, http.StatusBadRequest, "Tipo inválido no filtro.")
				return
			}
			onde = append(onde, "s.tipo_solicitacao = "+arg(tp))
		}
		// período pela data de abertura, no fuso de Recife (a data final é inclusiva)
		loc := agoraRecife().Location()
		if de := qs.Get("de"); de != "" {
			t, err := time.ParseInLocation("2006-01-02", de, loc)
			if err != nil {
				jsonErr(w, http.StatusBadRequest, "Data inicial inválida (use AAAA-MM-DD).")
				return
			}
			onde = append(onde, "s.created_at >= "+arg(t))
		}
		if ate := qs.Get("ate"); ate != "" {
			t, err := time.ParseInLocation("2006-01-02", ate, loc)
			if err != nil {
				jsonErr(w, http.StatusBadRequest, "Data final inválida (use AAAA-MM-DD).")
				return
			}
			onde = append(onde, "s.created_at < "+arg(t.AddDate(0, 0, 1)))
		}
		if q := strings.TrimSpace(qs.Get("q")); q != "" {
			var ou []string
			if m := reCodigoChamado.FindStringSubmatch(q); m != nil {
				n, _ := strconv.ParseInt(m[1], 10, 64)
				ou = append(ou, "s.numero = "+arg(n))
			}
			p := arg("%" + escapaLike(q) + "%")
			ou = append(ou,
				"cc.codigo ILIKE "+p, "cc.nome ILIKE "+p, "u.nome ILIKE "+p, "u.email ILIKE "+p, "s.fornecedor ILIKE "+p,
				"EXISTS (SELECT 1 FROM solicitacao_lancamentos l JOIN contas c ON c.id = l.conta_id WHERE l.solicitacao_id = s.id AND (c.codigo ILIKE "+p+" OR c.nome ILIKE "+p+"))",
				"EXISTS (SELECT 1 FROM solicitacao_obras_linhas ol JOIN locais_obra lo ON lo.id = ol.local_obra_id WHERE ol.solicitacao_id = s.id AND lo.nome ILIKE "+p+")",
			)
			onde = append(onde, "("+strings.Join(ou, " OR ")+")")
		}
		filtro := ""
		if len(onde) > 0 {
			filtro = "WHERE " + strings.Join(onde, " AND ")
		}

		// pede uma linha a mais para saber se há próxima página (sem COUNT caro)
		consulta := fmt.Sprintf(`
			SELECT s.id, s.numero, s.tipo_solicitacao, s.status, s.created_at, cc.codigo, cc.nome, u.nome, a.nome,
			       (SELECT lo.nome FROM solicitacao_obras_linhas ol JOIN locais_obra lo ON lo.id = ol.local_obra_id WHERE ol.solicitacao_id = s.id ORDER BY ol.created_at LIMIT 1),
			       COALESCE((SELECT SUM(l.valor) FROM solicitacao_lancamentos l WHERE l.solicitacao_id = s.id AND l.lado <> 'destino'), 0)::float8
			       + COALESCE((SELECT SUM(ol.valor) FROM solicitacao_obras_linhas ol WHERE ol.solicitacao_id = s.id AND ol.lado IS DISTINCT FROM 'inclusao'), 0)::float8
			FROM solicitacoes s
			LEFT JOIN centros_custo cc ON cc.id = s.centro_custo_id
			LEFT JOIN usuarios u ON u.id = s.solicitante_id
			LEFT JOIN usuarios a ON a.id = s.administrador_id
			%s
			ORDER BY s.numero DESC
			LIMIT %s OFFSET %s`, filtro, arg(tamanho+1), arg((pagina-1)*tamanho))
		rows, err := db.Query(consulta, args...)
		if err != nil {
			log.Printf("[Busca] erro: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		items := make([]map[string]any, 0, tamanho)
		for rows.Next() {
			var id, tipo, status string
			var numero int64
			var criada time.Time
			var ccCod, ccNome, solNome, atNome, local sql.NullString
			var valor float64
			if err := rows.Scan(&id, &numero, &tipo, &status, &criada, &ccCod, &ccNome, &solNome, &atNome, &local, &valor); err != nil {
				log.Printf("[Busca] leitura: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			items = append(items, map[string]any{
				"id": id, "codigo": CodigoChamado(numero), "tipo_solicitacao": tipo, "status": status, "created_at": criada,
				"centro_custo_codigo": nullStringOuNil(ccCod), "centro_custo_nome": nullStringOuNil(ccNome),
				"solicitante_nome": nullStringOuNil(solNome), "atendente_nome": nullStringOuNil(atNome),
				"local_obra": nullStringOuNil(local), "valor": valor,
			})
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Busca] iteração: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		temMais := len(items) > tamanho
		if temMais {
			items = items[:tamanho]
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "pagina": pagina, "tamanho": tamanho, "tem_mais": temMais})
	}
}
