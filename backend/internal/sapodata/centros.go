package sapodata

// centros.go — Centros (Plant/Werk: filiais e locais físicos) do SAP, serviço OData V4 zsd_plant / Centros.
// Em produção desde 26/11/2026 pelo gateway (rota /sap/prd/centros, que já aponta para a coleção Centros —
// por isso a URL configurada é a da COLEÇÃO e nada é acrescentado ao caminho). Somente GET.
//
// Campos devolvidos: Centro, Nome, CNPJ, Cidade, Estado, GrupoContas, Empresa.

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

// Centros: a URL é a da coleção (gateway /sap/prd/centros ou direto .../zsd_plant/0001/Centros).
var Centros = Servico{Chave: "wk", Nome: "Centros (filiais)", Segmento: "zsd_plant", Entidade: "", Chaves: "Centro,Nome", V4: true, Diaria: true}

// Centro é um centro (Plant) do SAP, já normalizado.
type Centro struct {
	Codigo, Nome, CNPJ, Cidade, UF, GrupoContas, Empresa string
}

type centroLinha struct {
	Centro      string `json:"Centro"`
	Nome        string `json:"Nome"`
	CNPJ        string `json:"CNPJ"`
	Cidade      string `json:"Cidade"`
	Estado      string `json:"Estado"`
	GrupoContas string `json:"GrupoContas"`
	Empresa     string `json:"Empresa"`
}

const tamanhoPaginaCentros = 1000

// LerCentros devolve todos os centros do SAP (paginando com $top/$skip), um por código.
func LerCentros(ctx context.Context, cfg Config) ([]Centro, error) {
	if err := validarURL(cfg.URLBase, Centros, cfg.APIKey != ""); err != nil {
		return nil, err
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return nil, errors.New("credencial do SAP não informada")
	}
	vistos := map[string]bool{}
	var out []Centro
	for pagina := 0; pagina < maxPaginas; pagina++ {
		linhas, err := lerPaginaCentros(ctx, cfg, pagina*tamanhoPaginaCentros)
		if err != nil {
			return nil, err
		}
		for _, l := range linhas {
			c := Centro{Codigo: strings.TrimSpace(l.Centro), Nome: strings.TrimSpace(l.Nome), CNPJ: strings.TrimSpace(l.CNPJ),
				Cidade: strings.TrimSpace(l.Cidade), UF: strings.TrimSpace(l.Estado), GrupoContas: strings.TrimSpace(l.GrupoContas), Empresa: strings.TrimSpace(l.Empresa)}
			if c.Codigo == "" || vistos[c.Codigo] {
				continue
			}
			vistos[c.Codigo] = true
			out = append(out, c)
		}
		if len(linhas) < tamanhoPaginaCentros {
			return out, nil
		}
	}
	return nil, errors.New("o SAP devolveu mais páginas que o limite de segurança")
}

func lerPaginaCentros(ctx context.Context, cfg Config, pular int) ([]centroLinha, error) {
	q := url.Values{"$top": {strconv.Itoa(tamanhoPaginaCentros)}, "$skip": {strconv.Itoa(pular)}, "$orderby": {"Centro"}}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlColecao(cfg, q), nil)
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
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, errors.New("o gateway/SAP recusou a credencial (401)")
	case http.StatusForbidden:
		return nil, errors.New("sem autorização para o serviço de centros (403)")
	case http.StatusNotFound:
		return nil, errors.New("serviço de centros não encontrado (404) — confira a URL")
	default:
		return nil, fmt.Errorf("SAP respondeu HTTP %d ao ler os centros", resp.StatusCode)
	}
	var env struct {
		Value []centroLinha `json:"value"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		return nil, errors.New("resposta do SAP em formato inesperado (esperado JSON OData V4)")
	}
	return env.Value, nil
}

// urlColecao: a URL configurada JÁ é a da coleção. O mandante (sap-client) só vai se estiver informado no
// próprio serviço (Configurações); o geral/do servidor é o do QAS e não é usado aqui.
func urlColecao(cfg Config, q url.Values) string {
	if cfg.APIKey != "" && cfg.SapClient != "" {
		q.Set("sap-client", cfg.SapClient)
	}
	enc := strings.ReplaceAll(q.Encode(), "+", "%20")
	return strings.TrimRight(strings.TrimSpace(cfg.URLBase), "/") + "?" + enc
}

// sondaCentros lê a coleção inteira (é pequena) e conta os centros, sem gravar nada.
func sondaCentros(ctx context.Context, cfg Config) Resultado {
	ini := time.Now()
	cs, err := LerCentros(ctx, cfg)
	if err != nil {
		return Resultado{OK: false, Mensagem: err.Error(), DuracaoMs: time.Since(ini).Milliseconds()}
	}
	msg := fmt.Sprintf("conexão ok — %d centros no SAP", len(cs))
	if len(cs) > 0 {
		msg += fmt.Sprintf(" (ex.: %s — %s)", cs[0].Codigo, cs[0].Nome)
	}
	return Resultado{OK: true, Total: len(cs), Mensagem: msg, DuracaoMs: time.Since(ini).Milliseconds()}
}
