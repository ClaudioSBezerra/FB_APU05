package handlers

// configuracoes.go — tela "Configurações" (17/11/2026), só para administradores.
//
//	GET  /api/admin/configuracoes                       — lê (a senha do SAP nunca volta)
//	PUT  /api/admin/configuracoes                       — grava (parcial)
//	POST /api/admin/configuracoes/sap/{servico}/testar  — sonda o SAP agora (servico = cc | gl)
//
// Integração SAP:
//   - integração SAP, lida 1x/dia (padrão 06:00, horário de Recife): credencial Basic única e, por
//     serviço (cc = centros de custo, gl = contas contábeis), URL e liga/desliga.
//
// A rotina diária (RotinaSAPDiaria) lê o mesmo cadastro. Centros de custo (cc), em modo gateway
// (SAP_API_KEY), são CARREGADOS em centros_custo; os demais serviços ainda só conferem o acesso e contam os
// registros (internal/sapodata). Gravar em centros_custo/contas depende do mapeamento SAP ↔ cadastro.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"fb_apu05/crypto"
	"fb_apu05/internal/sapodata"
)

const (
	cfgSapHorario = "sap_horario"
	cfgSapUsuario = "sap_usuario"
	cfgSapSenha   = "sap_senha"
	// gateway interno (Kong): chave (criptografada) e mandante informados na tela; a variável de
	// ambiente SAP_API_KEY/SAP_CLIENT vale quando a tela não tem a chave.
	cfgSapApiKey = "sap_apikey"
	cfgSapClient = "sap_client"
)

var clientRegexp = regexp.MustCompile(`^\d{1,3}$`)

var horarioRegexp = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func cfgServico(sv sapodata.Servico, campo string) string { return "sap_" + sv.Chave + "_" + campo }

// lerConfiguracoes devolve todas as chaves de configuracoes.
func lerConfiguracoes(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT chave, valor FROM configuracoes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

type resultadoSalvo struct {
	OK        bool   `json:"ok"`
	Total     int    `json:"total"`
	Mensagem  string `json:"mensagem"`
	DuracaoMs int64  `json:"duracao_ms"`
	Origem    string `json:"origem"` // "agendada" | "manual"
}

type servicoConfig struct {
	Chave           string          `json:"chave"`
	Nome            string          `json:"nome"`
	Ativo           bool            `json:"ativo"`
	URLBase         string          `json:"url_base"`
	ApiKeyDefinida  bool            `json:"apikey_definida"` // apikey própria deste serviço
	UltimaExecucao  string          `json:"ultima_execucao"`
	UltimoResultado *resultadoSalvo `json:"ultimo_resultado,omitempty"`
}

type configuracoesResposta struct {
	SAP struct {
		Horario       string `json:"horario"`
		Usuario       string `json:"usuario"`
		SenhaDefinida bool   `json:"senha_definida"`
		// gateway interno (Kong)
		ApiKeyDefinida bool            `json:"apikey_definida"`
		ApiKeyAmbiente bool            `json:"apikey_ambiente"` // a chave vem da variável de ambiente do servidor
		SapClient      string          `json:"sap_client"`
		Servicos       []servicoConfig `json:"servicos"`
	} `json:"sap"`
}

func montarResposta(m map[string]string) configuracoesResposta {
	var r configuracoesResposta
	r.SAP.Horario = m[cfgSapHorario]
	r.SAP.Usuario = m[cfgSapUsuario]
	r.SAP.SenhaDefinida = m[cfgSapSenha] != ""
	r.SAP.ApiKeyDefinida = m[cfgSapApiKey] != ""
	r.SAP.ApiKeyAmbiente = sapodata.ModoGateway()
	r.SAP.SapClient = m[cfgSapClient]
	for _, sv := range sapodata.Todos {
		sc := servicoConfig{
			Chave: sv.Chave, Nome: sv.Nome,
			Ativo:          m[cfgServico(sv, "ativo")] == "true",
			URLBase:        m[cfgServico(sv, "url_base")],
			ApiKeyDefinida: m[cfgServico(sv, "apikey")] != "",
			UltimaExecucao: m[cfgServico(sv, "ultima_execucao")],
		}
		if raw := m[cfgServico(sv, "ultimo_resultado")]; raw != "" {
			var rs resultadoSalvo
			if json.Unmarshal([]byte(raw), &rs) == nil {
				sc.UltimoResultado = &rs
			}
		}
		r.SAP.Servicos = append(r.SAP.Servicos, sc)
	}
	return r
}

// ConfiguracoesHandler — GET/PUT /api/admin/configuracoes.
func ConfiguracoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			m, err := lerConfiguracoes(db)
			if err != nil {
				log.Printf("[Config] Erro ao ler configurações: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(montarResposta(m))
		case http.MethodPut:
			salvarConfiguracoes(db, w, r)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

type sapEntrada struct {
	Horario      *string                   `json:"horario"`
	Usuario      *string                   `json:"usuario"`
	Senha        *string                   `json:"senha"`
	LimparSenha  bool                      `json:"limpar_senha"`
	ApiKey       *string                   `json:"apikey"`
	LimparApiKey bool                      `json:"limpar_apikey"`
	SapClient    *string                   `json:"sap_client"`
	Servicos     map[string]servicoEntrada `json:"servicos"`
}

// servicoEntrada: ajustes de um serviço (cc, gl). Cada API do gateway pode ter a SUA apikey; sem ela
// vale a apikey geral.
type servicoEntrada struct {
	Ativo        *bool   `json:"ativo"`
	URLBase      *string `json:"url_base"`
	ApiKey       *string `json:"apikey"`
	LimparApiKey bool    `json:"limpar_apikey"`
}

type configuracoesEntrada struct {
	SAP *sapEntrada `json:"sap"`
}

// validarEntrada transforma a entrada em pares chave→valor a gravar (a senha já cifrada), ou erro de validação.
func validarEntrada(in configuracoesEntrada, atual map[string]string) (map[string]string, error) {
	grava := map[string]string{}
	if in.SAP == nil {
		return grava, nil
	}
	s := in.SAP
	if s.Horario != nil {
		h := strings.TrimSpace(*s.Horario)
		if !horarioRegexp.MatchString(h) {
			return nil, errors.New("horário inválido: use HH:MM (00:00 a 23:59)")
		}
		grava[cfgSapHorario] = h
	}
	if s.Usuario != nil {
		u := strings.TrimSpace(*s.Usuario)
		if u != "" {
			var err error
			if u, err = NomeSeguro("usuário", u, 80); err != nil {
				return nil, err
			}
		}
		grava[cfgSapUsuario] = u
	}
	switch {
	case s.LimparSenha:
		grava[cfgSapSenha] = ""
	case s.Senha != nil && *s.Senha != "":
		if len(*s.Senha) > 200 || strings.ContainsAny(*s.Senha, "\r\n\x00") {
			return nil, errors.New("senha inválida")
		}
		cifrada, err := crypto.EncryptField(*s.Senha)
		if err != nil {
			return nil, errors.New("não foi possível proteger a senha")
		}
		grava[cfgSapSenha] = cifrada
	}
	switch {
	case s.LimparApiKey:
		grava[cfgSapApiKey] = ""
	case s.ApiKey != nil && *s.ApiKey != "":
		k := strings.TrimSpace(*s.ApiKey)
		if len(k) < 8 || len(k) > 200 || strings.ContainsAny(k, " \r\n\x00\t") {
			return nil, errors.New("apikey inválida")
		}
		cifrada, err := crypto.EncryptField(k)
		if err != nil {
			return nil, errors.New("não foi possível proteger a apikey")
		}
		grava[cfgSapApiKey] = cifrada
	}
	if s.SapClient != nil {
		c := strings.TrimSpace(*s.SapClient)
		if c != "" && !clientRegexp.MatchString(c) {
			return nil, errors.New("sap-client inválido: use só números (ex.: 300)")
		}
		grava[cfgSapClient] = c
	}
	// estado final da credencial (para exigir antes de ligar um serviço)
	apikeyFinal := atual[cfgSapApiKey] != "" || sapodata.ModoGateway()
	if v, ok := grava[cfgSapApiKey]; ok {
		apikeyFinal = v != "" || sapodata.ModoGateway()
	}
	usuarioFinal, senhaFinal := atual[cfgSapUsuario], atual[cfgSapSenha] != ""
	if v, ok := grava[cfgSapUsuario]; ok {
		usuarioFinal = v
	}
	if v, ok := grava[cfgSapSenha]; ok {
		senhaFinal = v != ""
	}
	for chave, sc := range s.Servicos {
		sv, ok := sapodata.PorChave(chave)
		if !ok {
			return nil, errors.New("serviço desconhecido: " + SanitizarNome(chave, 10))
		}
		// apikey própria do serviço (cada API do gateway pode ter a sua)
		svKeyFinal := atual[cfgServico(sv, "apikey")] != ""
		switch {
		case sc.LimparApiKey:
			grava[cfgServico(sv, "apikey")] = ""
			svKeyFinal = false
		case sc.ApiKey != nil && *sc.ApiKey != "":
			k := strings.TrimSpace(*sc.ApiKey)
			if len(k) < 8 || len(k) > 200 || strings.ContainsAny(k, " \r\n\x00\t") {
				return nil, errors.New(sv.Nome + ": apikey inválida")
			}
			cifrada, err := crypto.EncryptField(k)
			if err != nil {
				return nil, errors.New("não foi possível proteger a apikey")
			}
			grava[cfgServico(sv, "apikey")] = cifrada
			svKeyFinal = true
		}
		gw := apikeyFinal || svKeyFinal
		urlFinal := atual[cfgServico(sv, "url_base")]
		if sc.URLBase != nil {
			urlFinal = strings.TrimSpace(*sc.URLBase)
			if err := sapodata.ValidarURL(urlFinal, sv, gw); err != nil {
				return nil, errors.New(sv.Nome + ": " + err.Error())
			}
			grava[cfgServico(sv, "url_base")] = urlFinal
		}
		if sc.Ativo != nil {
			if *sc.Ativo {
				if !gw && (usuarioFinal == "" || !senhaFinal) {
					return nil, errors.New(sv.Nome + ": informe a apikey do gateway (ou o usuário e a senha do SAP) antes de ativar")
				}
				if err := sapodata.ValidarURL(urlFinal, sv, gw); err != nil {
					return nil, errors.New(sv.Nome + ": " + err.Error())
				}
			}
			grava[cfgServico(sv, "ativo")] = map[bool]string{true: "true", false: "false"}[*sc.Ativo]
		}
	}
	return grava, nil
}

func salvarConfiguracoes(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var in configuracoesEntrada
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		jsonErr(w, http.StatusBadRequest, "Corpo inválido")
		return
	}
	atual, err := lerConfiguracoes(db)
	if err != nil {
		log.Printf("[Config] Erro ao ler configurações: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	grava, err := validarEntrada(in, atual)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	atorID := GetUserIDFromContext(r)
	tx, err := db.Begin()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	defer func() { _ = tx.Rollback() }()
	chaves := make([]string, 0, len(grava))
	for k, v := range grava {
		if _, err := tx.Exec(`
			INSERT INTO configuracoes (chave, valor, updated_at, updated_by) VALUES ($1, $2, now(), $3)
			ON CONFLICT (chave) DO UPDATE SET valor = EXCLUDED.valor, updated_at = now(), updated_by = EXCLUDED.updated_by`,
			k, v, atorID); err != nil {
			log.Printf("[Config] Erro ao gravar %s: %v", k, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		chaves = append(chaves, k)
	}
	if err := tx.Commit(); err != nil {
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	// só os NOMES das chaves vão para o log (nunca valores: há senha)
	log.Printf("[Config] %s alterou: %s", atorID, strings.Join(chaves, ", "))

	m, err := lerConfiguracoes(db)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(montarResposta(m))
}

// ---- sonda do SAP ----

var (
	sondaMu        sync.Mutex
	sondaUltimoUso = map[string]time.Time{}
	// trocável nos testes
	sondaSAP = sapodata.Sonda
)

func credencialSAP(m map[string]string, sv sapodata.Servico) sapodata.Config {
	senha := ""
	if cifrada := m[cfgSapSenha]; cifrada != "" {
		if txt, err := crypto.DecryptField(cifrada); err == nil {
			senha = txt
		} else {
			log.Printf("[SAP] Erro ao decifrar a senha do SAP: %v", err)
		}
	}
	apikey := ""
	cifrada := m[cfgServico(sv, "apikey")] // a do serviço vale mais que a geral
	if cifrada == "" {
		cifrada = m[cfgSapApiKey]
	}
	if cifrada != "" {
		if txt, err := crypto.DecryptField(cifrada); err == nil {
			apikey = txt
		} else {
			log.Printf("[SAP] Erro ao decifrar a apikey do gateway: %v", err)
		}
	}
	return sapodata.Config{URLBase: m[cfgServico(sv, "url_base")], Usuario: m[cfgSapUsuario], Senha: senha,
		APIKey: apikey, SapClient: m[cfgSapClient]}.DoGateway()
}

func guardarResultado(db *sql.DB, sv sapodata.Servico, res sapodata.Resultado, origem string, quando time.Time) {
	raw, _ := json.Marshal(resultadoSalvo{OK: res.OK, Total: res.Total, Mensagem: res.Mensagem, DuracaoMs: res.DuracaoMs, Origem: origem})
	for chave, valor := range map[string]string{
		cfgServico(sv, "ultima_execucao"):  quando.UTC().Format(time.RFC3339),
		cfgServico(sv, "ultimo_resultado"): string(raw),
	} {
		if _, err := db.Exec(`UPDATE configuracoes SET valor = $2, updated_at = now() WHERE chave = $1`, chave, valor); err != nil {
			log.Printf("[SAP] Erro ao guardar %s: %v", chave, err)
		}
	}
}

// SAPTestarHandler — POST /api/admin/configuracoes/sap/{servico}/testar.
func SAPTestarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sv, ok := sapodata.PorChave(r.PathValue("servico"))
		if !ok {
			jsonErr(w, http.StatusNotFound, "Serviço desconhecido")
			return
		}
		sondaMu.Lock()
		if t, usado := sondaUltimoUso[sv.Chave]; usado && time.Since(t) < 5*time.Second {
			sondaMu.Unlock()
			jsonErr(w, http.StatusTooManyRequests, "Aguarde alguns segundos antes de testar de novo")
			return
		}
		sondaUltimoUso[sv.Chave] = time.Now()
		sondaMu.Unlock()

		m, err := lerConfiguracoes(db)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		res := sondaSAP(r.Context(), sv, credencialSAP(m, sv))
		guardarResultado(db, sv, res, "manual", time.Now())
		log.Printf("[SAP] Teste manual de %s por %s: ok=%v http=%d", sv.Chave, GetUserIDFromContext(r), res.OK, res.HTTPStatus)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}

// ---- rotina diária ----

// cargaCC roda a carga de centros de custo e a transforma em Resultado para a tela de Configurações.
func cargaCC(ctx context.Context, db *sql.DB, cfg sapodata.Config, agora time.Time) sapodata.Resultado {
	ini := time.Now()
	r, err := carregarCentrosCustoSAP(ctx, db, cfg, agora)
	res := sapodata.Resultado{OK: err == nil, Total: r.Lidos, DuracaoMs: time.Since(ini).Milliseconds()}
	if err != nil {
		res.Mensagem = "carga falhou: " + err.Error()
	} else {
		res.Mensagem = r.mensagem()
	}
	return res
}

var fusoRecife = func() *time.Location {
	if loc, err := time.LoadLocation("America/Recife"); err == nil {
		return loc
	}
	return time.FixedZone("BRT", -3*3600)
}()

// sapDevido diz se a rotina do serviço deve rodar agora: já passou do horário de hoje (Recife) e ainda
// não rodou hoje. Se o sistema ficou fora do ar no horário, roda ao voltar, uma vez só no dia.
func sapDevido(horario, ultimaExecucao string, agora time.Time) bool {
	if !horarioRegexp.MatchString(horario) {
		return false
	}
	agoraLocal := agora.In(fusoRecife)
	hora := int(horario[0]-'0')*10 + int(horario[1]-'0')
	minuto := int(horario[3]-'0')*10 + int(horario[4]-'0')
	marco := time.Date(agoraLocal.Year(), agoraLocal.Month(), agoraLocal.Day(), hora, minuto, 0, 0, fusoRecife)
	if agoraLocal.Before(marco) {
		return false
	}
	if ultimaExecucao == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, ultimaExecucao)
	if err != nil {
		return true
	}
	return t.In(fusoRecife).Before(marco)
}

// RotinaSAPDiaria roda (no máximo uma vez por dia e por serviço) a leitura de cada serviço ativo.
// Chamada a cada 30 s por um laço em main.go.
func RotinaSAPDiaria(db *sql.DB, agora time.Time) {
	m, err := lerConfiguracoes(db)
	if err != nil {
		log.Printf("[SAP] Rotina diária: erro ao ler configurações: %v", err)
		return
	}
	for _, sv := range sapodata.Todos {
		if m[cfgServico(sv, "ativo")] != "true" || !sapDevido(m[cfgSapHorario], m[cfgServico(sv, "ultima_execucao")], agora) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		var res sapodata.Resultado
		if cfg := credencialSAP(m, sv); sv.Chave == sapodata.CentrosCusto.Chave && cfg.APIKey != "" {
			res = cargaCC(ctx, db, cfg, agora)
		} else if sv.Chave == sapodata.ContasContabeis.Chave && cfg.APIKey != "" {
			res = cargaGL(ctx, db, cfg)
		} else {
			res = sondaSAP(ctx, sv, credencialSAP(m, sv))
		}
		cancel()
		guardarResultado(db, sv, res, "agendada", agora)
		log.Printf("[SAP] Rotina diária de %s: ok=%v total=%d (%s)", sv.Chave, res.OK, res.Total, res.Mensagem)
	}
}
