package handlers

// edicao_chamado.go — edição do chamado pela Controladoria (resposta de 26/11/2026, §3):
//   - o atendimento edita os campos de todos os formulários (cabeçalho, linhas de verba e linhas de Obras);
//   - mudança de valor, conta, centro de custo ou mês da Transferência reconsulta o saldo, com o MESMO bloqueio da
//     abertura (item 6);
//   - o histórico guarda campo, valor antigo, valor novo, quem e quando (comentário do tipo "edicao");
//   - o solicitante e quem acompanha recebem o que mudou por e-mail.
// Status em que se edita (sugestão da TI, a Controladoria confere): aberta, em_atendimento e pendente.
// Grava pela conexão PRIVILEGIADA (a geral não faz UPDATE em solicitacoes — migration 010; grants na 053).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
)

var statusEditaveis = map[string]bool{"aberta": true, "em_atendimento": true, "pendente": true}

type edicaoLancamento struct {
	ID            string   `json:"id"`
	ContaID       *string  `json:"conta_id"`
	CentroCustoID *string  `json:"centro_custo_id"`
	Mes           *string  `json:"mes"`
	Valor         *float64 `json:"valor"`
}

type edicaoObraLinha struct {
	ID                string   `json:"id"`
	Valor             *float64 `json:"valor"`
	OrdemInvestimento *string  `json:"ordem_investimento"`
}

type edicaoRequest struct {
	Versao                  int                `json:"versao"`
	Descricao               *string            `json:"descricao"`
	Fornecedor              *string            `json:"fornecedor"`
	FornecedorCNPJ          *string            `json:"fornecedor_cnpj"`
	FornecedorNaoCadastrado *bool              `json:"fornecedor_nao_cadastrado"`
	TipoCompra              *string            `json:"tipo_compra"`
	QuantidadeMaterial      *int               `json:"quantidade_material"`
	ValorUnitario           *float64           `json:"valor_unitario"`
	Lancamentos             []edicaoLancamento `json:"lancamentos"`
	ObrasLinhas             []edicaoObraLinha  `json:"obras_linhas"`
}

type linhaEdicao struct {
	lancamentoValidado
	id                  string
	contaCod, ccCod     string
	contaAntes, ccAntes string
	mesAntes            time.Time
	valorAntes          float64
	tipoAlterado        bool
}

func textoOuTraco(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// EditarChamadoHandler — PUT /api/chamados/{id}/edicao (atendente/administrador).
func EditarChamadoHandler(dbApp, dbPriv *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		var req edicaoRequest
		corpo, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<18))
		if json.Unmarshal(corpo, &req) != nil || req.Versao < 1 {
			jsonErr(w, http.StatusBadRequest, "Os dados enviados estão incompletos ou em formato inválido. Atualize a página e tente de novo.")
			return
		}
		autor := GetUserIDFromContext(r)

		tx, err := dbPriv.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var tipo, status, classificacao, tipoTransf string
		var versao int
		var descricao, fornecedor, cnpj, tipoCompra sql.NullString
		var naoCadastrado bool
		var qtd sql.NullInt64
		var unit sql.NullFloat64
		err = tx.QueryRow(`SELECT tipo_solicitacao, status, versao, descricao, fornecedor, fornecedor_cnpj, fornecedor_nao_cadastrado,
				tipo_compra, quantidade_material, valor_unitario::float8, COALESCE(classificacao, ''), COALESCE(tipo_transferencia, '')
			FROM solicitacoes WHERE id = $1 FOR UPDATE`, id).
			Scan(&tipo, &status, &versao, &descricao, &fornecedor, &cnpj, &naoCadastrado, &tipoCompra, &qtd, &unit, &classificacao, &tipoTransf)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		} else if err != nil {
			log.Printf("[Edicao] ler %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if versao != req.Versao {
			jsonErrConflitoVersao(w, versao)
			return
		}
		if !statusEditaveis[status] {
			jsonErr(w, http.StatusConflict, "Este chamado não pode mais ser editado (só aberto, em atendimento ou pendente).")
			return
		}

		var mudancas []string
		mudou := func(campo, antes, depois string) {
			mudancas = append(mudancas, fmt.Sprintf("%s: %s → %s", campo, textoOuTraco(antes), textoOuTraco(depois)))
		}

		// ---------------- cabeçalho
		novaDesc, novoForn, novoCNPJ, novoTipoCompra := descricao, fornecedor, cnpj, tipoCompra
		novoNaoCad := naoCadastrado
		if req.Descricao != nil && strings.TrimSpace(*req.Descricao) != descricao.String {
			if msg := validarDescricao(tipo, *req.Descricao); msg != "" {
				jsonErr(w, http.StatusBadRequest, msg)
				return
			}
			novaDesc = stringOuNulo(*req.Descricao)
			mudou("Descrição", descricao.String, novaDesc.String)
		}
		if req.Fornecedor != nil && strings.TrimSpace(*req.Fornecedor) != fornecedor.String {
			if msg := validarFornecedorTamanho(*req.Fornecedor); msg != "" {
				jsonErr(w, http.StatusBadRequest, msg)
				return
			}
			novoForn = stringOuNulo(*req.Fornecedor)
			mudou("Fornecedor", fornecedor.String, novoForn.String)
		}
		if req.FornecedorCNPJ != nil {
			c, msg := validarCNPJFornecedor(*req.FornecedorCNPJ)
			if msg != "" {
				jsonErr(w, http.StatusBadRequest, "O CNPJ deve ter 14 dígitos (ou CPF com 11).")
				return
			}
			if c.String != cnpj.String {
				novoCNPJ = c
				mudou("CNPJ do fornecedor", cnpj.String, c.String)
			}
		}
		if req.FornecedorNaoCadastrado != nil && *req.FornecedorNaoCadastrado != naoCadastrado {
			novoNaoCad = *req.FornecedorNaoCadastrado
			mudou("Fornecedor não cadastrado", map[bool]string{true: "sim", false: "não"}[naoCadastrado], map[bool]string{true: "sim", false: "não"}[novoNaoCad])
		}
		novaQtd, novoUnit := qtd, unit
		if tipo == "imobilizado" {
			if req.TipoCompra != nil {
				tc, msg := validarTipoCompra(*req.TipoCompra)
				if msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
				if tc != tipoCompra.String {
					novoTipoCompra = sql.NullString{String: tc, Valid: true}
					mudou("Tipo de compra", tipoCompra.String, tc)
				}
			}
			if req.QuantidadeMaterial != nil && int64(*req.QuantidadeMaterial) != qtd.Int64 {
				if *req.QuantidadeMaterial <= 0 {
					jsonErr(w, http.StatusBadRequest, "A quantidade deve ser um número inteiro maior que zero.")
					return
				}
				novaQtd = sql.NullInt64{Int64: int64(*req.QuantidadeMaterial), Valid: true}
				mudou("Quantidade do material", fmt.Sprint(qtd.Int64), fmt.Sprint(novaQtd.Int64))
			}
			if req.ValorUnitario != nil && math.Abs(*req.ValorUnitario-unit.Float64) > 0.004 {
				if *req.ValorUnitario <= 0 {
					jsonErr(w, http.StatusBadRequest, "Informe o valor unitário do imobilizado (maior que zero).")
					return
				}
				if msg := validarValorMaximo(*req.ValorUnitario); msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
				novoUnit = sql.NullFloat64{Float64: math.Round(*req.ValorUnitario*100) / 100, Valid: true}
				mudou("Valor unitário", moedaBR(unit.Float64), moedaBR(novoUnit.Float64))
			}
		}

		// ---------------- linhas de verba
		var linhas []*linhaEdicao
		if tipo != "obras" {
			rows, err := tx.Query(`SELECT l.id, l.lado, l.divisao_id, l.centro_custo_id, COALESCE(l.conta_id::text, ''), l.mes, l.valor::float8,
					cc.codigo, COALESCE(c.codigo, '')
				FROM solicitacao_lancamentos l JOIN centros_custo cc ON cc.id = l.centro_custo_id LEFT JOIN contas c ON c.id = l.conta_id
				WHERE l.solicitacao_id = $1 ORDER BY l.lado DESC, l.mes`, id)
			if err != nil {
				log.Printf("[Edicao] linhas %s: %v", id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			for rows.Next() {
				l := &linhaEdicao{}
				var mes sql.NullTime
				if err := rows.Scan(&l.id, &l.Lado, &l.DivisaoID, &l.CentroCustoID, &l.ContaID, &mes, &l.Valor, &l.ccCod, &l.contaCod); err != nil {
					rows.Close()
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				l.Mes = mes.Time
				l.contaAntes, l.ccAntes, l.mesAntes, l.valorAntes = l.contaCod, l.ccCod, l.Mes, l.Valor
				linhas = append(linhas, l)
			}
			rows.Close()
		}
		porID := map[string]*linhaEdicao{}
		for _, l := range linhas {
			porID[strings.ToLower(l.id)] = l
		}
		mexeuSaldo := false
		for _, e := range req.Lancamentos {
			l := porID[strings.ToLower(e.ID)]
			if l == nil {
				jsonErr(w, http.StatusBadRequest, "Linha do chamado não encontrada. Atualize a página.")
				return
			}
			rot := fmt.Sprintf("%s %s", map[string]string{"origem": "Origem", "destino": "Destino"}[l.Lado], mesAnoSeguro(l.mesAntes))
			if e.ContaID != nil && *e.ContaID != "" && !strings.EqualFold(*e.ContaID, l.ContaID) && tipo != "imobilizado" {
				var cod string
				if !uuidFormatRegexp.MatchString(*e.ContaID) || tx.QueryRow(`SELECT codigo FROM contas WHERE id = $1`, *e.ContaID).Scan(&cod) != nil {
					jsonErr(w, http.StatusBadRequest, "Conta não encontrada.")
					return
				}
				l.ContaID, l.contaCod, l.tipoAlterado = *e.ContaID, cod, true
				mudou(rot+" — conta", l.contaAntes, cod)
			}
			if e.CentroCustoID != nil && *e.CentroCustoID != "" && !strings.EqualFold(*e.CentroCustoID, l.CentroCustoID) {
				var cod, div string
				if !uuidFormatRegexp.MatchString(*e.CentroCustoID) || tx.QueryRow(`SELECT codigo, divisao_id FROM centros_custo WHERE id = $1`, *e.CentroCustoID).Scan(&cod, &div) != nil {
					jsonErr(w, http.StatusBadRequest, "Centro de custo não encontrado.")
					return
				}
				l.CentroCustoID, l.DivisaoID, l.ccCod, l.tipoAlterado = *e.CentroCustoID, div, cod, true
				mudou(rot+" — centro de custo", l.ccAntes, cod)
			}
			if e.Mes != nil && *e.Mes != "" {
				m := *e.Mes
				if len(m) == 7 {
					m += "-01"
				}
				t, err := parseMesCompetencia(m)
				if err != nil {
					jsonErr(w, http.StatusBadRequest, "Mês inválido.")
					return
				}
				if !t.Equal(l.Mes) {
					l.Mes, l.tipoAlterado = t, true
					mudou(rot+" — mês", mesAnoSeguro(l.mesAntes), mesAno(t))
				}
			}
			if e.Valor != nil && math.Abs(*e.Valor-l.Valor) > 0.004 && tipo != "imobilizado" {
				if *e.Valor <= 0 {
					jsonErr(w, http.StatusBadRequest, "O valor deve ser maior que zero.")
					return
				}
				if msg := validarValorMaximo(*e.Valor); msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
				l.Valor, l.tipoAlterado = math.Round(*e.Valor*100)/100, true
				mudou(rot+" — valor", moedaBR(l.valorAntes), moedaBR(l.Valor))
			}
			if l.tipoAlterado {
				mexeuSaldo = true
			}
		}
		// Imobilizado: o total da linha acompanha quantidade × unitário
		if tipo == "imobilizado" && len(linhas) == 1 && novaQtd.Valid && novoUnit.Valid {
			total := totalImobilizado(int(novaQtd.Int64), novoUnit.Float64)
			if math.Abs(total-linhas[0].Valor) > 0.004 {
				mudou("Valor total", moedaBR(linhas[0].Valor), moedaBR(total))
				linhas[0].Valor, linhas[0].tipoAlterado = total, true
			}
		}
		if tipo == "inclusao" && mexeuSaldo {
			lv := make([]lancamentoValidado, len(linhas))
			for i, l := range linhas {
				lv[i] = l.lancamentoValidado
			}
			if msg := validarMesesDeUmaConta(lv, "Inclusão"); msg != "" {
				jsonErr(w, http.StatusBadRequest, msg)
				return
			}
		}
		if tipo == "transferencia" {
			// destino único acompanha a soma da origem (como no formulário)
			var destinos []*linhaEdicao
			for _, l := range linhas {
				if l.Lado == "destino" {
					destinos = append(destinos, l)
				}
			}
			lv := make([]lancamentoValidado, len(linhas))
			for i, l := range linhas {
				lv[i] = l.lancamentoValidado
			}
			if len(destinos) == 1 {
				soma := math.Round(somaPorLado(lv, "origem")*100) / 100
				if math.Abs(soma-destinos[0].Valor) > 0.004 {
					mudou("Destino — valor (soma da origem)", moedaBR(destinos[0].Valor), moedaBR(soma))
					destinos[0].Valor, destinos[0].tipoAlterado = soma, true
					lv = lv[:0]
					for _, l := range linhas {
						lv = append(lv, l.lancamentoValidado)
					}
				}
			}
			for _, f := range []func([]lancamentoValidado) string{validarBalanceamento, validarTransferenciaUmaContaPorLado, validarTransferenciaPropria, validarExercicioUnico} {
				if msg := f(lv); msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
			}
			if tipoTransf != "" {
				if msg, err := conferirTipoDestino(tx, lv, tipoTransf); err != nil {
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				} else if msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
			}
			if mexeuSaldo {
				msg, err := conferirSaldoOrigem(r.Context(), dbApp, tx, lv, id)
				if err != nil {
					log.Printf("[Edicao] saldo %s: %v", id, err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				if msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
			}
		}

		// ---------------- linhas de Obras
		type obraEd struct {
			id, lado, local, subgrupo, ordem string
			valor                            float64
			alterada                         bool
		}
		var obras []*obraEd
		if tipo == "obras" && len(req.ObrasLinhas) > 0 {
			rows, err := tx.Query(`SELECT id, COALESCE(lado, ''), local_obra_id, subgrupo_despesa_id, ordem_investimento, valor::float8 FROM solicitacao_obras_linhas WHERE solicitacao_id = $1`, id)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			for rows.Next() {
				o := &obraEd{}
				if rows.Scan(&o.id, &o.lado, &o.local, &o.subgrupo, &o.ordem, &o.valor) == nil {
					obras = append(obras, o)
				}
			}
			rows.Close()
			idx := map[string]*obraEd{}
			for _, o := range obras {
				idx[strings.ToLower(o.id)] = o
			}
			for i, e := range req.ObrasLinhas {
				o := idx[strings.ToLower(e.ID)]
				if o == nil {
					jsonErr(w, http.StatusBadRequest, "Linha de Obras não encontrada. Atualize a página.")
					return
				}
				rot := fmt.Sprintf("Linha %d de Obras", i+1)
				if e.Valor != nil && math.Abs(*e.Valor-o.valor) > 0.004 {
					if *e.Valor <= 0 || validarValorMaximo(*e.Valor) != "" {
						jsonErr(w, http.StatusBadRequest, "Valor de Obras inválido (maior que zero e dentro do máximo).")
						return
					}
					mudou(rot+" — valor", moedaBR(o.valor), moedaBR(*e.Valor))
					o.valor, o.alterada = math.Round(*e.Valor*100)/100, true
				}
				if e.OrdemInvestimento != nil && strings.TrimSpace(*e.OrdemInvestimento) != "" && strings.TrimSpace(*e.OrdemInvestimento) != o.ordem {
					nova := strings.TrimSpace(*e.OrdemInvestimento)
					if nova != "CRIAR" {
						var loc, sub string
						if tx.QueryRow(`SELECT local_obra_id, subgrupo_despesa_id FROM obra_ordens WHERE numero_ordem = $1`, nova).Scan(&loc, &sub) != nil ||
							!strings.EqualFold(loc, o.local) || !strings.EqualFold(sub, o.subgrupo) {
							jsonErr(w, http.StatusBadRequest, "A ordem de investimento não existe para este local e subgrupo.")
							return
						}
					}
					mudou(rot+" — ordem", o.ordem, nova)
					o.ordem, o.alterada = nova, true
				}
			}
			ov := make([]obraLinhaValidado, len(obras))
			for i, o := range obras {
				ov[i] = obraLinhaValidado{OrdemInvestimento: o.ordem, Valor: o.valor, Lado: sql.NullString{String: o.lado, Valid: o.lado != ""},
					LocalObraID: o.local, SubgrupoDespesaID: o.subgrupo}
			}
			if msg, err := conferirGraoObras(tx, classificacao, ov); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			} else if msg != "" {
				jsonErr(w, http.StatusBadRequest, msg)
				return
			}
			if msg := validarCriarObras(classificacao, ov); msg != "" {
				jsonErr(w, http.StatusBadRequest, msg)
				return
			}
			if classificacao == classificacaoTransferenciaSaldo {
				if msg := validarBlocosObras(ov); msg != "" {
					jsonErr(w, http.StatusBadRequest, msg)
					return
				}
			}
		}

		if len(mudancas) == 0 {
			jsonErr(w, http.StatusBadRequest, "Nada mudou: altere algum campo antes de salvar.")
			return
		}

		// ---------------- gravação
		if _, err := tx.Exec(`UPDATE solicitacoes SET descricao = $2, fornecedor = $3, fornecedor_cnpj = $4, fornecedor_nao_cadastrado = $5,
				tipo_compra = $6, quantidade_material = $7, valor_unitario = $8, versao = versao + 1 WHERE id = $1`,
			id, novaDesc, novoForn, novoCNPJ, novoNaoCad, novoTipoCompra, novaQtd, novoUnit); err != nil {
			log.Printf("[Edicao] gravar cabeçalho %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		for _, l := range linhas {
			if !l.tipoAlterado {
				continue
			}
			var conta any
			if l.ContaID != "" {
				conta = l.ContaID
			}
			if _, err := tx.Exec(`UPDATE solicitacao_lancamentos SET conta_id = $2, centro_custo_id = $3, divisao_id = $4, mes = $5, valor = $6 WHERE id = $1`,
				l.id, conta, l.CentroCustoID, l.DivisaoID, l.Mes.Format(dataISOFormato), l.Valor); err != nil {
				log.Printf("[Edicao] gravar linha %s: %v", l.id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		for _, o := range obras {
			if !o.alterada {
				continue
			}
			if _, err := tx.Exec(`UPDATE solicitacao_obras_linhas SET valor = $2, ordem_investimento = $3 WHERE id = $1`, o.id, o.valor, o.ordem); err != nil {
				log.Printf("[Edicao] gravar linha de obras %s: %v", o.id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		texto := "Editou o chamado:\n- " + strings.Join(mudancas, "\n- ")
		if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'edicao', $3)`, id, autor, texto); err != nil {
			log.Printf("[Edicao] histórico %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		log.Printf("[Edicao] %s editou %s (%d mudança(s))", autor, id, len(mudancas))

		itens := ""
		for _, m := range mudancas {
			itens += "<li>" + html.EscapeString(m) + "</li>"
		}
		go avisarAcompanhantes(dbApp, id, autor, "chamado editado pela Controladoria", "<p>A Controladoria alterou o chamado:</p><ul>"+itens+"</ul>")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"versao": versao + 1, "mudancas": mudancas})
	}
}

func mesAnoSeguro(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return mesAno(t)
}
