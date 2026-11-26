package handlers

// controle_ti_sap_api.go — carga do Realizado (KSB1) e do Compromissado (KSB2) do Controle de Despesas pelas APIs do
// SAP (CostCenterActual / CostCenterCommitment), em vez das planilhas. Grava pelos MESMOS caminhos da importação de
// planilha (controleti.ImportarKSB1 / ImportarKSB2), então conferência, fechamento e relatórios não mudam; a planilha
// continua aceita como plano B.
//
// Escopo da leitura: os centros de custo que o módulo já conhece (lançamentos e cargas KSB anteriores).
//   - KSB1: lançamentos de 1º de janeiro do ano corrente até hoje; substitui esses meses (como a planilha).
//   - KSB2: foto do compromisso EM ABERTO, documentos lançados de 1º de janeiro do ano anterior até hoje; substitui
//     tudo. Usa o valor em aberto (AmountControllingCurrency) e descarta itens eliminados e zerados — a confirmar com a
//     TI SAP (o documento cita o valor planejado como "provável" Valor/MR; somado ao realizado, ele contaria duas vezes).
// Uma resposta vazia nunca apaga dados (a API omite em silêncio as empresas sem autorização).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"fb_apu05/internal/controleti"
	"fb_apu05/internal/sapodata"
)

// trocáveis nos testes
var (
	lerRealizadoSAP     = sapodata.LerRealizado
	lerCompromissadoSAP = sapodata.LerCompromissado
)

// ccsDoControleTI: os CCs (código SAP, só dígitos) que o módulo acompanha.
func ccsDoControleTI(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT cc FROM (
			SELECT cc_sap AS cc FROM ti_lancamentos
			UNION SELECT cc_sap FROM ti_ksb1
			UNION SELECT cc_sap FROM ti_ksb2
		) x WHERE cc ~ '^[0-9]{4,10}$' ORDER BY cc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// realizadoParaKSB1: só as linhas com pedido entram (como na planilha).
func realizadoParaKSB1(api []sapodata.RealizadoLinha) []controleti.KSB1Linha {
	out := make([]controleti.KSB1Linha, 0, len(api))
	for _, l := range api {
		if strings.TrimSpace(l.PurchaseOrder) == "" || l.PostingDate == "" {
			continue
		}
		k := controleti.KSB1Linha{
			Pedido: strings.TrimSpace(l.PurchaseOrder), CCSap: strings.TrimSpace(l.CostCenter), Conta: strings.TrimSpace(l.CostElement),
			Fornecedor: strings.TrimSpace(l.SupplierName), CNPJ: somenteDigitosTI(l.SupplierCnpj), Texto: strings.TrimSpace(l.PurchaseOrderText),
			Valor:    float64(int64(l.AmountControllingCurrency*100+sinal(l.AmountControllingCurrency)*0.5)) / 100,
			DataLanc: l.PostingDate, DocRef: strings.TrimSpace(l.ReferenceDocument), Estornado: l.IsReversed,
		}
		if l.IsReversed {
			k.DocEstorno = l.ReversalReferenceDocument
		}
		if l.IsReversalDocument {
			k.RefEstorno = l.ReversalReferenceDocument
		}
		out = append(out, k)
	}
	return out
}

// compromissadoParaKSB2: o saldo em aberto; itens eliminados e zerados saem.
func compromissadoParaKSB2(api []sapodata.CompromissadoLinha) []controleti.KSB2Linha {
	out := make([]controleti.KSB2Linha, 0, len(api))
	for _, l := range api {
		if strings.TrimSpace(l.ReferenceDocument) == "" || strings.TrimSpace(l.DeletionIndicator) != "" || l.AmountControllingCurrency == 0 {
			continue
		}
		texto := strings.TrimSpace(l.MaterialText)
		if texto == "" {
			texto = strings.TrimSpace(l.ItemText)
		}
		out = append(out, controleti.KSB2Linha{
			Pedido: strings.TrimSpace(l.ReferenceDocument), Item: strings.TrimSpace(l.ReferenceItem), CCSap: strings.TrimSpace(l.CostCenter),
			Conta: strings.TrimSpace(l.CostElement), Fornecedor: strings.TrimSpace(l.SupplierName),
			Valor:   float64(int64(l.AmountControllingCurrency*100+sinal(l.AmountControllingCurrency)*0.5)) / 100,
			DataDoc: l.DocumentDate, Texto: texto,
		})
	}
	return out
}

func sinal(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func somenteDigitosTI(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cargaKSBdoSAP roda a carga de um dos dois serviços e devolve o resultado para a tela de Configurações.
func cargaKSBdoSAP(ctx context.Context, db *sql.DB, sv sapodata.Servico, cfg sapodata.Config, usuarioID string, agora time.Time) sapodata.Resultado {
	ini := time.Now()
	falha := func(msg string) sapodata.Resultado {
		return sapodata.Resultado{OK: false, Mensagem: "carga falhou: " + msg, DuracaoMs: time.Since(ini).Milliseconds()}
	}
	ccs, err := ccsDoControleTI(db)
	if err != nil {
		return falha("não foi possível ler os centros de custo do Controle de Despesas")
	}
	if len(ccs) == 0 {
		return falha("o Controle de Despesas ainda não tem centros de custo (importe a planilha de despesas primeiro)")
	}
	hoje := agora.In(locRecife)
	ate := time.Date(hoje.Year(), hoje.Month(), hoje.Day(), 0, 0, 0, 0, time.UTC)
	switch sv.Chave {
	case sapodata.Realizado.Chave:
		de := time.Date(hoje.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		api, err := lerRealizadoSAP(ctx, cfg, ccs, de, ate)
		if err != nil {
			return falha(err.Error())
		}
		linhas := realizadoParaKSB1(api)
		if len(linhas) == 0 {
			return falha(fmt.Sprintf("o SAP não devolveu nenhuma linha com pedido (%d lidas) — nada foi apagado", len(api)))
		}
		leitura := controleti.ResumoLeituraSAP{Linhas: len(api), Aproveitadas: len(linhas), SemPedido: len(api) - len(linhas),
			PrimeiroMes: de.Format("2006-01"), UltimoMes: ate.Format("2006-01")}
		if _, err := controleti.ImportarKSB1(db, linhas, leitura, usuarioID, fmt.Sprintf("API SAP KSB1 %s a %s", de.Format("02/01/2006"), ate.Format("02/01/2006"))); err != nil {
			log.Printf("[ControleTI] gravar KSB1 da API: %v", err)
			return falha("erro ao gravar")
		}
		return sapodata.Resultado{OK: true, Total: len(linhas), DuracaoMs: time.Since(ini).Milliseconds(),
			Mensagem: fmt.Sprintf("carga ok — %d lançamentos com pedido de %s a %s (%d CCs)", len(linhas), de.Format("01/2006"), ate.Format("01/2006"), len(ccs))}
	case sapodata.Compromissado.Chave:
		de := time.Date(hoje.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
		api, err := lerCompromissadoSAP(ctx, cfg, ccs, de, ate)
		if err != nil {
			return falha(err.Error())
		}
		linhas := compromissadoParaKSB2(api)
		if len(linhas) == 0 {
			return falha(fmt.Sprintf("o SAP não devolveu nenhum compromisso em aberto (%d lidas) — nada foi apagado", len(api)))
		}
		leitura := controleti.ResumoLeituraSAP{Linhas: len(api), Aproveitadas: len(linhas), Totais: 0}
		if _, err := controleti.ImportarKSB2(db, linhas, leitura, usuarioID, fmt.Sprintf("API SAP KSB2 (em aberto, lançados desde %s)", de.Format("02/01/2006"))); err != nil {
			log.Printf("[ControleTI] gravar KSB2 da API: %v", err)
			return falha("erro ao gravar")
		}
		return sapodata.Resultado{OK: true, Total: len(linhas), DuracaoMs: time.Since(ini).Milliseconds(),
			Mensagem: fmt.Sprintf("carga ok — %d itens de compromisso em aberto (%d CCs)", len(linhas), len(ccs))}
	}
	return falha("serviço desconhecido")
}

var atualizandoKSB sync.Mutex

// TIAtualizarSAPHandler — POST /api/ti/sap/atualizar ("Atualizar agora" no Controle de Despesas, só ADM do módulo):
// roda as cargas KSB1 e KSB2 dos serviços ativos em Configurações.
func TIAtualizarSAPHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usuario := GetUserIDFromContext(r)
		if nivel, err := nivelTI(db, usuario); err != nil || nivel != controleti.NivelADM {
			jsonErr(w, http.StatusForbidden, msgSemControleTI)
			return
		}
		if !atualizandoKSB.TryLock() {
			jsonErr(w, http.StatusConflict, "Já há uma atualização em andamento. Aguarde alguns minutos.")
			return
		}
		defer atualizandoKSB.Unlock()
		m, err := lerConfiguracoes(db)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		out := map[string]sapodata.Resultado{}
		for _, sv := range []sapodata.Servico{sapodata.Realizado, sapodata.Compromissado} {
			if m[cfgServico(sv, "ativo")] != "true" {
				out[sv.Chave] = sapodata.Resultado{OK: false, Mensagem: sv.Nome + " está desligado em Configurações → Integração SAP."}
				continue
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
			res := cargaKSBdoSAP(ctx, db, sv, credencialSAP(m, sv), usuario, time.Now())
			cancel()
			guardarResultado(db, sv, res, "manual", time.Now())
			log.Printf("[ControleTI] %s atualizou %s pela API: ok=%v (%s)", usuario, sv.Chave, res.OK, res.Mensagem)
			out[sv.Chave] = res
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ksb1": out[sapodata.Realizado.Chave], "ksb2": out[sapodata.Compromissado.Chave]})
	}
}
