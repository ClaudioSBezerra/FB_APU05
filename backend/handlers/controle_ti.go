package handlers

// controle_ti.go — rotas do módulo Controle TI (migration 025):
//
//	GET  /api/me/permissoes  qualquer usuário autenticado: o que o menu mostra
//	GET  /api/ti/modelo      orçamento + lançamentos + projeção (modelo do painel)
//	POST /api/ti/importar    carrega a planilha FC FY27 Forecast (.xlsx)
//
// Acesso ao módulo: usuarios.controle_ti (interruptor em Gestão de Usuários).
// A checagem lê o banco a cada chamada — ligar/desligar o interruptor vale na
// hora, sem esperar o token expirar.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"fb_apu05/internal/controleti"
)

// 25 MiB casa com o client_max_body_size do nginx (frontend/nginx.conf).
const limiteUploadPlanilha = 25 << 20

const msgSemControleTI = "acesso ao Controle TI não liberado para este usuário"

// temControleTI diz se o usuário está ativo e com o interruptor ligado.
func temControleTI(db *sql.DB, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var liberado bool
	err := db.QueryRow(`SELECT controle_ti AND ativo FROM usuarios WHERE id = $1`, userID).Scan(&liberado)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return liberado, err
}

// nivelTI devolve "adm" (usuarios.controle_ti), "gestor" (tem escopo em ti_gestor_escopo) ou "".
// O ADM vale mais que o escopo de gestor. Lido do banco a cada chamada.
func nivelTI(db *sql.DB, userID string) (string, error) {
	adm, err := temControleTI(db, userID)
	if err != nil || adm {
		if adm {
			return controleti.NivelADM, nil
		}
		return "", err
	}
	e, err := controleti.CarregarEscopo(db, userID)
	if err != nil {
		return "", err
	}
	if e.Vazio() {
		return "", nil
	}
	return controleti.NivelGestor, nil
}

// comTILeitura libera ADM e gestor (rotas só de leitura); o handler decide o que cada um recebe.
func comTILeitura(db *sql.DB, next func(w http.ResponseWriter, r *http.Request, nivel string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nivel, err := nivelTI(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[ControleTI] Erro ao checar permissão: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if nivel == "" {
			jsonErr(w, http.StatusForbidden, msgSemControleTI)
			return
		}
		next(w, r, nivel)
	}
}

// comControleTI barra com 403 quem não tem o interruptor ligado. Vai por
// dentro de RequireAuth (que já garantiu usuário autenticado).
func comControleTI(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		liberado, err := temControleTI(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[ControleTI] Erro ao checar permissão: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !liberado {
			jsonErr(w, http.StatusForbidden, msgSemControleTI)
			return
		}
		next(w, r)
	}
}

// PermissoesHandler — GET /api/me/permissoes. Alimenta o menu lateral.
func PermissoesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		nivel, err := nivelTI(db, GetUserIDFromContext(r))
		if err != nil {
			log.Printf("[ControleTI] Erro ao ler permissões: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"controle_ti": nivel != "", "controle_ti_nivel": nivel})
	}
}

// TIModeloHandler — GET /api/ti/modelo.
func TIModeloHandler(db *sql.DB) http.HandlerFunc {
	return comTILeitura(db, func(w http.ResponseWriter, r *http.Request, nivel string) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		modelo, err := controleti.Carregar(db, time.Now())
		if err != nil {
			log.Printf("[ControleTI] Erro ao montar o modelo: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if nivel == controleti.NivelGestor { // gestor: só os próprios CCs, filtrado AQUI no servidor
			esc, err := controleti.CarregarEscopo(db, GetUserIDFromContext(r))
			if err != nil {
				log.Printf("[ControleTI] Erro ao ler o escopo do gestor: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			modelo = controleti.FiltrarModelo(modelo, esc)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(modelo)
	})
}

// resultadoArquivo é o desfecho de cada arquivo da importação.
type resultadoArquivo struct {
	Arquivo  string                       `json:"arquivo"`
	Tipo     string                       `json:"tipo,omitempty"` // forecast | despesas | orcamento | ksb1 | ksb2 | fbl1n
	Ok       bool                         `json:"ok"`
	Erro     string                       `json:"erro,omitempty"`
	Forecast *controleti.Resumo           `json:"forecast,omitempty"`
	SAP      *controleti.ResumoLeituraSAP `json:"sap,omitempty"`
	FBL1N    *controleti.ResumoFBL1N      `json:"fbl1n,omitempty"`
}

const (
	limiteArquivosImportacao = 10
	limiteTotalImportacao    = 60 << 20 // 3 planilhas de até 25 MB não cabem juntas; 60 MB cobre o uso real
)

// TIImportarHandler — POST /api/ti/importar (multipart, campo "arquivo", um ou mais).
// Aceita a planilha Forecast, a KSB1 e a KSB2 JUNTAS; o tipo de cada uma é detectado pelo
// conteúdo. Ordem: Forecast, KSB1, KSB2 (a conferência depende dos três). Cada arquivo é
// uma transação: um que falha não desfaz os outros, e o desfecho vem arquivo por arquivo.
//   - Forecast: substitui parâmetros, orçamento e lançamentos de fonte "planilha";
//     lançamentos manuais ou do SAP não são tocados.
//   - KSB1: substitui as linhas dos meses de lançamento que o arquivo cobre.
//   - KSB2: foto do saldo em aberto; substitui tudo.
func TIImportarHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, limiteTotalImportacao+1<<20)
		// maxMemory = limite: os arquivos ficam em memória, nunca em disco.
		if err := r.ParseMultipartForm(limiteTotalImportacao); err != nil {
			jsonErr(w, http.StatusBadRequest, "envie as planilhas no campo \"arquivo\" (até 25 MB cada, 60 MB no total)")
			return
		}
		cabecalhos := r.MultipartForm.File["arquivo"]
		if len(cabecalhos) == 0 {
			jsonErr(w, http.StatusBadRequest, "envie as planilhas no campo \"arquivo\"")
			return
		}
		if len(cabecalhos) > limiteArquivosImportacao {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("envie no máximo %d arquivos por vez", limiteArquivosImportacao))
			return
		}

		type arquivo struct {
			nome  string
			dados []byte
			tipo  string
			res   *resultadoArquivo
		}
		var arquivos []*arquivo
		var resultados []*resultadoArquivo
		for _, cab := range cabecalhos {
			nome := filepath.Base(cab.Filename)
			for utf8.RuneCountInString(nome) > 255 {
				_, tam := utf8.DecodeLastRuneInString(nome)
				nome = nome[:len(nome)-tam]
			}
			res := &resultadoArquivo{Arquivo: nome}
			resultados = append(resultados, res)

			f, err := cab.Open()
			if err != nil {
				res.Erro = "não foi possível ler o arquivo"
				continue
			}
			dados, err := io.ReadAll(io.LimitReader(f, limiteUploadPlanilha+1))
			_ = f.Close()
			if err != nil || len(dados) > limiteUploadPlanilha {
				res.Erro = "não foi possível ler o arquivo (limite de 25 MB)"
				continue
			}
			tipo, err := controleti.DetectarTipo(dados)
			if err != nil {
				res.Erro = err.Error()
				continue
			}
			res.Tipo = tipo
			arquivos = append(arquivos, &arquivo{nome: nome, dados: dados, tipo: tipo, res: res})
		}

		// Forecast primeiro, depois KSB1, KSB2 e FBL1N
		ordem := map[string]int{controleti.TipoForecast: 0, controleti.TipoDespesas: 0, controleti.TipoOrcamento: 1, controleti.TipoKSB1: 2, controleti.TipoKSB2: 3, controleti.TipoFBL1N: 4}
		sort.SliceStable(arquivos, func(i, j int) bool { return ordem[arquivos[i].tipo] < ordem[arquivos[j].tipo] })

		userID := GetUserIDFromContext(r)
		for _, a := range arquivos {
			var err error
			switch a.tipo {
			case controleti.TipoForecast:
				var planilha *controleti.Planilha
				if planilha, err = controleti.LerPlanilha(a.dados); err == nil {
					var resumo controleti.Resumo
					if resumo, err = controleti.Importar(db, planilha, userID, a.nome); err == nil {
						a.res.Forecast = &resumo
					}
				}
			case controleti.TipoDespesas:
				var d *controleti.Despesas
				if d, err = controleti.LerDespesas(a.dados); err == nil {
					var resumo controleti.Resumo
					if resumo, err = controleti.ImportarDespesas(db, d, userID, a.nome); err == nil {
						a.res.Forecast = &resumo
					}
				}
			case controleti.TipoOrcamento:
				var o *controleti.OrcamentoTI
				if o, err = controleti.LerOrcamentoTI(a.dados); err == nil {
					var resumo controleti.Resumo
					if resumo, err = controleti.ImportarOrcamentoTI(db, o, userID, a.nome); err == nil {
						a.res.Forecast = &resumo
					}
				}
			case controleti.TipoKSB1:
				var linhas []controleti.KSB1Linha
				var leitura controleti.ResumoLeituraSAP
				if linhas, leitura, err = controleti.LerKSB1(a.dados); err == nil {
					if _, err = controleti.ImportarKSB1(db, linhas, leitura, userID, a.nome); err == nil {
						a.res.SAP = &leitura
					}
				}
			case controleti.TipoKSB2:
				var linhas []controleti.KSB2Linha
				var leitura controleti.ResumoLeituraSAP
				if linhas, leitura, err = controleti.LerKSB2(a.dados); err == nil {
					if _, err = controleti.ImportarKSB2(db, linhas, leitura, userID, a.nome); err == nil {
						a.res.SAP = &leitura
					}
				}
			case controleti.TipoFBL1N:
				var linhas []controleti.PartidaFBL1N
				var leitura controleti.ResumoFBL1N
				if linhas, leitura, err = controleti.LerFBL1N(a.dados); err == nil {
					if _, err = controleti.ImportarFBL1N(db, linhas, leitura, userID, a.nome); err == nil {
						a.res.FBL1N = &leitura
					}
				}
			}
			if err != nil {
				// erro de leitura (arquivo ruim) vem com a causa; erro de gravação não expõe detalhe de banco
				a.res.Erro = mensagemDeImportacao(err)
				log.Printf("[ControleTI] Erro ao importar %q (%s): %v", a.nome, a.tipo, err)
				continue
			}
			a.res.Ok = true
			log.Printf("[ControleTI] %s importou %q (%s)", userID, a.nome, a.tipo)
		}

		ok := 0
		for _, res := range resultados {
			if res.Ok {
				ok++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if ok == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "nenhum arquivo foi importado", "resultados": resultados})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"resultados": resultados})
	})
}

// mensagemDeImportacao: erros de leitura da planilha (em português, escritos por nós) vão para a
// tela; erros de banco não — viram uma mensagem genérica.
func mensagemDeImportacao(err error) string {
	msg := err.Error()
	for _, prefixo := range []string{"inserir ", "iniciar ", "registrar ", "limpar ", "confirmar ", "ler "} {
		if strings.HasPrefix(msg, prefixo) {
			return "erro ao gravar — nada foi alterado neste arquivo"
		}
	}
	return msg
}

// TIConferenciaHandler — GET /api/ti/conferencia: compara a planilha com a KSB1 por pedido + CC + mês.
func TIConferenciaHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		res, err := controleti.Conferencia(db, time.Now())
		if err != nil {
			log.Printf("[ControleTI] Erro na conferência: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"resumo": res.Resumo, "linhas": res.Linhas, "situacoes": controleti.Situacoes,
		})
	})
}

// TIEuHandler — GET /api/ti/eu: quem sou eu no Controle TI (nível e escopo). Alimenta a tela:
// o gestor não vê importação nem conferência.
func TIEuHandler(db *sql.DB) http.HandlerFunc {
	return comTILeitura(db, func(w http.ResponseWriter, r *http.Request, nivel string) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		uid := GetUserIDFromContext(r)
		var nome string
		_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, uid).Scan(&nome)
		resp := map[string]any{"nome": nome, "nivel": nivel, "editor": nivel == controleti.NivelADM, "ccs": []string{}, "grupos": []string{}}
		if nivel == controleti.NivelGestor {
			esc, err := controleti.CarregarEscopo(db, uid)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			resp["ccs"], resp["grupos"] = esc.Lista()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
