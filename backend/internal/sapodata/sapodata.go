// Package sapodata lê dados mestre do SAP S/4HANA pelas APIs standard OData V2 (somente leitura):
//
//	cc  centros de custo  — API_COSTCENTER_SRV / A_CostCenter
//	    (docs/referencia/05-API-SAP-CENTRO-DE-CUSTO.md)
//	gl  contas contábeis  — API_GLACCOUNTINCHARTOFACCOUNTS_SRV / A_GLAccountInChartOfAccounts
//	    (docs/referencia/06-API-SAP-CONTAS-CONTABEIS.md)
//
// Hoje o pacote só faz a SONDA diária (GET <entidade>?$top=1&$inlinecount=allpages): confirma
// credencial, rede e permissão e conta os registros. A gravação em centros_custo/contas depende do
// mapeamento SAP ↔ cadastro (a definir) e ainda não é feita aqui.
//
// Segurança: só HTTPS, só domínios da Ferreira Costa, sem seguir redirecionamentos (a credencial
// Basic nunca vai para outro host), corpo limitado e erros sem credencial.
package sapodata

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Servico descreve uma API OData do SAP que o sistema lê.
type Servico struct {
	Chave    string // "cc" | "gl" (chave nas configurações e na tela)
	Nome     string // para mensagens
	Segmento string // nome do serviço no caminho da URL
	Entidade string // entity set listado na sonda
	Chaves   string // $select mínimo da sonda
}

// Serviços conhecidos.
var (
	CentrosCusto = Servico{Chave: "cc", Nome: "Centros de custo", Segmento: "API_COSTCENTER_SRV",
		Entidade: "A_CostCenter", Chaves: "ControllingArea,CostCenter"}
	ContasContabeis = Servico{Chave: "gl", Nome: "Contas contábeis", Segmento: "API_GLACCOUNTINCHARTOFACCOUNTS_SRV",
		Entidade: "A_GLAccountInChartOfAccounts", Chaves: "ChartOfAccounts,GLAccount"}
)

// Todos é a ordem em que a rotina diária processa os serviços.
var Todos = []Servico{CentrosCusto, ContasContabeis}

// PorChave devolve o serviço da chave ("cc"/"gl").
func PorChave(chave string) (Servico, bool) {
	for _, sv := range Todos {
		if sv.Chave == chave {
			return sv, true
		}
	}
	return Servico{}, false
}

// Config é o que a sonda precisa; Senha chega já em texto puro (decifrada por quem chama).
type Config struct {
	URLBase string
	Usuario string
	Senha   string
	// Modo gateway (Kong): preenchido por ConfigGateway a partir de SAP_API_KEY / SAP_CLIENT.
	// Com APIKey, a chamada vai com o header apikey (e ?sap-client=) em vez de Basic.
	APIKey    string
	SapClient string
}

// ModoGateway diz se o SAP é lido pelo gateway interno (Kong, header apikey) em vez de direto no
// SAP (Basic). Liga quando a variável SAP_API_KEY está definida no servidor.
func ModoGateway() bool { return strings.TrimSpace(os.Getenv("SAP_API_KEY")) != "" }

// DoGateway completa cfg com a chave e o mandante lidos do ambiente (só em modo gateway).
func (c Config) DoGateway() Config {
	if ModoGateway() {
		c.APIKey = strings.TrimSpace(os.Getenv("SAP_API_KEY"))
		c.SapClient = strings.TrimSpace(os.Getenv("SAP_CLIENT"))
	}
	return c
}

// Resultado da sonda.
type Resultado struct {
	OK         bool   `json:"ok"`
	Total      int    `json:"total"`      // centros de custo (d.__count)
	Mensagem   string `json:"mensagem"`   // texto para a tela
	DuracaoMs  int64  `json:"duracao_ms"` // tempo da chamada
	HTTPStatus int    `json:"http_status,omitempty"`
}

var sufixosPermitidos = []string{".ferreiracosta.com", ".ferreiracosta.com.br"}

// ValidarURLBase aceita só https://host[:porta]/caminho, sem usuário embutido, sem IP literal e com
// host sob um domínio da Ferreira Costa — a URL é digitada por um administrador e o servidor a chama
// (evita usar o sistema como ponte para outros destinos).
func ValidarURLBase(raw string, sv Servico) error {
	if ModoGateway() {
		return validarURLGateway(raw)
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("URL inválida")
	}
	if u.Scheme != "https" {
		return errors.New("a URL precisa começar com https://")
	}
	if u.User != nil {
		return errors.New("a URL não pode conter usuário e senha")
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil {
		return errors.New("use o nome do servidor, não o endereço IP")
	}
	ok := false
	for _, s := range sufixosPermitidos {
		if strings.HasSuffix(host, s) {
			ok = true
		}
	}
	if !ok {
		return errors.New("o servidor precisa ser de um domínio da Ferreira Costa (ferreiracosta.com ou ferreiracosta.com.br)")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("a URL não pode ter parâmetros (?…) nem âncora (#…)")
	}
	if !strings.Contains(strings.ToUpper(u.Path), strings.ToUpper(sv.Segmento)) {
		return errors.New("o caminho deve apontar para o serviço " + sv.Segmento)
	}
	return nil
}

// validarURLGateway aceita http(s)://host[:porta]/caminho do gateway interno: host sob um domínio da
// Ferreira Costa OU IP de rede privada (10.x, 172.16-31.x, 192.168.x). Nunca IP público nem usuário
// embutido, para o servidor não virar ponte para fora da rede.
func validarURLGateway(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("URL inválida")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("a URL precisa começar com http:// ou https://")
	}
	if u.User != nil {
		return errors.New("a URL não pode conter usuário e senha")
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsPrivate() {
			return errors.New("o gateway precisa estar na rede interna (IP privado)")
		}
	} else {
		ok := false
		for _, s := range sufixosPermitidos {
			if strings.HasSuffix(host, s) {
				ok = true
			}
		}
		if !ok {
			return errors.New("o gateway precisa ser de um domínio da Ferreira Costa (ferreiracosta.com ou ferreiracosta.com.br)")
		}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("a URL não pode ter parâmetros (?…) nem âncora (#…)")
	}
	if strings.Trim(u.Path, "/") == "" {
		return errors.New("a URL deve incluir a rota do serviço no gateway (ex.: /sap/qas/cost-center-read)")
	}
	return nil
}

// montarURL monta URL da coleção com as opções OData (e sap-client no modo gateway).
func montarURL(cfg Config, entidade string, q url.Values) string {
	if cfg.APIKey != "" && cfg.SapClient != "" {
		q.Set("sap-client", cfg.SapClient)
	}
	// %20 (não "+") entre os termos do $filter; o SAP interpreta de forma estrita
	enc := strings.ReplaceAll(q.Encode(), "+", "%20")
	return strings.TrimRight(strings.TrimSpace(cfg.URLBase), "/") + "/" + entidade + "?" + enc
}

// autenticar põe a credencial certa na requisição (apikey no gateway, Basic direto no SAP).
func autenticar(req *http.Request, cfg Config) {
	if cfg.APIKey != "" {
		req.Header.Set("apikey", cfg.APIKey)
	} else {
		req.SetBasicAuth(cfg.Usuario, cfg.Senha)
	}
	req.Header.Set("Accept", "application/json")
}

// Pontos de troca para teste (o servidor de teste não é de um domínio da FC e usa certificado próprio).
var (
	validarURL  = ValidarURLBase
	novoCliente = func() *http.Client {
		return &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: poolDeCAs(), MinVersion: tls.VersionTLS12}},
			// nunca segue redirecionamento: a credencial Basic não pode ir para outro lugar
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
)

// Sonda faz a chamada de leitura e devolve o resultado (nunca erro: tudo vira Resultado.Mensagem).
func Sonda(ctx context.Context, sv Servico, cfg Config) Resultado {
	ini := time.Now()
	res := func(ok bool, total int, status int, msg string) Resultado {
		return Resultado{OK: ok, Total: total, HTTPStatus: status, Mensagem: msg, DuracaoMs: time.Since(ini).Milliseconds()}
	}
	if err := validarURL(cfg.URLBase, sv); err != nil {
		return res(false, 0, 0, err.Error())
	}
	if cfg.APIKey == "" && (cfg.Usuario == "" || cfg.Senha == "") {
		return res(false, 0, 0, "usuário e senha do SAP ainda não foram informados")
	}

	endpoint := montarURL(cfg, sv.Entidade, url.Values{"$top": {"1"}, "$inlinecount": {"allpages"}, "$select": {sv.Chaves}})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return res(false, 0, 0, "não foi possível montar a requisição")
	}
	autenticar(req, cfg)

	resp, err := novoCliente().Do(req)
	if err != nil {
		return res(false, 0, 0, classificarErroRede(err))
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return res(false, 0, resp.StatusCode, "SAP recusou o usuário/senha (401)")
	case resp.StatusCode == http.StatusForbidden:
		return res(false, 0, resp.StatusCode, "usuário sem autorização para o serviço — falta o arranjo de comunicação (403)")
	case resp.StatusCode == http.StatusNotFound:
		return res(false, 0, resp.StatusCode, "serviço "+sv.Segmento+" não encontrado ou não ativado (404)")
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return res(false, 0, resp.StatusCode, fmt.Sprintf("SAP respondeu com redirecionamento (%d); confira a URL", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return res(false, 0, resp.StatusCode, fmt.Sprintf("SAP respondeu HTTP %d", resp.StatusCode))
	}

	var corpoJSON struct {
		D struct {
			Count json.Number `json:"__count"`
		} `json:"d"`
	}
	if err := json.Unmarshal(corpo, &corpoJSON); err != nil {
		return res(false, 0, resp.StatusCode, "resposta do SAP em formato inesperado (esperado JSON OData V2)")
	}
	total, err := strconv.Atoi(corpoJSON.D.Count.String())
	if err != nil {
		return res(false, 0, resp.StatusCode, "resposta do SAP sem o total de registros")
	}
	return res(true, total, resp.StatusCode, fmt.Sprintf("conexão ok — %d registros (%s) no SAP", total, strings.ToLower(sv.Nome)))
}

func classificarErroRede(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return "o servidor do sistema não consegue resolver o endereço do SAP (DNS) — falta liberar a rota/DNS até o SAP"
	case errors.Is(err, context.DeadlineExceeded):
		return "tempo esgotado ao falar com o SAP (20 s) — verifique a rota de rede/firewall"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "x509"):
		return "certificado do SAP não confiável para o servidor do sistema (certificado/CA interna)"
	case strings.Contains(msg, "connection refused"):
		return "conexão recusada pelo SAP (porta fechada ou serviço fora do ar)"
	case strings.Contains(msg, "timeout"):
		return "tempo esgotado ao falar com o SAP — verifique a rota de rede/firewall"
	}
	return "não foi possível conectar ao SAP"
}

// poolDeCAs devolve as CAs do sistema mais, se existir, o arquivo PEM de SAP_CA_FILE (cadeia da CA
// interna que emite o certificado do SAP). A validação do certificado nunca é desligada.
func poolDeCAs() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caminho := strings.TrimSpace(os.Getenv("SAP_CA_FILE")); caminho != "" {
		if pem, err := os.ReadFile(caminho); err == nil {
			pool.AppendCertsFromPEM(pem)
		}
	}
	return pool
}
