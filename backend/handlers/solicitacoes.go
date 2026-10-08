package handlers

// solicitacoes.go — Story 3.1 (Abrir Transferência com aprovação calculada,
// FR-5/FR-10), Story 3.2 (Abrir Inclusão SFC com aprovação calculada,
// FR-7/FR-10), Story 3.3 (Abrir Inclusão com autorizador nominal,
// FR-6/FR-11), Story 3.4 (Abrir Imobilizado com anexo de cotação,
// FR-8/FR-11) e Story 3.5 (Abrir Obras multi-linha, FR-9/FR-11).
//
// AbrirSolicitacaoHandler é a primeira rota do FB_APU05 que não exige
// perfil `administrador` (RequireAuth(..., "") — qualquer solicitante
// autenticado pode abrir uma solicitação). Valida o corpo (lados batendo,
// exercício único, CC autorizado, plano de conta/classe de imobilizado,
// anexo de cotação — balanceamento e exercício único só se aplicam a
// transferencia; Inclusão/Inclusão SFC/Imobilizado são 1 linha só), delega
// o cálculo do aprovador a `internal/aprovacao` (AD-1/AD-2 — o handler
// nunca decide aprovador diretamente), o upload de anexo a
// `internal/anexos` (AD-13 — o handler nunca grava bytes de anexo por conta
// própria) e grava `solicitacoes`+`solicitacao_lancamentos`[+`solicitacao_
// anexos`] numa única transação: qualquer erro de validação,
// ErrSemAlcadaCadastrada ou ErrAutorizadorInvalido não grava nada
// (Boundaries "Always" da spec).
//
// "obras" (Story 3.5) é aceito pelo MESMO handler/endpoint, mas não tem
// nenhum dos campos de divisão/CC/conta/mês que o pipeline de
// `lancamentos` abaixo exige — abrirSolicitacaoObras roda um pipeline de
// validação própria (classificação + linhas por local de obra/subgrupo de
// despesa/ordem de investimento) e grava em `solicitacao_obras_linhas`,
// nunca em `solicitacao_lancamentos` (Boundaries "Never" da spec 3.5).
// Qualquer tipo_solicitacao fora de {"transferencia", "inclusao_sfc",
// "inclusao", "imobilizado", "obras"} é 400 "tipo de solicitação não
// suportado ainda".

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"fb_apu05/internal/anexos"
	"fb_apu05/internal/aprovacao"
)

// toleranciaBalanceamento é a margem aceita entre soma(origem) e
// soma(destino) — R$0,01 (I/O Matrix da spec). Um epsilon extra absorve
// erro de arredondamento de ponto flutuante na soma de float64.
const toleranciaBalanceamento = 0.01 + 1e-9

// mesConvencaoDia1 — "mes" é sempre dia 1 do mês de competência (Design
// Notes da spec: "mes é uma competência completa (DATE, convenção dia 1)").
const mesConvencaoDia1 = "01"

// abrirSolicitacaoRequest é o corpo de POST /api/solicitacoes.
// `centro_custo_id` NÃO é um campo próprio do corpo — é derivado das linhas
// (todas devem referenciar o MESMO centro_custo_id, Boundaries "Always" da
// spec) e copiado para o cabeçalho dentro do handler.
type abrirSolicitacaoRequest struct {
	TipoSolicitacao string              `json:"tipo_solicitacao"`
	Lancamentos     []lancamentoRequest `json:"lancamentos"`
	// AutorizadorID (Story 3.3) é o colaborador_id escolhido pelo
	// solicitante para tipo_solicitacao="inclusao"/"imobilizado" — campo no
	// nível da solicitação, não por linha (só há 1 linha). Ignorado para os
	// demais tipos.
	AutorizadorID string `json:"autorizador_id"`
	// Anexos (Story 3.4) é a lista de anexos de cotação em base64 — campo
	// no nível da solicitação, exigida (len>=1) exclusivamente para
	// tipo_solicitacao="imobilizado". Ignorado para os demais tipos.
	Anexos []anexoRequest `json:"anexos"`
	// Classificacao (Story 3.5) é um dos 4 valores {inclusao, fl,
	// retirada_saldo, transferencia_saldo} — campo no nível da solicitação,
	// obrigatório exclusivamente para tipo_solicitacao="obras". Ignorado
	// para os demais tipos, mesmo precedente de Anexos.
	Classificacao string `json:"classificacao"`
	// ObrasLinhas (Story 3.5) é a lista de linhas de Obras — local de obra +
	// subgrupo de despesa + ordem de investimento + valor, exigida
	// (len>=1) exclusivamente para tipo_solicitacao="obras". Ignorado para
	// os demais tipos.
	ObrasLinhas []obraLinhaRequest `json:"obras_linhas"`
}

type lancamentoRequest struct {
	Lado          string  `json:"lado"`
	DivisaoID     string  `json:"divisao_id"`
	CentroCustoID string  `json:"centro_custo_id"`
	ContaID       string  `json:"conta_id"`
	Mes           string  `json:"mes"`
	Valor         float64 `json:"valor"`
	// ClasseImobilizadoID (Story 3.4) substitui ContaID/Mes exclusivamente
	// para tipo_solicitacao="imobilizado" — classe de imobilizado, não
	// conta contábil/mês de competência (Epic 3 context). Ignorado pelos
	// demais tipos, mesmo precedente de AutorizadorID ignorado por
	// transferencia/inclusao_sfc (Story 3.3).
	ClasseImobilizadoID string `json:"classe_imobilizado_id"`
}

// anexoRequest (Story 3.4) é um anexo de cotação no corpo da requisição —
// conteúdo em base64 dentro do MESMO corpo JSON (Design Notes da spec:
// nunca multipart/novo endpoint).
type anexoRequest struct {
	NomeArquivo    string `json:"nome_arquivo"`
	ContentType    string `json:"content_type"`
	ConteudoBase64 string `json:"conteudo_base64"`
}

// lancamentoValidado é uma linha já validada estruturalmente (formato de
// UUID, lado válido, valor>0, mês parseado OU classe de imobilizado) — usada
// tanto para a checagem de balanceamento/exercício quanto para o INSERT
// final. ContaID/Mes e ClasseImobilizadoID são mutuamente exclusivos
// (preenchido um ou outro conforme o tipo_solicitacao).
type lancamentoValidado struct {
	Lado                string
	DivisaoID           string
	CentroCustoID       string
	ContaID             string
	Mes                 time.Time
	ClasseImobilizadoID string
	Valor               float64
}

// obraLinhaRequest (Story 3.5) é uma linha do corpo de Obras — local de
// obra + subgrupo de despesa + ordem de investimento, nunca divisão/CC/
// conta/mês (Boundaries "Never" da spec). `Lado` só é significativo quando
// a solicitação é classificacao="transferencia_saldo" — ignorado pelas
// outras 3 classificações (I/O Matrix da spec).
type obraLinhaRequest struct {
	Lado              string  `json:"lado"`
	LocalObraID       string  `json:"local_obra_id"`
	SubgrupoDespesaID string  `json:"subgrupo_despesa_id"`
	OrdemInvestimento string  `json:"ordem_investimento"`
	Valor             float64 `json:"valor"`
}

// obraLinhaValidado é uma linha de Obras já validada estruturalmente
// (formato de UUID, ordem_investimento não vazio, valor>0) — Lado é NULL
// (sql.NullString inválido) exceto quando classificacao="transferencia_
// saldo", mesmo quando o cliente envia algo para as outras 3 classificações
// (Design Notes da spec: "lado" é ignorado por completo nesse caso).
type obraLinhaValidado struct {
	Lado              sql.NullString
	LocalObraID       string
	SubgrupoDespesaID string
	OrdemInvestimento string
	Valor             float64
}

// AbrirSolicitacaoHandler — POST /api/solicitacoes. Registrado em main.go
// atrás de RequireAuth(withDB(...), "") — qualquer perfil autenticado
// (solicitante ou administrador), primeira rota do sistema sem exigir
// perfil `administrador`.
func AbrirSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 15MiB (não 1MiB): anexos de cotação em base64 (Story 3.4) inflam
		// ~33% o payload de um upload direto; demais tipos continuam bem
		// abaixo do novo teto.
		r.Body = http.MaxBytesReader(w, r.Body, 15<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		var req abrirSolicitacaoRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}

		// Boundaries "Never" da spec: só "transferencia" (3.1),
		// "inclusao_sfc" (3.2), "inclusao" (3.3), "imobilizado" (3.4) e
		// "obras" (3.5) têm handler — qualquer outro valor é 400, nunca um
		// 501/404 que sugira "não existe rota" (a rota existe; o TIPO ainda
		// não é suportado).
		if req.TipoSolicitacao != "transferencia" && req.TipoSolicitacao != "inclusao_sfc" && req.TipoSolicitacao != "inclusao" && req.TipoSolicitacao != "imobilizado" && req.TipoSolicitacao != "obras" {
			jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
			return
		}

		// Story 3.5: Obras não tem nenhum dos campos de divisão/CC/conta/
		// mês que o pipeline de `lancamentos` abaixo exige — pipeline de
		// validação própria, dentro do MESMO handler/endpoint (Boundaries
		// "Never" da spec: nunca um 2º endpoint).
		if req.TipoSolicitacao == "obras" {
			abrirSolicitacaoObras(w, r, db, req)
			return
		}

		lancamentos, msgErro := validarLancamentosEstrutura(req.Lancamentos, req.TipoSolicitacao)
		if msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}

		if msgErro := validarContagemELadoPorTipo(req.TipoSolicitacao, lancamentos); msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}

		// Story 3.3/3.4: Inclusão e Imobilizado exigem um autorizador_id
		// (UUID) escolhido pelo solicitante — validado contra
		// autorizadores_formulario mais adiante, via AutorizadorNominal
		// (nunca aqui — este handler só checa o FORMATO do campo).
		if req.TipoSolicitacao == "inclusao" || req.TipoSolicitacao == "imobilizado" {
			if !uuidFormatRegexp.MatchString(req.AutorizadorID) {
				jsonErr(w, http.StatusBadRequest, "campo 'autorizador_id' inválido")
				return
			}
		}

		// Story 3.4: Imobilizado exige ao menos 1 anexo de cotação —
		// validação puramente estrutural (sem acesso a banco), mesma faixa
		// das demais checagens de formato acima do tx.Begin(). anexosParaSalvar
		// fica vazio para os demais tipos (Anexos é ignorado por eles).
		var anexosParaSalvar []anexos.ArquivoDecodificado
		if req.TipoSolicitacao == "imobilizado" {
			var msgErroAnexo string
			anexosParaSalvar, msgErroAnexo = validarAnexos(req.Anexos)
			if msgErroAnexo != "" {
				jsonErr(w, http.StatusBadRequest, msgErroAnexo)
				return
			}
		}

		centroCustoID, msgErro := validarMesmoCentroCusto(lancamentos)
		if msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}

		// Balanceamento entre 2 lados e exercício orçamentário único só fazem
		// sentido para Transferência (Boundaries "Never" da spec: Inclusão SFC
		// é sempre 1 única linha, nenhum dos dois conceitos se aplica).
		if req.TipoSolicitacao == "transferencia" {
			if msgErro := validarBalanceamento(lancamentos); msgErro != "" {
				jsonErr(w, http.StatusBadRequest, msgErro)
				return
			}

			if msgErro := validarExercicioUnico(lancamentos); msgErro != "" {
				jsonErr(w, http.StatusBadRequest, msgErro)
				return
			}
		}

		solicitanteID := GetUserIDFromContext(r)

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Solicitacoes] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var ccCodigo string
		var ccDivisaoID string
		var ccFilial sql.NullString
		err = tx.QueryRow(`
			SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1
		`, centroCustoID).Scan(&ccCodigo, &ccDivisaoID, &ccFilial)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "centro de custo inválido")
			return
		} else if err != nil {
			log.Printf("[Solicitacoes] Erro ao buscar centro de custo %s: %v", centroCustoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		autorizado, err := solicitanteAutorizadoParaCC(tx, solicitanteID, centroCustoID)
		if err != nil {
			log.Printf("[Solicitacoes] Erro ao checar autorização de CC (%s/%s): %v", solicitanteID, centroCustoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !autorizado {
			jsonErr(w, http.StatusForbidden, "centro de custo não autorizado para este solicitante")
			return
		}

		// Boundaries "Always" da spec: a divisão da linha é "derivada do CC",
		// nunca um valor independente do cliente — rejeita aqui (400) em vez
		// de deixar a FK de solicitacao_lancamentos.divisao_id estourar como
		// 500 numa divisão bem-formada porém incorreta/inexistente.
		if msgErro := validarDivisaoDerivadaDoCC(lancamentos, ccDivisaoID); msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}

		// Mapa plano/rótulo por tipo (Story 3.3): "inclusao_sfc" é o ÚNICO
		// tipo que usa o plano SFC (FR-7); "inclusao" (Story 3.3) usa BIFC
		// como "transferencia", mas com rótulo próprio na mensagem de erro.
		// Imobilizado (Story 3.4) pula este bloco inteiramente — é classe de
		// imobilizado, não conta contábil, então não existe plano a checar
		// (Code Map da spec 3.4).
		if req.TipoSolicitacao != "imobilizado" {
			var planoEsperado, rotuloTipo string
			switch req.TipoSolicitacao {
			case "inclusao_sfc":
				planoEsperado, rotuloTipo = "SFC", "Inclusão SFC"
			case "inclusao":
				planoEsperado, rotuloTipo = "BIFC", "Inclusão"
			default:
				planoEsperado, rotuloTipo = "BIFC", "Transferência"
			}
			msgErro, errServidor := validarPlanoConta(tx, lancamentos, planoEsperado, rotuloTipo)
			if errServidor != nil {
				log.Printf("[Solicitacoes] Erro ao checar plano de conta: %v", errServidor)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			if msgErro != "" {
				jsonErr(w, http.StatusBadRequest, msgErro)
				return
			}
		}

		// Story 3.4: Imobilizado valida classe_imobilizado_id contra
		// classes_imobilizado (cadastro já administrável desde o Epic 2) —
		// mesmo padrão de validarPlanoConta: (msg, nil) para violação de
		// regra de negócio, (_, err) para erro de infraestrutura (500).
		if req.TipoSolicitacao == "imobilizado" {
			msgErroClasse, errServidorClasse := validarClasseImobilizado(tx, lancamentos)
			if errServidorClasse != nil {
				log.Printf("[Solicitacoes] Erro ao checar classe de imobilizado: %v", errServidorClasse)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			if msgErroClasse != "" {
				jsonErr(w, http.StatusBadRequest, msgErroClasse)
				return
			}
		}

		// Os 2 lados batem dentro de uma tolerância de R$0,01 (não
		// exatamente) — usar o maior dos dois é a escolha conservadora: nunca
		// resolve a alçada para um valor menor do que o cliente de fato
		// movimentou, mesmo bem na borda de uma faixa.
		valorTotal := math.Max(somaPorLado(lancamentos, "origem"), somaPorLado(lancamentos, "destino"))

		resolver, err := aprovacao.ResolverParaTipo(req.TipoSolicitacao, tx)
		if err != nil {
			// Defensivo: o guard de tipo_solicitacao acima já filtra para
			// "transferencia"/"inclusao_sfc"/"inclusao"/"imobilizado" —
			// este branch só seria alcançado se o guard e o dispatch do
			// pacote aprovacao divergissem no futuro.
			jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
			return
		}

		filial := ""
		if ccFilial.Valid {
			filial = ccFilial.String
		}

		aprovadorResolvido, err := resolver.Resolve(aprovacao.Solicitacao{
			Filial:                 filial,
			CentroCustoID:          centroCustoID,
			CentroCustoCodigo:      ccCodigo,
			DivisaoID:              ccDivisaoID,
			Valor:                  valorTotal,
			AutorizadorEscolhidoID: req.AutorizadorID,
		})
		if err != nil {
			var semAlcada *aprovacao.ErrSemAlcadaCadastrada
			if errors.As(err, &semAlcada) {
				jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf(
					"sem alçada cadastrada para o centro de custo %q (filial %q)", semAlcada.CentroCusto, semAlcada.Filial,
				))
				return
			}
			// Story 3.3: autorizador_id escolhido pelo solicitante não tem
			// linha ativa em autorizadores_formulario cobrindo o CC/faixa de
			// valor — 400 (não 422, Design Notes da spec: o próprio cliente
			// enviou esse autorizador_id), mesmo formato de mensagem do
			// sentinela (sem distinguir "ninguém cadastrado" de "pessoa
			// errada").
			var autorizadorInvalido *aprovacao.ErrAutorizadorInvalido
			if errors.As(err, &autorizadorInvalido) {
				log.Printf("[Solicitacoes] Autorizador rejeitado (CC=%s, autorizador_id=%s)", autorizadorInvalido.CentroCusto, req.AutorizadorID)
				jsonErr(w, http.StatusBadRequest, autorizadorInvalido.Error())
				return
			}
			log.Printf("[Solicitacoes] Erro ao resolver aprovador (CC=%s): %v", centroCustoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		snapshot := construirAprovadorSnapshot(aprovadorResolvido)
		snapshotJSON, err := json.Marshal(snapshot)
		if err != nil {
			log.Printf("[Solicitacoes] Erro ao serializar aprovador_snapshot: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var solicitacaoID string
		err = tx.QueryRow(`
			INSERT INTO solicitacoes (tipo_solicitacao, solicitante_id, centro_custo_id, status, aprovador_snapshot, versao)
			VALUES ($1, $2, $3, 'aberta', $4, 1)
			RETURNING id
		`, req.TipoSolicitacao, solicitanteID, centroCustoID, string(snapshotJSON)).Scan(&solicitacaoID)
		if err != nil {
			log.Printf("[Solicitacoes] Erro ao inserir solicitacao: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		for _, l := range lancamentos {
			// Story 3.4: Imobilizado grava classe_imobilizado_id em vez de
			// conta_id/mes (ambos NULL nesse caso) — mesmo invariante do
			// CHECK XOR da migration 008. Para os demais tipos, o inverso
			// (classe_imobilizado_id NULL).
			var contaID, mes, classeImobilizadoID sql.NullString
			if req.TipoSolicitacao == "imobilizado" {
				classeImobilizadoID = sql.NullString{String: l.ClasseImobilizadoID, Valid: true}
			} else {
				contaID = sql.NullString{String: l.ContaID, Valid: true}
				mes = sql.NullString{String: l.Mes.Format(dataISOFormato), Valid: true}
			}

			if _, err := tx.Exec(`
				INSERT INTO solicitacao_lancamentos (solicitacao_id, lado, divisao_id, centro_custo_id, conta_id, mes, classe_imobilizado_id, valor)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			`, solicitacaoID, l.Lado, l.DivisaoID, l.CentroCustoID, contaID, mes, classeImobilizadoID, l.Valor); err != nil {
				log.Printf("[Solicitacoes] Erro ao inserir lançamento (lado=%s): %v", l.Lado, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}

		// Story 3.4: anexos de cotação são gravados na MESMA transação —
		// solicitação+lançamento+anexos, tudo ou nada (Boundaries "Always"
		// da spec). Upload passa exclusivamente por internal/anexos (AD-13)
		// — nenhuma outra lógica de persistência de anexo aqui.
		if req.TipoSolicitacao == "imobilizado" {
			for _, a := range anexosParaSalvar {
				if _, err := anexos.Salvar(tx, solicitacaoID, a.NomeArquivo, a.ContentType, a.Dados); err != nil {
					log.Printf("[Solicitacoes] Erro ao salvar anexo (solicitacao=%s): %v", solicitacaoID, err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Solicitacoes] Erro ao commitar abertura de solicitação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Solicitacoes] %s abriu solicitação %s (tipo=%s, cc=%s, aprovador=%s/%s)",
			solicitanteID, solicitacaoID, req.TipoSolicitacao, centroCustoID, aprovadorResolvido.Tipo, aprovadorResolvido.Nome)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                 solicitacaoID,
			"tipo_solicitacao":   req.TipoSolicitacao,
			"centro_custo_id":    centroCustoID,
			"status":             "aberta",
			"versao":             1,
			"aprovador_snapshot": snapshot,
		})
	}
}

// validarLancamentosEstrutura valida cada linha isoladamente (lado válido,
// UUIDs bem formados, valor>0, e mês no formato YYYY-MM-01 OU classe de
// imobilizado, conforme o tipo). A contagem de linhas e a regra de "lado"
// permitido são tipo-aware e ficam em validarContagemELadoPorTipo, chamada
// logo depois desta (Code Map da spec 3.2). `tipo` (Story 3.4) decide entre
// as duas trilhas mutuamente exclusivas: "imobilizado" valida
// ClasseImobilizadoID (formato UUID) em vez de ContaID/Mes — pula
// parseMesCompetencia/checagem de conta_id por completo; os demais tipos
// seguem com o comportamento inalterado (ContaID/Mes).
func validarLancamentosEstrutura(linhas []lancamentoRequest, tipo string) ([]lancamentoValidado, string) {
	validadas := make([]lancamentoValidado, 0, len(linhas))
	for i, l := range linhas {
		n := i + 1
		if l.Lado != "origem" && l.Lado != "destino" {
			return nil, fmt.Sprintf("linha %d: campo 'lado' deve ser 'origem' ou 'destino'", n)
		}
		if !uuidFormatRegexp.MatchString(l.DivisaoID) {
			return nil, fmt.Sprintf("linha %d: campo 'divisao_id' inválido", n)
		}
		if !uuidFormatRegexp.MatchString(l.CentroCustoID) {
			return nil, fmt.Sprintf("linha %d: campo 'centro_custo_id' inválido", n)
		}
		validada := lancamentoValidado{
			Lado:          l.Lado,
			DivisaoID:     l.DivisaoID,
			CentroCustoID: l.CentroCustoID,
			Valor:         l.Valor,
		}

		if tipo == "imobilizado" {
			if !uuidFormatRegexp.MatchString(l.ClasseImobilizadoID) {
				return nil, fmt.Sprintf("linha %d: campo 'classe_imobilizado_id' inválido", n)
			}
			validada.ClasseImobilizadoID = l.ClasseImobilizadoID
		} else {
			if !uuidFormatRegexp.MatchString(l.ContaID) {
				return nil, fmt.Sprintf("linha %d: campo 'conta_id' inválido", n)
			}
			validada.ContaID = l.ContaID
		}

		// `valor` é checado antes de `mes` para preservar, para os tipos
		// não-imobilizado, a mesma ordem de checagem (conta_id -> valor ->
		// mes) anterior à Story 3.4 (Review Triage Log 2026-10-08: a
		// bifurcação por tipo havia invertido valor/mes).
		if l.Valor <= 0 {
			return nil, fmt.Sprintf("linha %d: campo 'valor' deve ser maior que zero", n)
		}

		if tipo != "imobilizado" {
			mes, err := parseMesCompetencia(l.Mes)
			if err != nil {
				return nil, fmt.Sprintf("linha %d: %v", n, err)
			}
			validada.Mes = mes
		}

		validadas = append(validadas, validada)
	}
	return validadas, ""
}

// validarContagemELadoPorTipo aplica a regra de contagem de linhas e "lado"
// permitido específica de cada tipo_solicitacao (Code Map da spec 3.2/3.3):
// "transferencia" exige ao menos 2 linhas (uma de cada lado — implícito pelo
// balanceamento, mas checado aqui cedo para uma mensagem mais clara);
// "inclusao_sfc", "inclusao" e "imobilizado" (Story 3.4) não têm o conceito
// de "2 lados" de Transferência (Boundaries "Never" da spec) e exigem
// exatamente 1 linha com lado="destino" (mesma regra para os três, Code Map
// da spec 3.3/3.4).
func validarContagemELadoPorTipo(tipo string, linhas []lancamentoValidado) string {
	switch tipo {
	case "transferencia":
		if len(linhas) < 2 {
			return "a solicitação de transferência precisa de ao menos uma linha de origem e uma de destino"
		}
	case "inclusao_sfc":
		if len(linhas) != 1 {
			return "Inclusão SFC aceita apenas uma linha"
		}
		if linhas[0].Lado != "destino" {
			return `inclusão SFC exige lado="destino"`
		}
	case "inclusao":
		if len(linhas) != 1 {
			return "Inclusão aceita apenas uma linha"
		}
		if linhas[0].Lado != "destino" {
			return `inclusão exige lado="destino"`
		}
	case "imobilizado":
		if len(linhas) != 1 {
			return "Imobilizado aceita apenas uma linha"
		}
		if linhas[0].Lado != "destino" {
			return `Imobilizado exige lado="destino"`
		}
	}
	return ""
}

// validarAnexos aplica a parte puramente estrutural (sem acesso a banco) da
// I/O Matrix de anexo (Story 3.4): lista vazia é recusada, cada item exige
// nome_arquivo não vazio e conteudo_base64 decodificável e não vazio.
// Devolve os anexos já decodificados para anexos.Salvar gravar dentro da
// mesma transação mais adiante (Boundaries "Always" da spec: solicitação +
// lançamento + anexos, tudo ou nada).
func validarAnexos(itens []anexoRequest) ([]anexos.ArquivoDecodificado, string) {
	if len(itens) == 0 {
		return nil, "Imobilizado exige ao menos um anexo de cotação"
	}

	decodificados := make([]anexos.ArquivoDecodificado, 0, len(itens))
	for i, a := range itens {
		n := i + 1
		if strings.TrimSpace(a.NomeArquivo) == "" {
			return nil, fmt.Sprintf("anexo %d: campo 'nome_arquivo' obrigatório", n)
		}
		if utf8.RuneCountInString(a.NomeArquivo) > 255 {
			return nil, fmt.Sprintf("anexo %d: campo 'nome_arquivo' excede 255 caracteres", n)
		}
		if utf8.RuneCountInString(a.ContentType) > 100 {
			return nil, fmt.Sprintf("anexo %d: campo 'content_type' excede 100 caracteres", n)
		}
		dados, err := base64.StdEncoding.DecodeString(a.ConteudoBase64)
		if err != nil || len(dados) == 0 {
			return nil, fmt.Sprintf("anexo %d: conteúdo não é base64 válido", n)
		}
		decodificados = append(decodificados, anexos.ArquivoDecodificado{
			NomeArquivo: a.NomeArquivo,
			ContentType: a.ContentType,
			Dados:       dados,
		})
	}
	return decodificados, ""
}

// validarClasseImobilizado garante que toda linha de Imobilizado referencia
// uma classe_imobilizado_id existente em classes_imobilizado (cadastro já
// administrável desde o Epic 2) — mesmo padrão de validarPlanoConta: (msg,
// nil) para violação de regra de negócio, (_, err) para erro de
// infraestrutura que o chamador deve traduzir como 500.
func validarClasseImobilizado(tx *sql.Tx, linhas []lancamentoValidado) (string, error) {
	for i, l := range linhas {
		var id string
		err := tx.QueryRow(`SELECT id FROM classes_imobilizado WHERE id = $1`, l.ClasseImobilizadoID).Scan(&id)
		if err == sql.ErrNoRows {
			return fmt.Sprintf("linha %d: classe de imobilizado não encontrada", i+1), nil
		}
		if err != nil {
			return "", fmt.Errorf("checar classe de imobilizado %s: %w", l.ClasseImobilizadoID, err)
		}
	}
	return "", nil
}

// parseMesCompetencia exige o formato YYYY-MM-DD com dia fixo 01 —
// "convenção dia 1" (Design Notes da spec: `mes` é uma competência completa,
// não um campo de dia livre).
func parseMesCompetencia(valor string) (time.Time, error) {
	trimmed := strings.TrimSpace(valor)
	t, err := time.Parse(dataISOFormato, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("campo 'mes' fora do formato YYYY-MM-DD")
	}
	if t.Format("02") != mesConvencaoDia1 {
		return time.Time{}, fmt.Errorf("campo 'mes' deve ser o dia 1 do mês de competência (ex. \"2026-10-01\")")
	}
	return t, nil
}

// validarMesmoCentroCusto garante que todas as linhas referenciam o MESMO
// centro_custo_id (Boundaries "Always" da spec) — é esse valor único que
// alimenta a checagem de autorização e o resolver.
func validarMesmoCentroCusto(linhas []lancamentoValidado) (string, string) {
	centroCustoID := linhas[0].CentroCustoID
	for i, l := range linhas {
		if l.CentroCustoID != centroCustoID {
			return "", fmt.Sprintf("linha %d: centro de custo diverge das demais linhas da solicitação", i+1)
		}
	}
	return centroCustoID, ""
}

// validarDivisaoDerivadaDoCC garante que toda linha referencia a MESMA
// divisao_id do centro de custo da solicitação (Boundaries "Always" da
// spec: "...e portanto a mesma divisao_id, derivada do CC") — divisao_id
// não é um valor independente que o cliente possa escolher por linha.
func validarDivisaoDerivadaDoCC(linhas []lancamentoValidado, ccDivisaoID string) string {
	for i, l := range linhas {
		if l.DivisaoID != ccDivisaoID {
			return fmt.Sprintf("linha %d: campo 'divisao_id' diverge da divisão do centro de custo", i+1)
		}
	}
	return ""
}

// validarBalanceamento checa soma(origem) == soma(destino) dentro da
// tolerância de R$0,01 (I/O Matrix da spec).
func validarBalanceamento(linhas []lancamentoValidado) string {
	somaOrigem := somaPorLado(linhas, "origem")
	somaDestino := somaPorLado(linhas, "destino")
	if math.Abs(somaOrigem-somaDestino) > toleranciaBalanceamento {
		return fmt.Sprintf(
			"os dois lados da transferência não batem (origem=%.2f, destino=%.2f) — tolerância de R$0,01",
			somaOrigem, somaDestino,
		)
	}
	return ""
}

// construirAprovadorSnapshot monta o `aprovador_snapshot` gravado em
// solicitacoes a partir do Aprovador resolvido — mesmo shape para todos os
// tipos de solicitação (lançamentos e obras), extraído para não duplicar o
// literal entre os dois caminhos.
func construirAprovadorSnapshot(a aprovacao.Aprovador) map[string]interface{} {
	return map[string]interface{}{
		"tipo":           a.Tipo,
		"nome":           a.Nome,
		"colaborador_id": a.ColaboradorID,
		"motivo":         a.Motivo,
		"regra_id":       a.RegraID,
		"regra_versao":   a.RegraVersao,
	}
}

func somaPorLado(linhas []lancamentoValidado, lado string) float64 {
	var soma float64
	for _, l := range linhas {
		if l.Lado == lado {
			soma += l.Valor
		}
	}
	return soma
}

// validarExercicioUnico checa que todas as linhas caem no mesmo
// ano-competência (`mes`) — "exercício orçamentário" é o ano extraído de
// `mes`, nunca um campo submetido à parte (Design Notes da spec).
func validarExercicioUnico(linhas []lancamentoValidado) string {
	ano := linhas[0].Mes.Year()
	for _, l := range linhas {
		if l.Mes.Year() != ano {
			return "mais de um exercício orçamentário"
		}
	}
	return ""
}

// solicitanteAutorizadoParaCC checa usuarios.cc_proprio_id OU cc_excecao
// para o par (solicitante, centro de custo) — Boundaries "Always" da spec.
func solicitanteAutorizadoParaCC(tx *sql.Tx, solicitanteID, centroCustoID string) (bool, error) {
	var ccProprioID sql.NullString
	err := tx.QueryRow(`SELECT cc_proprio_id FROM usuarios WHERE id = $1`, solicitanteID).Scan(&ccProprioID)
	if err != nil {
		return false, err
	}
	if ccProprioID.Valid && ccProprioID.String == centroCustoID {
		return true, nil
	}

	var existeExcecao bool
	err = tx.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM cc_excecao WHERE colaborador_id = $1 AND centro_custo_id = $2)
	`, solicitanteID, centroCustoID).Scan(&existeExcecao)
	if err != nil {
		return false, err
	}
	return existeExcecao, nil
}

// validarPlanoConta checa que toda conta referenciada pelas linhas pertence
// EXATAMENTE ao plano esperado para o tipo_solicitacao em questão (nunca o
// outro plano, mesmo quando o código numérico colide — Boundaries "Always"
// da spec): ("BIFC", "Transferência") para transferencia, ("SFC", "Inclusão
// SFC") para inclusao_sfc. Devolve (mensagem 400, nil) para uma violação de
// regra de negócio, ou (_, err) para um erro de infraestrutura que o
// chamador deve traduzir como 500 — os dois nunca se confundem.
func validarPlanoConta(tx *sql.Tx, linhas []lancamentoValidado, planoEsperado, rotuloTipo string) (string, error) {
	for i, l := range linhas {
		var plano string
		err := tx.QueryRow(`SELECT plano FROM contas WHERE id = $1`, l.ContaID).Scan(&plano)
		if err == sql.ErrNoRows {
			return fmt.Sprintf("linha %d: conta não encontrada", i+1), nil
		}
		if err != nil {
			return "", fmt.Errorf("checar plano da conta %s: %w", l.ContaID, err)
		}
		if plano != planoEsperado {
			return fmt.Sprintf("linha %d: conta do plano %s não é permitida em %s (apenas %s)", i+1, plano, rotuloTipo, planoEsperado), nil
		}
	}
	return "", nil
}

// --- Story 3.5: Abrir Obras multi-linha (FR-9/FR-11) ---

// classificacaoTransferenciaSaldo é a única das 4 classificações de Obras
// que exige os 2 blocos retirada/inclusão balanceados (I/O Matrix da spec);
// as outras 3 ("inclusao", "fl", "retirada_saldo") ignoram `lado` por
// completo (Design Notes da spec).
const classificacaoTransferenciaSaldo = "transferencia_saldo"

// classificacoesObrasValidas são os 4 valores aceitos para o campo
// `classificacao` de uma solicitação de Obras (Design Notes da spec:
// "retirada_saldo" é a 4ª classificação standalone; "retirada"/"inclusao"
// são só os rótulos de `lado`, usados exclusivamente dentro de
// "transferencia_saldo").
var classificacoesObrasValidas = map[string]bool{
	"inclusao":                      true,
	"fl":                            true,
	"retirada_saldo":                true,
	classificacaoTransferenciaSaldo: true,
}

// abrirSolicitacaoObras é o caminho de tipo_solicitacao="obras" dentro do
// MESMO handler/endpoint (AbrirSolicitacaoHandler nunca reimplementado,
// Boundaries "Never" da spec) — roda um pipeline de validação próprio
// (classificação + linhas por local de obra/subgrupo de despesa/ordem de
// investimento, sem CC/filial/conta/mês) e grava em
// `solicitacao_obras_linhas`, nunca em `solicitacao_lancamentos`. Qualquer
// erro de validação ou ErrAutorizadorInvalido não grava nada (mesmo
// invariante atômico do fluxo não-obras).
func abrirSolicitacaoObras(w http.ResponseWriter, r *http.Request, db *sql.DB, req abrirSolicitacaoRequest) {
	// Story 3.3/3.4/3.5: autorizador_id (UUID) escolhido pelo solicitante —
	// validado contra aprovadores_obra mais adiante, via AutorizadorNominal
	// (nunca aqui — este handler só checa o FORMATO do campo).
	if !uuidFormatRegexp.MatchString(req.AutorizadorID) {
		jsonErr(w, http.StatusBadRequest, "campo 'autorizador_id' inválido")
		return
	}

	if msgErro := validarClassificacaoObras(req.Classificacao); msgErro != "" {
		jsonErr(w, http.StatusBadRequest, msgErro)
		return
	}

	linhas, msgErro := validarObrasLinhasEstrutura(req.ObrasLinhas, req.Classificacao)
	if msgErro != "" {
		jsonErr(w, http.StatusBadRequest, msgErro)
		return
	}

	if req.Classificacao == classificacaoTransferenciaSaldo {
		if msgErro := validarBlocosObras(linhas); msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}
	}

	solicitanteID := GetUserIDFromContext(r)

	tx, err := db.Begin()
	if err != nil {
		log.Printf("[Solicitacoes] Erro ao iniciar transação (obras): %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Boundaries "Never" da spec: Obras nunca checa CC/autorização de
	// acesso (não existe CC para checar) — só existência de local de
	// obra/subgrupo de despesa e, quando ordem_investimento != "CRIAR",
	// existência da ordem + casamento com o local/subgrupo da linha.
	msgErroLinhas, errServidor := validarLocalSubgrupoOrdemObras(tx, linhas)
	if errServidor != nil {
		log.Printf("[Solicitacoes] Erro ao validar local/subgrupo/ordem de obras: %v", errServidor)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}
	if msgErroLinhas != "" {
		jsonErr(w, http.StatusBadRequest, msgErroLinhas)
		return
	}

	// valorTotal para Obras é a soma de TODAS as linhas, sem distinguir
	// lado (Code Map da spec), exceto para "transferencia_saldo" que usa o
	// maior dos 2 lados — mesma convenção de Transferência.
	valorTotal := somaTotalObras(linhas, req.Classificacao)

	resolver, err := aprovacao.ResolverParaTipo("obras", tx)
	if err != nil {
		// Defensivo: o guard de tipo_solicitacao no topo do handler já
		// filtra para os tipos suportados — este branch só seria
		// alcançado se o guard e o dispatch do pacote aprovacao
		// divergissem no futuro.
		jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
		return
	}

	aprovadorResolvido, err := resolver.Resolve(aprovacao.Solicitacao{
		TipoSolicitacao:        "obras",
		Valor:                  valorTotal,
		AutorizadorEscolhidoID: req.AutorizadorID,
	})
	if err != nil {
		// Obras nunca passa por ErrSemAlcadaCadastrada (isso é exclusivo de
		// ResolverCalculado) — só ErrAutorizadorInvalido (teto individual,
		// sem CC/faixa) é possível aqui.
		var autorizadorInvalido *aprovacao.ErrAutorizadorInvalido
		if errors.As(err, &autorizadorInvalido) {
			log.Printf("[Solicitacoes] Autorizador de obras rejeitado (autorizador_id=%s)", req.AutorizadorID)
			jsonErr(w, http.StatusBadRequest, autorizadorInvalido.Error())
			return
		}
		log.Printf("[Solicitacoes] Erro ao resolver aprovador de obras: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	snapshot := construirAprovadorSnapshot(aprovadorResolvido)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("[Solicitacoes] Erro ao serializar aprovador_snapshot (obras): %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	// centro_custo_id fica NULL para obras (migration 009: CHECK XOR com
	// classificacao torna essa relação um invariante de schema).
	var solicitacaoID string
	err = tx.QueryRow(`
		INSERT INTO solicitacoes (tipo_solicitacao, solicitante_id, centro_custo_id, status, aprovador_snapshot, versao, classificacao)
		VALUES ('obras', $1, NULL, 'aberta', $2, 1, $3)
		RETURNING id
	`, solicitanteID, string(snapshotJSON), req.Classificacao).Scan(&solicitacaoID)
	if err != nil {
		log.Printf("[Solicitacoes] Erro ao inserir solicitacao (obras): %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	for _, l := range linhas {
		if _, err := tx.Exec(`
			INSERT INTO solicitacao_obras_linhas (solicitacao_id, lado, local_obra_id, subgrupo_despesa_id, ordem_investimento, valor)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, solicitacaoID, l.Lado, l.LocalObraID, l.SubgrupoDespesaID, l.OrdemInvestimento, l.Valor); err != nil {
			log.Printf("[Solicitacoes] Erro ao inserir linha de obras (solicitacao=%s): %v", solicitacaoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[Solicitacoes] Erro ao commitar abertura de solicitação de obras: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	log.Printf("[Solicitacoes] %s abriu solicitação %s (tipo=obras, classificacao=%s, aprovador=%s/%s)",
		solicitanteID, solicitacaoID, req.Classificacao, aprovadorResolvido.Tipo, aprovadorResolvido.Nome)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                 solicitacaoID,
		"tipo_solicitacao":   "obras",
		"centro_custo_id":    nil,
		"classificacao":      req.Classificacao,
		"status":             "aberta",
		"versao":             1,
		"aprovador_snapshot": snapshot,
	})
}

// validarClassificacaoObras checa que `classificacao` é um dos 4 valores do
// Glossário de Obras — "campo 'classificacao' inválido" cobre tanto ausente
// (string vazia) quanto qualquer valor fora da lista (I/O Matrix da spec).
func validarClassificacaoObras(classificacao string) string {
	if !classificacoesObrasValidas[classificacao] {
		return "campo 'classificacao' inválido"
	}
	return ""
}

// validarObrasLinhasEstrutura valida cada linha isoladamente (formato de
// UUID de local_obra_id/subgrupo_despesa_id, ordem_investimento não vazio,
// valor>0) e, só para classificacao="transferencia_saldo", o campo `lado`
// (I/O Matrix da spec: as outras 3 classificações ignoram `lado` por
// completo — a linha validada sai sempre com Lado NULL nesse caso, mesmo
// quando o cliente enviou algo).
func validarObrasLinhasEstrutura(linhas []obraLinhaRequest, classificacao string) ([]obraLinhaValidado, string) {
	if len(linhas) == 0 {
		return nil, "a solicitação de obras precisa de ao menos uma linha"
	}

	validadas := make([]obraLinhaValidado, 0, len(linhas))
	for i, l := range linhas {
		n := i + 1

		if !uuidFormatRegexp.MatchString(l.LocalObraID) {
			return nil, fmt.Sprintf("linha %d: campo 'local_obra_id' inválido", n)
		}
		if !uuidFormatRegexp.MatchString(l.SubgrupoDespesaID) {
			return nil, fmt.Sprintf("linha %d: campo 'subgrupo_despesa_id' inválido", n)
		}
		if strings.TrimSpace(l.OrdemInvestimento) == "" {
			return nil, fmt.Sprintf("linha %d: campo 'ordem_investimento' obrigatório", n)
		}
		if l.Valor <= 0 {
			return nil, fmt.Sprintf("linha %d: campo 'valor' deve ser maior que zero", n)
		}

		validada := obraLinhaValidado{
			LocalObraID:       l.LocalObraID,
			SubgrupoDespesaID: l.SubgrupoDespesaID,
			OrdemInvestimento: strings.TrimSpace(l.OrdemInvestimento),
			Valor:             l.Valor,
		}

		if classificacao == classificacaoTransferenciaSaldo {
			if l.Lado != "retirada" && l.Lado != "inclusao" {
				return nil, fmt.Sprintf(`linha %d: campo 'lado' deve ser 'retirada' ou 'inclusao'`, n)
			}
			validada.Lado = sql.NullString{String: l.Lado, Valid: true}
		}

		validadas = append(validadas, validada)
	}
	return validadas, ""
}

// validarBlocosObras checa, exclusivamente para classificacao=
// "transferencia_saldo", que as linhas formam os 2 blocos (retirada e
// inclusão) exigidos e que a soma de cada bloco bate dentro da MESMA
// tolerância de Transferência (I/O Matrix da spec).
func validarBlocosObras(linhas []obraLinhaValidado) string {
	var somaRetirada, somaInclusao float64
	var temRetirada, temInclusao bool
	for _, l := range linhas {
		if !l.Lado.Valid {
			continue
		}
		switch l.Lado.String {
		case "retirada":
			somaRetirada += l.Valor
			temRetirada = true
		case "inclusao":
			somaInclusao += l.Valor
			temInclusao = true
		}
	}
	if !temRetirada || !temInclusao {
		return "transferência de saldo exige ao menos uma linha de retirada e uma de inclusão"
	}
	if math.Abs(somaRetirada-somaInclusao) > toleranciaBalanceamento {
		return fmt.Sprintf(
			"os dois lados da transferência de saldo não batem (retirada=%.2f, inclusao=%.2f) — tolerância de R$0,01",
			somaRetirada, somaInclusao,
		)
	}
	return ""
}

// validarLocalSubgrupoOrdemObras garante, dentro da MESMA transação que vai
// gravar a solicitação, que toda linha referencia um local_obra_id/
// subgrupo_despesa_id existente e, quando ordem_investimento != "CRIAR",
// uma ordem já existente em obra_ordens cujo local/subgrupo casam com os da
// linha (I/O Matrix da spec). ordem_investimento="CRIAR" pula a checagem de
// obra_ordens por completo — nenhuma ordem é criada no SAP ou em
// obra_ordens nesta etapa (Boundaries "Never" da spec, isso só acontece na
// finalização, Epic 4). Devolve (msg 400, nil) para violação de regra de
// negócio, (_, err) para erro de infraestrutura — mesmo padrão de
// validarPlanoConta/validarClasseImobilizado.
func validarLocalSubgrupoOrdemObras(tx *sql.Tx, linhas []obraLinhaValidado) (string, error) {
	for i, l := range linhas {
		n := i + 1

		var existeLocal bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)`, l.LocalObraID).Scan(&existeLocal); err != nil {
			return "", fmt.Errorf("checar local de obra %s: %w", l.LocalObraID, err)
		}
		if !existeLocal {
			return fmt.Sprintf("linha %d: local de obra não encontrado", n), nil
		}

		var existeSubgrupo bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)`, l.SubgrupoDespesaID).Scan(&existeSubgrupo); err != nil {
			return "", fmt.Errorf("checar subgrupo de despesa %s: %w", l.SubgrupoDespesaID, err)
		}
		if !existeSubgrupo {
			return fmt.Sprintf("linha %d: subgrupo de despesa não encontrado", n), nil
		}

		if l.OrdemInvestimento == "CRIAR" {
			continue
		}

		var ordemLocalObraID, ordemSubgrupoDespesaID string
		err := tx.QueryRow(`
			SELECT local_obra_id, subgrupo_despesa_id FROM obra_ordens WHERE numero_ordem = $1
		`, l.OrdemInvestimento).Scan(&ordemLocalObraID, &ordemSubgrupoDespesaID)
		if err == sql.ErrNoRows {
			return fmt.Sprintf("linha %d: ordem de investimento não encontrada", n), nil
		}
		if err != nil {
			return "", fmt.Errorf("checar ordem de investimento %s: %w", l.OrdemInvestimento, err)
		}
		if !strings.EqualFold(ordemLocalObraID, l.LocalObraID) || !strings.EqualFold(ordemSubgrupoDespesaID, l.SubgrupoDespesaID) {
			return fmt.Sprintf("linha %d: ordem de investimento não pertence ao local de obra/subgrupo de despesa informado", n), nil
		}
	}
	return "", nil
}

// somaTotalObras soma o valor das linhas de Obras. Para as 3 classificações
// standalone ("inclusao", "fl", "retirada_saldo") é a soma de TODAS as
// linhas, sem distinguir lado (Code Map da spec). Para
// "transferencia_saldo", porém, o valor de fato movimentado é o maior dos 2
// blocos (retirada/inclusão) — mesma convenção de Transferência via
// somaPorLado+math.Max — e não a soma das linhas dos dois lados, que
// dobraria o valor usado para resolver a alçada do aprovador.
func somaTotalObras(linhas []obraLinhaValidado, classificacao string) float64 {
	if classificacao == classificacaoTransferenciaSaldo {
		var somaRetirada, somaInclusao float64
		for _, l := range linhas {
			if !l.Lado.Valid {
				continue
			}
			switch l.Lado.String {
			case "retirada":
				somaRetirada += l.Valor
			case "inclusao":
				somaInclusao += l.Valor
			}
		}
		return math.Max(somaRetirada, somaInclusao)
	}

	var soma float64
	for _, l := range linhas {
		soma += l.Valor
	}
	return soma
}
