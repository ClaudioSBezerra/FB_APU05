package handlers

// saldo_orcamento.go — saldo orçamentário (API de Trava orçamentária do SAP) mostrado no formulário de solicitação.
// docs/referencia/07-API-SAP-TRAVA-ORCAMENTARIA.md.
//
//	POST /api/solicitacoes/saldo   corpo {"lancamentos":[{lado, centro_custo_id, conta_id, mes}]}
//
// Decisões do usuário (23/11/2026): é AVISO, não trava (nunca impede o envio); se o SAP não responde, a linha volta
// "nao_identificado" (a tela mostra "Saldo Não Identificado"); basta o saldo da ORIGEM (confirmado com a Wanessa).
// Autorização: o SAP não restringe por CC, então AQUI só se consulta a ORIGEM e só para os CCs do solicitante (próprio
// + exceções); o saldo de um CC de destino (de outra pessoa) nunca é devolvido.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"fb_apu05/crypto"
	"fb_apu05/internal/sapodata"
)

const (
	maxLinhasSaldo   = 30
	paralelismoSaldo = 4
	cacheSaldoTTL    = 20 * time.Second
)

var reUUIDSaldo = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// trocável nos testes
var consultarSaldoSAP = sapodata.ConsultarSaldo

type saldoLinhaEntrada struct {
	Lado          string  `json:"lado"`
	CentroCustoID string  `json:"centro_custo_id"`
	ContaID       string  `json:"conta_id"`
	Mes           string  `json:"mes"` // AAAA-MM-DD (dia 1) ou AAAA-MM
	Valor         float64 `json:"valor"`
}

type saldoLinhaSaida struct {
	Indice int    `json:"indice"`
	Lado   string `json:"lado"`
	CC     string `json:"cc"`
	Conta  string `json:"conta"`
	Mes    string `json:"mes"`
	// ok | sem_lancamentos | nao_identificado
	Status string `json:"status"`
	Motivo string `json:"motivo,omitempty"`
	// valores do MÊS (e o acumulado até o mês, que é o que vale para a trava)
	Planejado  *float64 `json:"planejado,omitempty"`
	Realizado  *float64 `json:"realizado,omitempty"`
	Comprom    *float64 `json:"comprometido,omitempty"`
	Disposto   *float64 `json:"disposto,omitempty"`
	Disponivel *float64 `json:"disponivel,omitempty"`
	DispAcum   *float64 `json:"disponivel_acumulado,omitempty"`
	// só origem: o que a solicitação retira até este mês (somando as linhas do mesmo CC+conta) e se passa do saldo
	RetiradaAcum *float64 `json:"retirada_acumulada,omitempty"`
	Excede       bool     `json:"excede"`
	Falta        *float64 `json:"falta,omitempty"`
	// só origem: o que a solicitação retira NESTE mês (CC+conta+mês) contra o disponível do mês (aviso, nunca bloqueia)
	RetiradaMes *float64 `json:"retirada_mes,omitempty"`
	ExcedeMes   bool     `json:"excede_mes"`
	FaltaMes    *float64 `json:"falta_mes,omitempty"`
}

type cacheSaldoItem struct {
	s   *sapodata.Saldo
	err error
	ate time.Time
}

var (
	cacheSaldoMu sync.Mutex
	cacheSaldo   = map[string]cacheSaldoItem{}
)

func r2f(v float64) *float64 { x := math.Round(v*100) / 100; return &x }

// credencialSaldo: o serviço de saldo é consultado DIRETO no SAP (Basic) ou por apikey PRÓPRIA do serviço; nunca
// usa a apikey geral/ambiente do gateway (essa é a chave do Kong, não do SAP).
func credencialSaldo(m map[string]string) sapodata.Config {
	cfg := sapodata.Config{URLBase: m[cfgServico(sapodata.SaldoOrcamentario, "url_base")]}
	if c := m[cfgServico(sapodata.SaldoOrcamentario, "apikey")]; c != "" {
		if txt, err := crypto.DecryptField(c); err == nil {
			cfg.APIKey, cfg.SapClient = txt, m[cfgSapClient]
		}
		return cfg
	}
	cfg.Usuario = m[cfgSapUsuario]
	if c := m[cfgSapSenha]; c != "" {
		if txt, err := crypto.DecryptField(c); err == nil {
			cfg.Senha = txt
		}
	}
	return cfg
}

func saldoCacheado(ctx context.Context, cfg sapodata.Config, objnr, kstar, gjahr string) (*sapodata.Saldo, error) {
	k := objnr + "|" + kstar + "|" + gjahr
	cacheSaldoMu.Lock()
	if it, ok := cacheSaldo[k]; ok && time.Now().Before(it.ate) {
		cacheSaldoMu.Unlock()
		return it.s, it.err
	}
	cacheSaldoMu.Unlock()
	s, err := consultarSaldoSAP(ctx, cfg, objnr, kstar, gjahr)
	ttl := cacheSaldoTTL
	if err != nil {
		ttl = 5 * time.Second // erro: não martela o SAP, mas tenta de novo logo
	}
	cacheSaldoMu.Lock()
	cacheSaldo[k] = cacheSaldoItem{s, err, time.Now().Add(ttl)}
	if len(cacheSaldo) > 500 {
		cacheSaldo = map[string]cacheSaldoItem{}
	}
	cacheSaldoMu.Unlock()
	return s, err
}

type chaveSaldo struct{ objnr, kstar, gjahr string }

// SaldoOrcamentoHandler — POST /api/solicitacoes/saldo.
func SaldoOrcamentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			Lancamentos []saldoLinhaEntrada `json:"lancamentos"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			jsonErr(w, http.StatusBadRequest, "Corpo inválido")
			return
		}
		if len(in.Lancamentos) == 0 || len(in.Lancamentos) > maxLinhasSaldo {
			jsonErr(w, http.StatusBadRequest, "informe de 1 a 30 linhas")
			return
		}
		m, err := lerConfiguracoes(db)
		if err != nil {
			log.Printf("[Saldo] Erro ao ler configurações: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		ativo := m[cfgServico(sapodata.SaldoOrcamentario, "ativo")] == "true"
		if !ativo {
			// chave desligada em Configurações: a tela esconde o saldo (modo de testes sem a API)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ativo": false, "linhas": []saldoLinhaSaida{}})
			return
		}
		autorizados, err := centrosCustoAutorizados(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[Saldo] Erro ao ler os CCs do usuário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		todosLiberados := m[cfgTesteOrigemTodosCC] == "true" // fase de testes: origem livre
		meusCCs := map[string]bool{}
		for _, c := range autorizados {
			meusCCs[c.ID] = true
		}
		cfg := credencialSaldo(m)

		saida := make([]saldoLinhaSaida, len(in.Lancamentos))
		chaves := make([]*chaveSaldo, len(in.Lancamentos))
		mesIdx := make([]int, len(in.Lancamentos))
		for i, l := range in.Lancamentos {
			s := saldoLinhaSaida{Indice: i, Lado: l.Lado, Status: "nao_identificado"}
			mes := strings.TrimSpace(l.Mes)
			if len(mes) >= 7 {
				s.Mes = mes[:7]
			}
			fim := func(motivo string) { s.Motivo = motivo; saida[i] = s }
			if l.Lado != "origem" && l.Lado != "destino" {
				fim("linha inválida")
				continue
			}
			if !reUUIDSaldo.MatchString(l.CentroCustoID) || !reUUIDSaldo.MatchString(l.ContaID) || len(s.Mes) != 7 {
				fim("preencha centro de custo, conta e mês")
				continue
			}
			var cc, conta string
			if err := db.QueryRow(`SELECT codigo FROM centros_custo WHERE id = $1`, l.CentroCustoID).Scan(&cc); err != nil {
				fim("centro de custo não encontrado")
				continue
			}
			if err := db.QueryRow(`SELECT codigo FROM contas WHERE id = $1`, l.ContaID).Scan(&conta); err != nil {
				fim("conta não encontrada")
				continue
			}
			s.CC, s.Conta = cc, conta
			if l.Lado != "origem" {
				fim("o saldo é mostrado só para a origem")
				continue
			}
			if !meusCCs[l.CentroCustoID] && !todosLiberados {
				fim("sem permissão para ver o saldo deste centro de custo")
				continue
			}
			t, errMes := time.Parse("2006-01", s.Mes)
			if errMes != nil {
				fim("mês inválido")
				continue
			}
			objnr, errObj := sapodata.ObjnrCentroCusto(cc)
			if errObj != nil {
				fim("este centro de custo não existe no SAP (código antigo)")
				continue
			}
			saida[i] = s
			chaves[i] = &chaveSaldo{objnr, conta, t.Format("2006")}
			mesIdx[i] = int(t.Month()) - 1
		}

		// consulta (uma por chave distinta), poucas em paralelo
		unicas := map[chaveSaldo]bool{}
		for _, c := range chaves {
			if c != nil {
				unicas[*c] = true
			}
		}
		type res struct {
			s   *sapodata.Saldo
			err error
		}
		resultados := map[chaveSaldo]res{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, paralelismoSaldo)
		for c := range unicas {
			wg.Add(1)
			sem <- struct{}{}
			go func(c chaveSaldo) {
				defer wg.Done()
				defer func() { <-sem }()
				s, err := saldoCacheado(r.Context(), cfg, c.objnr, c.kstar, c.gjahr)
				mu.Lock()
				resultados[c] = res{s, err}
				mu.Unlock()
			}(c)
		}
		wg.Wait()

		// retirada acumulada (origem): por CC+conta+exercício, até cada mês
		type chaveRet struct{ cc, conta, gjahr string }
		retiradas := map[chaveRet][]struct {
			mes   int
			valor float64
		}{}
		for i, l := range in.Lancamentos {
			if chaves[i] != nil && l.Lado == "origem" && l.Valor > 0 {
				k := chaveRet{saida[i].CC, saida[i].Conta, chaves[i].gjahr}
				retiradas[k] = append(retiradas[k], struct {
					mes   int
					valor float64
				}{mesIdx[i], l.Valor})
			}
		}

		for i, c := range chaves {
			if c == nil {
				continue
			}
			s := saida[i]
			rs := resultados[*c]
			if rs.err != nil {
				log.Printf("[Saldo] Consulta ao SAP falhou (%s/%s/%s): %v", c.objnr, c.kstar, c.gjahr, rs.err)
				s.Status, s.Motivo = "nao_identificado", rs.err.Error()
				saida[i] = s
				continue
			}
			n := mesIdx[i]
			s.Status = "ok"
			if !rs.s.Encontrado {
				s.Status = "sem_lancamentos"
			}
			s.Planejado, s.Realizado, s.Comprom = r2f(rs.s.Plan[n]), r2f(rs.s.Real[n]), r2f(rs.s.Comp[n])
			s.Disposto, s.Disponivel, s.DispAcum = r2f(rs.s.Disposto[n]), r2f(rs.s.Disponivel[n]), r2f(rs.s.DispAcum[n])
			if in.Lancamentos[i].Lado == "origem" && in.Lancamentos[i].Valor > 0 {
				var ret, retMes float64
				for _, x := range retiradas[chaveRet{s.CC, s.Conta, c.gjahr}] {
					if x.mes <= n {
						ret += x.valor
					}
					if x.mes == n {
						retMes += x.valor
					}
				}
				s.RetiradaAcum = r2f(ret)
				s.RetiradaMes = r2f(retMes)
				if retMes > rs.s.Disponivel[n]+0.005 {
					s.ExcedeMes, s.FaltaMes = true, r2f(retMes-rs.s.Disponivel[n])
				}
				if ret > rs.s.DispAcum[n]+0.005 {
					s.Excede, s.Falta = true, r2f(ret-rs.s.DispAcum[n])
				}
			}
			saida[i] = s
		}
		sort.Slice(saida, func(a, b int) bool { return saida[a].Indice < saida[b].Indice })
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"ativo": true, "linhas": saida})
	}
}
