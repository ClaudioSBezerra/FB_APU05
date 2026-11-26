package sapodata

// ksb.go — Realizado (KSB1, CostCenterActual) e Compromissado (KSB2, CostCenterCommitment) por centro de custo, APIs
// OData V4 da Maria Eduarda (TI SAP, nov/2026). Substituem as planilhas KSB1/KSB2 que o Controle de Despesas importa.
//
// Somente GET. A base tem dezenas de milhões de linhas: toda leitura vai com área de controle, faixa de data de
// lançamento e lista de centros de custo (em blocos), paginada de 1.000 em 1.000.
//
// A URL configurada pode ser a RAIZ do serviço (.../zsd_cc_actual/0001 — a entidade é acrescentada) ou já a da
// COLEÇÃO (rota do gateway que aponta para .../CostCenterActual — nada é acrescentado).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	Realizado = Servico{Chave: "k1", Nome: "Realizado por CC (KSB1)", Segmento: "zsd_cc_actual", Entidade: "CostCenterActual",
		Chaves: "CoDocumentNumber", V4: true, Diaria: true}
	Compromissado = Servico{Chave: "k2", Nome: "Compromissado por CC (KSB2)", Segmento: "zsd_cc_commitment", Entidade: "CostCenterCommitment",
		Chaves: "ReferenceDocument", V4: true, Diaria: true}
)

const (
	paginaKSB  = 1000
	ccsPorLote = 40 // tamanho da lista do "CostCenter in (...)" por chamada
	maxPagKSB  = 500
)

// RealizadoLinha: os campos da KSB1 que o Controle de Despesas usa.
type RealizadoLinha struct {
	PurchaseOrder             string  `json:"PurchaseOrder"`
	CostCenter                string  `json:"CostCenter"`
	CostElement               string  `json:"CostElement"`
	SupplierName              string  `json:"SupplierName"`
	SupplierCnpj              string  `json:"SupplierCnpj"`
	PurchaseOrderText         string  `json:"PurchaseOrderText"`
	AmountControllingCurrency float64 `json:"AmountControllingCurrency"`
	PostingDate               string  `json:"PostingDate"`
	ReferenceDocument         string  `json:"ReferenceDocument"`
	IsReversalDocument        bool    `json:"IsReversalDocument"`
	IsReversed                bool    `json:"IsReversed"`
	ReversalReferenceDocument string  `json:"ReversalReferenceDocument"`
}

// CompromissadoLinha: os campos da KSB2 que o Controle de Despesas usa.
type CompromissadoLinha struct {
	ReferenceDocument         string  `json:"ReferenceDocument"`
	ReferenceItem             string  `json:"ReferenceItem"`
	CostCenter                string  `json:"CostCenter"`
	CostElement               string  `json:"CostElement"`
	SupplierName              string  `json:"SupplierName"`
	DocumentDate              string  `json:"DocumentDate"`
	MaterialText              string  `json:"MaterialText"`
	ItemText                  string  `json:"ItemText"`
	DeletionIndicator         string  `json:"DeletionIndicator"`
	PlannedAmountCtrlCurrency float64 `json:"PlannedAmountCtrlCurrency"`
	AmountControllingCurrency float64 `json:"AmountControllingCurrency"`
}

const (
	selectRealizado     = "PurchaseOrder,CostCenter,CostElement,SupplierName,SupplierCnpj,PurchaseOrderText,AmountControllingCurrency,PostingDate,ReferenceDocument,IsReversalDocument,IsReversed,ReversalReferenceDocument"
	selectCompromissado = "ReferenceDocument,ReferenceItem,CostCenter,CostElement,SupplierName,DocumentDate,MaterialText,ItemText,DeletionIndicator,PlannedAmountCtrlCurrency,AmountControllingCurrency"
)

// urlKSB: a coleção (acrescenta a entidade só quando a URL é a raiz do serviço).
func urlKSB(cfg Config, sv Servico, q url.Values) string {
	if cfg.APIKey != "" && cfg.SapClient != "" {
		q.Set("sap-client", cfg.SapClient)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.URLBase), "/")
	if !strings.HasSuffix(strings.ToLower(base), strings.ToLower(sv.Entidade)) {
		base += "/" + sv.Entidade
	}
	return base + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

func filtroKSB(campoData string, ccs []string, de, ate time.Time) string {
	aspas := make([]string, 0, len(ccs))
	for _, c := range ccs {
		aspas = append(aspas, "'"+strings.ReplaceAll(strings.TrimLeft(c, "0"), "'", "")+"'")
	}
	f := fmt.Sprintf("ControllingArea eq '%s' and %s ge %s and %s le %s", AreaControladoria(), campoData, de.Format("2006-01-02"), campoData, ate.Format("2006-01-02"))
	if len(aspas) > 0 {
		f += " and CostCenter in (" + strings.Join(aspas, ",") + ")"
	}
	return f
}

// getPaginaKSB faz uma chamada e decodifica o `value`.
func getPaginaKSB(ctx context.Context, cfg Config, sv Servico, q url.Values, destino any) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlKSB(cfg, sv, q), nil)
	if err != nil {
		return errors.New("não foi possível montar a requisição")
	}
	autenticar(req, cfg)
	resp, err := novoCliente().Do(req)
	if err != nil {
		return errors.New(classificarErroRede(err))
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return errors.New("o gateway/SAP recusou a credencial (401)")
	case http.StatusForbidden:
		return errors.New("sem autorização para o serviço (403)")
	case http.StatusNotFound:
		return fmt.Errorf("serviço %s não encontrado (404) — confira a URL", sv.Segmento)
	case http.StatusBadRequest:
		return fmt.Errorf("o SAP recusou o filtro (400): %s", resumoErroOData(corpo))
	default:
		return fmt.Errorf("SAP respondeu HTTP %d (%s)", resp.StatusCode, resumoErroOData(corpo))
	}
	env := struct {
		Value json.RawMessage `json:"value"`
	}{}
	if err := json.Unmarshal(corpo, &env); err != nil || len(env.Value) == 0 {
		return errors.New("resposta do SAP em formato inesperado (esperado JSON OData V4)")
	}
	if err := json.Unmarshal(env.Value, destino); err != nil {
		return errors.New("resposta do SAP com campos em formato inesperado")
	}
	return nil
}

func resumoErroOData(corpo []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(corpo, &e) == nil && e.Error.Message != "" {
		m := e.Error.Message
		if len(m) > 200 {
			m = m[:200]
		}
		return m
	}
	return "sem detalhe"
}

func credOK(cfg Config, sv Servico) error {
	if err := validarURL(cfg.URLBase, sv, cfg.APIKey != ""); err != nil {
		return err
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return errors.New("credencial do SAP não informada")
	}
	return nil
}

// lerKSB pagina os lotes de CCs.
func lerKSB[T any](ctx context.Context, cfg Config, sv Servico, sel, campoData string, ccs []string, de, ate time.Time) ([]T, error) {
	if err := credOK(cfg, sv); err != nil {
		return nil, err
	}
	var out []T
	lotes := [][]string{nil}
	if len(ccs) > 0 {
		lotes = nil
		for i := 0; i < len(ccs); i += ccsPorLote {
			lotes = append(lotes, ccs[i:min(i+ccsPorLote, len(ccs))])
		}
	}
	for _, lote := range lotes {
		filtro := filtroKSB(campoData, lote, de, ate)
		for pag := 0; pag < maxPagKSB; pag++ {
			var linhas []T
			q := url.Values{"$filter": {filtro}, "$select": {sel}, "$top": {strconv.Itoa(paginaKSB)}, "$skip": {strconv.Itoa(pag * paginaKSB)}}
			if err := getPaginaKSB(ctx, cfg, sv, q, &linhas); err != nil {
				return nil, err
			}
			out = append(out, linhas...)
			if len(linhas) < paginaKSB {
				break
			}
			if pag == maxPagKSB-1 {
				return nil, errors.New("o SAP devolveu mais páginas que o limite de segurança")
			}
		}
	}
	return out, nil
}

// LerRealizado: KSB1 dos CCs, com data de lançamento entre de e ate.
func LerRealizado(ctx context.Context, cfg Config, ccs []string, de, ate time.Time) ([]RealizadoLinha, error) {
	return lerKSB[RealizadoLinha](ctx, cfg, Realizado, selectRealizado, "PostingDate", ccs, de, ate)
}

// LerCompromissado: KSB2 dos CCs, com data de lançamento entre de e ate.
func LerCompromissado(ctx context.Context, cfg Config, ccs []string, de, ate time.Time) ([]CompromissadoLinha, error) {
	return lerKSB[CompromissadoLinha](ctx, cfg, Compromissado, selectCompromissado, "PostingDate", ccs, de, ate)
}

// sondaKSB: uma página pequena do mês corrente, sem filtro de CC, para conferir acesso e formato.
func sondaKSB(ctx context.Context, sv Servico, cfg Config) Resultado {
	ini := time.Now()
	res := func(ok bool, total int, msg string) Resultado {
		return Resultado{OK: ok, Total: total, Mensagem: msg, DuracaoMs: time.Since(ini).Milliseconds()}
	}
	if err := credOK(cfg, sv); err != nil {
		return res(false, 0, err.Error())
	}
	hoje := time.Now()
	de := time.Date(hoje.Year(), hoje.Month(), 1, 0, 0, 0, 0, time.UTC)
	q := url.Values{"$filter": {filtroKSB("PostingDate", nil, de, hoje)}, "$select": {sv.Chaves}, "$top": {"5"}}
	var linhas []map[string]any
	if err := getPaginaKSB(ctx, cfg, sv, q, &linhas); err != nil {
		return res(false, 0, err.Error())
	}
	return res(true, len(linhas), fmt.Sprintf("conexão ok — o serviço %s respondeu (%d linha(s) de amostra do mês)", sv.Segmento, len(linhas)))
}
