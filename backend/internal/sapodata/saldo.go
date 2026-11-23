package sapodata

// saldo.go — consulta do saldo orçamentário (API de Trava orçamentária, OData V4), SOB DEMANDA.
// docs/referencia/07-API-SAP-TRAVA-ORCAMENTARIA.md. Somente GET, sempre com os TRÊS campos no $filter
// (sem eles o SAP agrega toda a COSP e dá timeout).

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

// Saldo é o resumo mensal de um (objeto CO, conta, exercício). Índice 0 = janeiro … 11 = dezembro.
type Saldo struct {
	Plan, Real, Comp, Disposto, Disponivel, DispAcum [12]float64
	Encontrado                                       bool // false = o SAP respondeu, mas não há lançamentos para a combinação
}

var (
	reObjnr = regexp.MustCompile(`^KS[A-Z0-9]{4}[0-9]{10}$`)
	reKstar = regexp.MustCompile(`^[0-9]{10}$`)
	reGjahr = regexp.MustCompile(`^20[0-9]{2}$`)
	reCC    = regexp.MustCompile(`^[0-9]{1,10}$`)
)

// AreaControladoria é a área de controladoria dos objetos CO (FC01 nos centros de custo do QAS); SAP_CONTROLLING_AREA troca.
func AreaControladoria() string {
	if a := strings.ToUpper(strings.TrimSpace(os.Getenv("SAP_CONTROLLING_AREA"))); regexp.MustCompile(`^[A-Z0-9]{4}$`).MatchString(a) {
		return a
	}
	return "FC01"
}

// ObjnrCentroCusto monta o objnr de um centro de custo: "KS" + área de controladoria + código com 10 posições.
// ex.: 10026040 → KSFC010010026040. Código que não é só dígito (ou passa de 10) não vira objnr.
func ObjnrCentroCusto(codigo string) (string, error) {
	c := strings.TrimSpace(codigo)
	if !reCC.MatchString(c) {
		return "", errors.New("o código do centro de custo não tem o formato do SAP (só dígitos, até 10)")
	}
	return "KS" + AreaControladoria() + strings.Repeat("0", 10-len(c)) + c, nil
}

// VersaoPlano é a versão do plano de custos (COSP.VERSN) que vale para o saldo (env SAP_PLAN_VERSION, padrão "000").
func VersaoPlano() string {
	if v := strings.TrimSpace(os.Getenv("SAP_PLAN_VERSION")); v != "" {
		return v
	}
	return "000"
}

// ConsultarSaldo lê o saldo de um (objnr, kstar, gjahr). Valida as três chaves antes de montar o $filter.
func ConsultarSaldo(ctx context.Context, cfg Config, objnr, kstar, gjahr string) (*Saldo, error) {
	if !reObjnr.MatchString(objnr) || !reKstar.MatchString(kstar) || !reGjahr.MatchString(gjahr) {
		return nil, errors.New("chave de consulta inválida")
	}
	if err := validarURL(cfg.URLBase, SaldoOrcamentario, cfg.APIKey != ""); err != nil {
		return nil, err
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return nil, errors.New("credencial do SAP não informada")
	}
	q := url.Values{"$filter": {fmt.Sprintf("objnr eq '%s' and kstar eq '%s' and gjahr eq '%s'", objnr, kstar, gjahr)}}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, montarURL(cfg, SaldoOrcamentario.Entidade, q), nil)
	if err != nil {
		return nil, errors.New("não foi possível montar a requisição")
	}
	autenticar(req, cfg)
	resp, err := novoCliente().Do(req)
	if err != nil {
		return nil, errors.New(classificarErroRede(err))
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("SAP recusou a credencial (401)")
	case resp.StatusCode == http.StatusForbidden:
		return nil, errors.New("usuário sem autorização para o serviço de saldo (403)")
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("serviço de saldo não encontrado ou inativo (404)")
	case resp.StatusCode == http.StatusGatewayTimeout:
		return nil, errors.New("o SAP demorou demais para responder (504)")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("SAP respondeu HTTP %d", resp.StatusCode)
	}
	return lerSaldoV4(corpo)
}

func lerSaldoV4(corpo []byte) (*Saldo, error) {
	var env struct {
		Value []map[string]json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		return nil, errors.New("resposta do SAP em formato inesperado (esperado JSON OData V4)")
	}
	s := &Saldo{}
	// A API agrupa também por VERSN (versão do plano): vale só a versão configurada; somar versões
	// duplicaria o planejado. Linhas sem o campo versn (API antiga) são aceitas como estão.
	versao := VersaoPlano()
	var linhas []map[string]json.RawMessage
	for _, linha := range env.Value {
		if raw, ok := linha["versn"]; ok {
			if strings.Trim(strings.TrimSpace(string(raw)), `"`) != versao {
				continue
			}
		}
		linhas = append(linhas, linha)
	}
	if len(linhas) == 0 {
		return s, nil
	}
	s.Encontrado = true
	for _, linha := range linhas {
		for mes := 1; mes <= 12; mes++ {
			for nome, dest := range map[string]*[12]float64{"plan": &s.Plan, "real": &s.Real, "comp": &s.Comp,
				"disposto": &s.Disposto, "disponivel": &s.Disponivel, "disp_acum": &s.DispAcum} {
				if raw, ok := linha[nome+strconv.Itoa(mes)]; ok {
					v, err := numeroV4(raw)
					if err != nil {
						return nil, fmt.Errorf("valor inválido em %s%d", nome, mes)
					}
					dest[mes-1] += v
				}
			}
		}
	}
	return s, nil
}

// numeroV4 aceita número JSON ou texto numérico (o OData V4 do SAP pode serializar Edm.Decimal como string).
func numeroV4(raw json.RawMessage) (float64, error) {
	t := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if t == "" || t == "null" {
		return 0, nil
	}
	return strconv.ParseFloat(t, 64)
}

// sondaSaldo confere credencial, rede e permissão com uma consulta de chave inexistente (devolve value vazio).
func sondaSaldo(ctx context.Context, sv Servico, cfg Config) Resultado {
	ini := time.Now()
	r := func(ok bool, msg string) Resultado {
		return Resultado{OK: ok, Mensagem: msg, DuracaoMs: time.Since(ini).Milliseconds()}
	}
	_, err := ConsultarSaldo(ctx, cfg, "KS"+AreaControladoria()+"0000000000", "0000000000", strconv.Itoa(time.Now().Year()))
	if err != nil {
		return r(false, err.Error())
	}
	return r(true, "conexão ok — o serviço de saldo respondeu à consulta de teste")
}
