package handlers

// solicitacoes.go — Story 3.1 (Abrir Transferência com aprovação calculada,
// FR-5/FR-10) e Story 3.2 (Abrir Inclusão SFC com aprovação calculada,
// FR-7/FR-10).
//
// AbrirSolicitacaoHandler é a primeira rota do FB_APU05 que não exige
// perfil `administrador` (RequireAuth(..., "") — qualquer solicitante
// autenticado pode abrir uma solicitação). Valida o corpo (lados batendo,
// exercício único, CC autorizado, plano de conta — balanceamento e
// exercício único só se aplicam a transferencia, Inclusão SFC é 1 linha
// só), delega o cálculo do aprovador a `internal/aprovacao` (AD-1/AD-2 — o
// handler nunca decide aprovador diretamente) e grava
// `solicitacoes`+`solicitacao_lancamentos` numa única transação: qualquer
// erro de validação ou ErrSemAlcadaCadastrada não grava nada (Boundaries
// "Always" da spec).
//
// Só tipo_solicitacao="transferencia" ou "inclusao_sfc" é aceito até agora;
// qualquer outro valor é 400 "tipo de solicitação não suportado ainda"
// (histórias 3.3-3.5 estendem este MESMO handler, nunca reimplementam).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

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
}

type lancamentoRequest struct {
	Lado          string  `json:"lado"`
	DivisaoID     string  `json:"divisao_id"`
	CentroCustoID string  `json:"centro_custo_id"`
	ContaID       string  `json:"conta_id"`
	Mes           string  `json:"mes"`
	Valor         float64 `json:"valor"`
}

// lancamentoValidado é uma linha já validada estruturalmente (formato de
// UUID, lado válido, valor>0, mês parseado) — usada tanto para a checagem
// de balanceamento/exercício quanto para o INSERT final.
type lancamentoValidado struct {
	Lado          string
	DivisaoID     string
	CentroCustoID string
	ContaID       string
	Mes           time.Time
	Valor         float64
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

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

		// Boundaries "Never" da spec: só "transferencia" (3.1) e
		// "inclusao_sfc" (3.2) têm handler até agora — qualquer outro valor
		// (inclusive os 3 ainda não implementados) é 400, nunca um 501/404
		// que sugira "não existe rota" (a rota existe; o TIPO ainda não é
		// suportado).
		if req.TipoSolicitacao != "transferencia" && req.TipoSolicitacao != "inclusao_sfc" {
			jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
			return
		}

		lancamentos, msgErro := validarLancamentosEstrutura(req.Lancamentos)
		if msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
		}

		if msgErro := validarContagemELadoPorTipo(req.TipoSolicitacao, lancamentos); msgErro != "" {
			jsonErr(w, http.StatusBadRequest, msgErro)
			return
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

		planoEsperado, rotuloTipo := "BIFC", "Transferência"
		if req.TipoSolicitacao == "inclusao_sfc" {
			planoEsperado, rotuloTipo = "SFC", "Inclusão SFC"
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

		// Os 2 lados batem dentro de uma tolerância de R$0,01 (não
		// exatamente) — usar o maior dos dois é a escolha conservadora: nunca
		// resolve a alçada para um valor menor do que o cliente de fato
		// movimentou, mesmo bem na borda de uma faixa.
		valorTotal := math.Max(somaPorLado(lancamentos, "origem"), somaPorLado(lancamentos, "destino"))

		resolver, err := aprovacao.ResolverParaTipo(req.TipoSolicitacao, tx)
		if err != nil {
			// Defensivo: o guard de tipo_solicitacao acima já filtra para
			// "transferencia"/"inclusao_sfc" — este branch só seria
			// alcançado se o guard e o dispatch do pacote aprovacao
			// divergissem no futuro.
			jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
			return
		}

		filial := ""
		if ccFilial.Valid {
			filial = ccFilial.String
		}

		aprovadorResolvido, err := resolver.Resolve(aprovacao.Solicitacao{
			Filial:            filial,
			CentroCustoID:     centroCustoID,
			CentroCustoCodigo: ccCodigo,
			DivisaoID:         ccDivisaoID,
			Valor:             valorTotal,
		})
		if err != nil {
			var semAlcada *aprovacao.ErrSemAlcadaCadastrada
			if errors.As(err, &semAlcada) {
				jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf(
					"sem alçada cadastrada para o centro de custo %q (filial %q)", semAlcada.CentroCusto, semAlcada.Filial,
				))
				return
			}
			log.Printf("[Solicitacoes] Erro ao resolver aprovador (CC=%s): %v", centroCustoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		snapshot := map[string]interface{}{
			"tipo":           aprovadorResolvido.Tipo,
			"nome":           aprovadorResolvido.Nome,
			"colaborador_id": aprovadorResolvido.ColaboradorID,
			"motivo":         aprovadorResolvido.Motivo,
			"regra_id":       aprovadorResolvido.RegraID,
			"regra_versao":   aprovadorResolvido.RegraVersao,
		}
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
			if _, err := tx.Exec(`
				INSERT INTO solicitacao_lancamentos (solicitacao_id, lado, divisao_id, centro_custo_id, conta_id, mes, valor)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, solicitacaoID, l.Lado, l.DivisaoID, l.CentroCustoID, l.ContaID, l.Mes.Format(dataISOFormato), l.Valor); err != nil {
				log.Printf("[Solicitacoes] Erro ao inserir lançamento (lado=%s): %v", l.Lado, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
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
// UUIDs bem formados, valor>0, mês no formato YYYY-MM-01). A contagem de
// linhas e a regra de "lado" permitido são tipo-aware e ficam em
// validarContagemELadoPorTipo, chamada logo depois desta (Code Map da
// spec 3.2).
func validarLancamentosEstrutura(linhas []lancamentoRequest) ([]lancamentoValidado, string) {
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
		if !uuidFormatRegexp.MatchString(l.ContaID) {
			return nil, fmt.Sprintf("linha %d: campo 'conta_id' inválido", n)
		}
		if l.Valor <= 0 {
			return nil, fmt.Sprintf("linha %d: campo 'valor' deve ser maior que zero", n)
		}
		mes, err := parseMesCompetencia(l.Mes)
		if err != nil {
			return nil, fmt.Sprintf("linha %d: %v", n, err)
		}
		validadas = append(validadas, lancamentoValidado{
			Lado:          l.Lado,
			DivisaoID:     l.DivisaoID,
			CentroCustoID: l.CentroCustoID,
			ContaID:       l.ContaID,
			Mes:           mes,
			Valor:         l.Valor,
		})
	}
	return validadas, ""
}

// validarContagemELadoPorTipo aplica a regra de contagem de linhas e "lado"
// permitido específica de cada tipo_solicitacao (Code Map da spec 3.2):
// "transferencia" exige ao menos 2 linhas (uma de cada lado — implícito pelo
// balanceamento, mas checado aqui cedo para uma mensagem mais clara);
// "inclusao_sfc" não tem o conceito de "2 lados" de Transferência (Boundaries
// "Never" da spec) e exige exatamente 1 linha com lado="destino".
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
	}
	return ""
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
