package handlers

// solicitacoes.go — Story 3.1 (Abrir Transferência com aprovação calculada,
// FR-5/FR-10), Story 3.2 (Abrir Inclusão SFC com aprovação calculada,
// FR-7/FR-10), Story 3.3 (Abrir Inclusão com autorizador nominal,
// FR-6/FR-11) e Story 3.4 (Abrir Imobilizado com anexo de cotação,
// FR-8/FR-11).
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
// Só tipo_solicitacao em {"transferencia", "inclusao_sfc", "inclusao",
// "imobilizado"} é aceito até agora; qualquer outro valor (ex. "obras") é
// 400 "tipo de solicitação não suportado ainda" (história 3.5 estende este
// MESMO handler, nunca reimplementa).

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
		// "inclusao_sfc" (3.2), "inclusao" (3.3) e "imobilizado" (3.4) têm
		// handler até agora — qualquer outro valor (inclusive "obras",
		// ainda não implementado) é 400, nunca um 501/404 que sugira "não
		// existe rota" (a rota existe; o TIPO ainda não é suportado).
		if req.TipoSolicitacao != "transferencia" && req.TipoSolicitacao != "inclusao_sfc" && req.TipoSolicitacao != "inclusao" && req.TipoSolicitacao != "imobilizado" {
			jsonErr(w, http.StatusBadRequest, "tipo de solicitação não suportado ainda")
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
