package handlers

// painel.go — painel de solicitações (Epic 5, FR-17), refeito em 26/11/2026 (F5 da segunda leva):
//   - recorte por PERÍODO (data de abertura, fuso de Recife), por QUEM ABRIU e por RESULTADO;
//   - por FILIAL (divisão do CC do chamado): valor já transferido e já incluído (chamados finalizados com sucesso);
//   - VISIBILIDADE no servidor: atendente e administrador veem tudo; os demais, só os chamados que abriram, os que
//     acompanham (cópia, grupo em cópia e escopo de CC do grupo — sqlAcompanha). O filtro "Quem abriu" só lista
//     quem a pessoa já pode ver e só existe quando há mais alguém além dela ("Você" + os colegas).
// Conta ao vivo em `solicitacoes` (volume pequeno); painel_snapshots continua sendo gerado pelo job, mas a tela
// não depende mais dele.
//
//	GET /api/paineis/consolidado?de=AAAA-MM-DD&ate=AAAA-MM-DD&solicitante=<uuid>

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type painelItem struct {
	Chave      string `json:"chave"`
	Rotulo     string `json:"rotulo"`
	Quantidade int    `json:"quantidade"`
}

type painelFilial struct {
	Divisao     string  `json:"divisao"`
	Rotulo      string  `json:"rotulo"`
	Transferido float64 `json:"transferido"`
	Incluido    float64 `json:"incluido"`
	Chamados    int     `json:"chamados"`
}

type pessoaFiltro struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

// ObterPainelHandler — GET /api/paineis/{painel}. Só "consolidado".
func ObterPainelHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.PathValue("painel") != "consolidado" {
			jsonErr(w, http.StatusNotFound, "painel não encontrado")
			return
		}
		eu := GetUserIDFromContext(r)
		geral := perfilAtende(GetPerfilFromContext(r))
		qs := r.URL.Query()

		var args []any
		arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
		var onde []string
		if !geral {
			p := arg(eu)
			onde = append(onde, "(s.solicitante_id = "+p+" OR "+sqlAcompanha(p)+")")
		}
		visibilidade := strings.Join(onde, " AND ")
		argsVisibilidade := append([]any(nil), args...)

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
		if sol := qs.Get("solicitante"); sol != "" {
			if !uuidFormatRegexp.MatchString(sol) {
				jsonErr(w, http.StatusBadRequest, "Filtro de solicitante inválido.")
				return
			}
			onde = append(onde, "s.solicitante_id = "+arg(sol))
		}
		filtro := ""
		if len(onde) > 0 {
			filtro = "WHERE " + strings.Join(onde, " AND ")
		}

		contar := func(q string, rotulos map[string]string, comRotulo bool) ([]painelItem, error) {
			rows, err := db.Query(q, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := make([]painelItem, 0)
			for rows.Next() {
				var it painelItem
				if comRotulo {
					err = rows.Scan(&it.Chave, &it.Rotulo, &it.Quantidade)
				} else {
					err = rows.Scan(&it.Chave, &it.Quantidade)
					it.Rotulo = rotuloOuChave(rotulos, it.Chave)
				}
				if err != nil {
					return nil, err
				}
				out = append(out, it)
			}
			return out, rows.Err()
		}
		porTipo, err := contar(`SELECT s.tipo_solicitacao, COUNT(*) FROM solicitacoes s `+filtro+` GROUP BY 1 ORDER BY 1`, rotulosTipoSolicitacao, false)
		if err == nil {
			var porStatus, porCC []painelItem
			porStatus, err = contar(`SELECT s.status, COUNT(*) FROM solicitacoes s `+filtro+` GROUP BY 1 ORDER BY 1`, rotulosStatusSolicitacao, false)
			if err == nil {
				porCC, err = contar(`SELECT cc.codigo, cc.nome, COUNT(*) FROM solicitacoes s JOIN centros_custo cc ON cc.id = s.centro_custo_id `+filtro+` GROUP BY 1, 2 ORDER BY 1`, nil, true)
			}
			if err == nil {
				var filiais []painelFilial
				filiais, err = painelPorFilial(db, filtro, args)
				if err == nil {
					var pessoas []pessoaFiltro
					if !geral {
						pessoas, err = pessoasVisiveis(db, visibilidade, argsVisibilidade, eu)
					}
					if err == nil {
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("Cache-Control", "no-store")
						escopo := "geral"
						if !geral {
							escopo = "meus"
						}
						_ = json.NewEncoder(w).Encode(map[string]any{
							"por_tipo": porTipo, "por_status": porStatus, "por_cc": porCC, "por_filial": filiais,
							"solicitantes": pessoas, "escopo": escopo, "gerado_em": time.Now(),
						})
						return
					}
				}
			}
		}
		log.Printf("[Painel] Erro ao montar o painel (%s): %v", eu, err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
	}
}

// painelPorFilial: por divisão do CC do chamado, o valor já TRANSFERIDO (saída da origem) e já INCLUÍDO (Inclusão e
// Inclusão SFC), só de chamados finalizados com sucesso, e o número de chamados (qualquer status) no recorte.
func painelPorFilial(db *sql.DB, filtro string, args []any) ([]painelFilial, error) {
	rows, err := db.Query(`
		SELECT d.codigo,
		       COALESCE(c.nome || COALESCE(' — ' || initcap(lower(c.cidade)) || '/' || c.uf, ''), NULLIF(d.nome, d.codigo), d.codigo),
		       COALESCE(SUM(CASE WHEN s.status = 'finalizada_sucesso' AND s.tipo_solicitacao = 'transferencia'
		            THEN (SELECT SUM(l.valor) FROM solicitacao_lancamentos l WHERE l.solicitacao_id = s.id AND l.lado = 'origem') END), 0)::float8,
		       COALESCE(SUM(CASE WHEN s.status = 'finalizada_sucesso' AND s.tipo_solicitacao IN ('inclusao', 'inclusao_sfc')
		            THEN (SELECT SUM(l.valor) FROM solicitacao_lancamentos l WHERE l.solicitacao_id = s.id) END), 0)::float8,
		       COUNT(*)
		FROM solicitacoes s
		JOIN centros_custo cc ON cc.id = s.centro_custo_id
		JOIN divisoes d ON d.id = cc.divisao_id
		LEFT JOIN centros c ON c.codigo = d.codigo
		`+filtro+`
		GROUP BY 1, 2 ORDER BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []painelFilial{}
	for rows.Next() {
		var f painelFilial
		if err := rows.Scan(&f.Divisao, &f.Rotulo, &f.Transferido, &f.Incluido, &f.Chamados); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// pessoasVisiveis: quem abriu chamados que a pessoa pode ver. Vazio quando só existe ela (o filtro não aparece);
// ela mesma vem primeiro, como "Você".
func pessoasVisiveis(db *sql.DB, visibilidade string, args []any, eu string) ([]pessoaFiltro, error) {
	rows, err := db.Query(`SELECT DISTINCT u.id, u.nome FROM solicitacoes s JOIN usuarios u ON u.id = s.solicitante_id WHERE `+visibilidade+` ORDER BY u.nome`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	outros := []pessoaFiltro{}
	for rows.Next() {
		var p pessoaFiltro
		if err := rows.Scan(&p.ID, &p.Nome); err != nil {
			return nil, err
		}
		if p.ID != eu {
			outros = append(outros, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(outros) == 0 {
		return []pessoaFiltro{}, nil
	}
	return append([]pessoaFiltro{{ID: eu, Nome: "Você"}}, outros...), nil
}
