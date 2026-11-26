package handlers

// busca_solicitacoes.go — tela "Solicitações" (retorno da homologação, itens 20 e 21): consulta de TODOS os chamados
// (abertos e finalizados) para quem atende, e dos PRÓPRIOS chamados para o solicitante.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reCodigoChamado = regexp.MustCompile(`(?i)^\s*(?:GO-?)?(\d{1,12})\s*$`)

var statusBusca = map[string]bool{
	"aguardando_aprovacao": true, "aberta": true, "em_atendimento": true, "pendente": true,
	"finalizada_sucesso": true, "finalizada_erro": true, "rejeitada": true, "cancelada": true,
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
			quem := arg(GetUserIDFromContext(r))
			onde = append(onde, "(s.solicitante_id = "+quem+" OR "+sqlAcompanha(quem)+")")
		}
		if st := qs.Get("status"); st != "" {
			if !statusBusca[st] {
				jsonErr(w, http.StatusBadRequest, "Status inválido no filtro.")
				return
			}
			onde = append(onde, "s.status = "+arg(st))
		}
		// F11 (26/11): filtro desconhecido é 400 — nunca "parece filtrado e não está"
		for k := range qs {
			if !filtrosBusca[k] {
				jsonErr(w, http.StatusBadRequest, "Filtro desconhecido: "+SanitizarNome(k, 30)+".")
				return
			}
		}
		// F11: filial/divisão, centro de custo e conta, no lado escolhido (origem, destino ou qualquer). Fora da
		// Transferência só existe o destino, e o lado fica sem efeito.
		if cond, msg := filtroLancamentos(qs, arg); msg != "" {
			jsonErr(w, http.StatusBadRequest, msg)
			return
		} else if cond != "" {
			onde = append(onde, cond)
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
			       COALESCE((SELECT cu.nome FROM solicitacao_compartilhamentos sc JOIN usuarios cu ON cu.id = sc.compartilhado_por WHERE sc.solicitacao_id = s.id AND sc.usuario_id = %[1]s),
			                (SELECT 'grupo ' || g.nome FROM solicitacao_grupos sg JOIN grupos g ON g.id = sg.grupo_id AND g.ativo WHERE sg.solicitacao_id = s.id
			                   AND (g.gestor_id = %[1]s OR EXISTS (SELECT 1 FROM grupo_membros gm WHERE gm.grupo_id = g.id AND gm.usuario_id = %[1]s)) LIMIT 1)),
			       COALESCE((SELECT SUM(l.valor) FROM solicitacao_lancamentos l WHERE l.solicitacao_id = s.id AND (l.lado = 'origem' OR s.tipo_solicitacao <> 'transferencia')), 0)::float8
			       + COALESCE((SELECT SUM(ol.valor) FROM solicitacao_obras_linhas ol WHERE ol.solicitacao_id = s.id AND ol.lado IS DISTINCT FROM 'inclusao'), 0)::float8
			FROM solicitacoes s
			LEFT JOIN centros_custo cc ON cc.id = s.centro_custo_id
			LEFT JOIN usuarios u ON u.id = s.solicitante_id
			LEFT JOIN usuarios a ON a.id = s.administrador_id
			%s
			ORDER BY s.numero DESC
			LIMIT %[3]s OFFSET %[4]s`, arg(GetUserIDFromContext(r)), filtro, arg(tamanho+1), arg((pagina-1)*tamanho))
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
			var ccCod, ccNome, solNome, atNome, local, compartilhadoPor sql.NullString
			var valor float64
			if err := rows.Scan(&id, &numero, &tipo, &status, &criada, &ccCod, &ccNome, &solNome, &atNome, &local, &compartilhadoPor, &valor); err != nil {
				log.Printf("[Busca] leitura: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			items = append(items, map[string]any{
				"id": id, "codigo": CodigoChamado(numero), "tipo_solicitacao": tipo, "status": status, "created_at": criada,
				"centro_custo_codigo": nullStringOuNil(ccCod), "centro_custo_nome": nullStringOuNil(ccNome),
				"solicitante_nome": nullStringOuNil(solNome), "atendente_nome": nullStringOuNil(atNome),
				"local_obra": nullStringOuNil(local), "valor": valor, "compartilhado_por": nullStringOuNil(compartilhadoPor),
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

// filtrosBusca: os únicos parâmetros aceitos em /api/solicitacoes/busca.
var filtrosBusca = map[string]bool{
	"q": true, "status": true, "tipo": true, "de": true, "ate": true, "pagina": true, "tamanho": true,
	"divisao": true, "cc": true, "conta": true, "lado": true,
}

var (
	reDivisaoFiltro = regexp.MustCompile(`^[0-9]{1,4}$`)
	reCCFiltro      = regexp.MustCompile(`^[0-9]{4,10}$`)
	reContaFiltro   = regexp.MustCompile(`^[0-9]{4,12}$`)
)

// sqlDivisaoDoCC (F13): a divisão é o código do CC sem os 4 últimos dígitos, com zeros à esquerda até 4
// (1016000 → 0101; 816000 → 0081). CC de 4 dígitos (corporativo) não tem divisão.
func sqlDivisaoDoCC(col string) string {
	return "(CASE WHEN length(" + col + ") > 4 AND " + col + " ~ '^[0-9]+$' THEN lpad(left(" + col + ", length(" + col + ") - 4), 4, '0') END)"
}

// filtroLancamentos monta o EXISTS sobre as linhas do chamado.
func filtroLancamentos(qs url.Values, arg func(any) string) (string, string) {
	div, cc, conta, lado := strings.TrimSpace(qs.Get("divisao")), strings.TrimSpace(qs.Get("cc")), strings.TrimSpace(qs.Get("conta")), strings.TrimSpace(qs.Get("lado"))
	if div == "" && cc == "" && conta == "" {
		if lado != "" && lado != "qualquer" && lado != "origem" && lado != "destino" {
			return "", "Lado inválido no filtro (use origem, destino ou qualquer)."
		}
		return "", ""
	}
	var conds []string
	switch lado {
	case "", "qualquer":
	case "origem", "destino":
		conds = append(conds, "(s.tipo_solicitacao <> 'transferencia' OR l.lado = "+arg(lado)+")")
	default:
		return "", "Lado inválido no filtro (use origem, destino ou qualquer)."
	}
	if div != "" {
		if !reDivisaoFiltro.MatchString(div) {
			return "", "Divisão inválida no filtro (até 4 dígitos)."
		}
		conds = append(conds, sqlDivisaoDoCC("ccl.codigo")+" = "+arg(CodigoDivisao4(div)))
	}
	if cc != "" {
		if !reCCFiltro.MatchString(cc) {
			return "", "Centro de custo inválido no filtro (4 dígitos, ou o código completo)."
		}
		conds = append(conds, "right(ccl.codigo, 4) = "+arg(cc[len(cc)-4:]))
		if len(cc) > 4 { // código completo (6, 7 ou 8 dígitos, com ou sem zero à esquerda): fixa também a divisão
			conds = append(conds, sqlDivisaoDoCC("ccl.codigo")+" = "+arg(CodigoDivisao4(strings.TrimLeft(cc[:len(cc)-4], "0"))))
		}
	}
	if conta != "" {
		if !reContaFiltro.MatchString(conta) {
			return "", "Conta inválida no filtro."
		}
		conds = append(conds, "ct.codigo = "+arg(conta))
	}
	return "EXISTS (SELECT 1 FROM solicitacao_lancamentos l JOIN centros_custo ccl ON ccl.id = l.centro_custo_id LEFT JOIN contas ct ON ct.id = l.conta_id WHERE l.solicitacao_id = s.id AND " +
		strings.Join(conds, " AND ") + ")", ""
}

// FiltrosBuscaHandler — GET /api/solicitacoes/filtros: as opções dos filtros (divisões, CCs e contas) só do que o
// usuário pode ver — o solicitante nunca recebe a lista inteira de CCs.
func FiltrosBuscaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var args []any
		vis := "true"
		if !perfilAtende(GetPerfilFromContext(r)) {
			args = append(args, GetUserIDFromContext(r))
			vis = "(s.solicitante_id = $1 OR " + sqlAcompanha("$1") + ")"
		}
		base := ` FROM solicitacoes s JOIN solicitacao_lancamentos l ON l.solicitacao_id = s.id JOIN centros_custo ccl ON ccl.id = l.centro_custo_id
			LEFT JOIN contas ct ON ct.id = l.conta_id WHERE ` + vis
		lista := func(q string) ([]map[string]string, error) {
			rows, err := db.Query(q, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []map[string]string{}
			for rows.Next() {
				var cod, nome string
				if rows.Scan(&cod, &nome) == nil && cod != "" {
					out = append(out, map[string]string{"codigo": cod, "nome": nome})
				}
			}
			return out, rows.Err()
		}
		divs, err1 := lista(`SELECT DISTINCT d.codigo, COALESCE(c.nome || COALESCE(' — ' || initcap(lower(c.cidade)) || '/' || c.uf, ''), '')` + base[:strings.Index(base, " WHERE")] +
			` JOIN divisoes d ON d.codigo = ` + sqlDivisaoDoCC("ccl.codigo") + ` LEFT JOIN centros c ON c.codigo = d.codigo WHERE ` + vis + ` ORDER BY 1`)
		ccs, err2 := lista(`SELECT DISTINCT ccl.codigo, ccl.nome` + base + ` ORDER BY 1`)
		contas, err3 := lista(`SELECT DISTINCT ct.codigo, ct.nome` + base + ` AND ct.codigo IS NOT NULL ORDER BY 1`)
		if err := errors.Join(err1, err2, err3); err != nil {
			log.Printf("[Busca] filtros: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"divisoes": divs, "centros_custo": ccs, "contas": contas})
	}
}
