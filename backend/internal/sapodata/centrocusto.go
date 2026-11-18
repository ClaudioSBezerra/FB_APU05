package sapodata

// centrocusto.go — leitura completa e paginada dos centros de custo VIGENTES (A_CostCenter com
// to_Text), usada pela carga diária das 06:00. Somente GET.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CentroCusto é um centro de custo vigente do SAP, já normalizado.
type CentroCusto struct {
	Codigo           string // CostCenter
	Nome             string // descrição em PT (cai para o nome curto)
	AreaControladora string // ControllingArea
	Empresa          string // CompanyCode
	AreaNegocio      string // BusinessArea (candidata a divisão)
	Responsavel      string // CostCtrResponsiblePersonName
	VigenteDesde     time.Time
}

const (
	tamanhoPagina = 500
	maxPaginas    = 100 // trava de segurança: 50 mil centros de custo
)

var reDataOData = regexp.MustCompile(`^/Date\((-?\d+)(?:[+-]\d+)?\)/$`)

// ParseDataOData converte "/Date(ms)/" (OData V2) em time.Time UTC. Vazio/inválido → zero.
func ParseDataOData(s string) time.Time {
	m := reDataOData.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

type ccTexto struct {
	Language              string `json:"Language"`
	CostCenterName        string `json:"CostCenterName"`
	CostCenterDescription string `json:"CostCenterDescription"`
}

type ccLinha struct {
	ControllingArea              string `json:"ControllingArea"`
	CostCenter                   string `json:"CostCenter"`
	ValidityStartDate            string `json:"ValidityStartDate"`
	ValidityEndDate              string `json:"ValidityEndDate"`
	CompanyCode                  string `json:"CompanyCode"`
	BusinessArea                 string `json:"BusinessArea"`
	CostCtrResponsiblePersonName string `json:"CostCtrResponsiblePersonName"`
	ToText                       struct {
		Results []ccTexto `json:"results"`
	} `json:"to_Text"`
}

// nomeDoTexto escolhe o nome: descrição PT, senão nome PT, senão qualquer idioma, senão "".
func nomeDoTexto(ts []ccTexto) string {
	melhor := func(t ccTexto) string {
		if d := strings.TrimSpace(t.CostCenterDescription); d != "" {
			return d
		}
		return strings.TrimSpace(t.CostCenterName)
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

// LerCentrosCusto devolve os centros de custo vigentes em `hoje`, um por código (se houver períodos
// sobrepostos ou mais de uma área controladora, vale o de vigência iniciada mais recentemente).
func LerCentrosCusto(ctx context.Context, cfg Config, hoje time.Time) ([]CentroCusto, error) {
	if err := validarURL(cfg.URLBase, CentrosCusto, cfg.APIKey != ""); err != nil {
		return nil, err
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return nil, errors.New("credencial do SAP não informada")
	}
	dia := hoje.UTC().Format("2006-01-02") + "T00:00:00"
	filtro := fmt.Sprintf("ValidityEndDate ge datetime'%s' and ValidityStartDate le datetime'%s'", dia, dia)

	porCodigo := map[string]CentroCusto{}
	var ordem []string
	for pagina := 0; pagina < maxPaginas; pagina++ {
		linhas, err := lerPaginaCC(ctx, cfg, filtro, pagina*tamanhoPagina)
		if err != nil {
			return nil, err
		}
		for _, l := range linhas {
			codigo := strings.TrimSpace(l.CostCenter)
			if codigo == "" {
				continue
			}
			cc := CentroCusto{
				Codigo: codigo, Nome: nomeDoTexto(l.ToText.Results),
				AreaControladora: strings.TrimSpace(l.ControllingArea), Empresa: strings.TrimSpace(l.CompanyCode),
				AreaNegocio: strings.TrimSpace(l.BusinessArea), Responsavel: strings.TrimSpace(l.CostCtrResponsiblePersonName),
				VigenteDesde: ParseDataOData(l.ValidityStartDate),
			}
			if ant, ok := porCodigo[codigo]; ok {
				if !cc.VigenteDesde.After(ant.VigenteDesde) {
					continue
				}
			} else {
				ordem = append(ordem, codigo)
			}
			porCodigo[codigo] = cc
		}
		if len(linhas) < tamanhoPagina {
			out := make([]CentroCusto, 0, len(ordem))
			for _, c := range ordem {
				out = append(out, porCodigo[c])
			}
			return out, nil
		}
	}
	return nil, errors.New("o SAP devolveu mais páginas que o limite de segurança")
}

func lerPaginaCC(ctx context.Context, cfg Config, filtro string, pular int) ([]ccLinha, error) {
	q := url.Values{
		"$filter":  {filtro},
		"$select":  {"ControllingArea,CostCenter,ValidityStartDate,ValidityEndDate,CompanyCode,BusinessArea,CostCtrResponsiblePersonName,to_Text/Language,to_Text/CostCenterName,to_Text/CostCenterDescription"},
		"$expand":  {"to_Text"},
		"$orderby": {"CostCenter"},
		"$top":     {strconv.Itoa(tamanhoPagina)},
		"$skip":    {strconv.Itoa(pular)},
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, montarURL(cfg, CentrosCusto.Entidade, q), nil)
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
		return nil, fmt.Errorf("SAP respondeu HTTP %d ao ler os centros de custo", resp.StatusCode)
	}
	var env struct {
		D struct {
			Results []ccLinha `json:"results"`
		} `json:"d"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		return nil, errors.New("resposta do SAP em formato inesperado (esperado JSON OData V2)")
	}
	return env.D.Results, nil
}
