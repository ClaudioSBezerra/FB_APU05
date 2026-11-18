package sapodata

// contas.go — leitura completa e paginada das contas contábeis do plano de contas da Ferreira Costa
// (A_GLAccountInChartOfAccounts com to_Text), usada pela carga diária das 06:00. Somente GET.
//
// A API devolve TODOS os planos de contas do S/4HANA (no QAS: 33 planos, 19.842 contas — inclusive os
// planos-padrão de outros países). O plano da Ferreira Costa é o "0050" (10 dígitos; todas as contas
// reais que já estavam no cadastro existem só nele). Vem filtrado no servidor, sem contas marcadas
// para eliminação nem bloqueadas para lançamento.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PlanoContasPadrao é o plano de contas (ChartOfAccounts) da Ferreira Costa no SAP. Pode ser trocado
// na virada para PRD pela variável SAP_GL_CHART.
const PlanoContasPadrao = "0050"

var rePlano = regexp.MustCompile(`^[A-Za-z0-9]{1,4}$`)

// PlanoContas devolve o plano em uso (SAP_GL_CHART ou o padrão).
func PlanoContas() string {
	if p := strings.TrimSpace(os.Getenv("SAP_GL_CHART")); rePlano.MatchString(p) {
		return p
	}
	return PlanoContasPadrao
}

// ContaContabil é uma conta do plano, já normalizada.
type ContaContabil struct {
	Codigo     string // GLAccount
	Plano      string // ChartOfAccounts
	Nome       string // descrição em PT (cai para o nome curto)
	ResultadoP bool   // conta de resultado (despesa/receita); false = conta de balanço
}

type glTexto struct {
	Language          string `json:"Language"`
	GLAccountName     string `json:"GLAccountName"`
	GLAccountLongName string `json:"GLAccountLongName"`
}

type glLinha struct {
	ChartOfAccounts     string `json:"ChartOfAccounts"`
	GLAccount           string `json:"GLAccount"`
	IsProfitLossAccount bool   `json:"IsProfitLossAccount"`
	ToText              struct {
		Results []glTexto `json:"results"`
	} `json:"to_Text"`
}

func nomeDaConta(ts []glTexto) string {
	melhor := func(t glTexto) string {
		if n := strings.TrimSpace(t.GLAccountLongName); n != "" {
			return n
		}
		return strings.TrimSpace(t.GLAccountName)
	}
	for _, t := range ts {
		if strings.EqualFold(t.Language, "PT") {
			if n := melhor(t); n != "" {
				return n
			}
		}
	}
	for _, t := range ts {
		if n := melhor(t); n != "" {
			return n
		}
	}
	return ""
}

// LerContas devolve as contas ativas (não eliminadas, não bloqueadas para lançamento) do plano.
func LerContas(ctx context.Context, cfg Config, plano string) ([]ContaContabil, error) {
	if err := validarURL(cfg.URLBase, ContasContabeis, cfg.APIKey != ""); err != nil {
		return nil, err
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return nil, errors.New("credencial do SAP não informada")
	}
	if !rePlano.MatchString(plano) {
		return nil, errors.New("plano de contas inválido")
	}
	filtro := fmt.Sprintf("ChartOfAccounts eq '%s' and AccountIsMarkedForDeletion eq false and AccountIsBlockedForPosting eq false", plano)

	var out []ContaContabil
	vistos := map[string]bool{}
	for pagina := 0; pagina < maxPaginas; pagina++ {
		linhas, err := lerPaginaGL(ctx, cfg, filtro, pagina*tamanhoPagina)
		if err != nil {
			return nil, err
		}
		for _, l := range linhas {
			cod := strings.TrimSpace(l.GLAccount)
			if cod == "" || vistos[cod] {
				continue
			}
			vistos[cod] = true
			out = append(out, ContaContabil{Codigo: cod, Plano: strings.TrimSpace(l.ChartOfAccounts), Nome: nomeDaConta(l.ToText.Results), ResultadoP: l.IsProfitLossAccount})
		}
		if len(linhas) < tamanhoPagina {
			return out, nil
		}
	}
	return nil, errors.New("o SAP devolveu mais páginas que o limite de segurança")
}

func lerPaginaGL(ctx context.Context, cfg Config, filtro string, pular int) ([]glLinha, error) {
	q := url.Values{
		"$filter":  {filtro},
		"$select":  {"ChartOfAccounts,GLAccount,IsProfitLossAccount,to_Text/Language,to_Text/GLAccountName,to_Text/GLAccountLongName"},
		"$expand":  {"to_Text"},
		"$orderby": {"GLAccount"},
		"$top":     {strconv.Itoa(tamanhoPagina)},
		"$skip":    {strconv.Itoa(pular)},
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, montarURL(cfg, ContasContabeis.Entidade, q), nil)
	if err != nil {
		return nil, errors.New("não foi possível montar a requisição")
	}
	autenticar(req, cfg)
	resp, err := novoCliente().Do(req)
	if err != nil {
		return nil, errors.New(classificarErroRede(err))
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAP respondeu HTTP %d ao ler as contas contábeis", resp.StatusCode)
	}
	var env struct {
		D struct {
			Results []glLinha `json:"results"`
		} `json:"d"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		return nil, errors.New("resposta do SAP em formato inesperado (esperado JSON OData V2)")
	}
	return env.D.Results, nil
}
