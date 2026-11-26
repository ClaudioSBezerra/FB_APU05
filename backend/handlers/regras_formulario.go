package handlers

// regras_formulario.go — regras dos formulários pedidas pela Controladoria em 26/11/2026 ("Ajustes a fazer",
// "Resposta para a TI" e retorno do Wilker):
//   - A1  Imobilizado: tipo de compra (FL ou Externa) obrigatório;
//   - A2  Transferência: UMA conta (e um CC) por lado; só os meses variam (1 a 12 por lado, sem repetir mês);
//   - A3  descrição detalhada obrigatória em todos os formulários, com tamanho máximo;
//   - A4  Imobilizado: valor unitário informado, total = quantidade × unitário calculado AQUI (o do cliente é ignorado);
//   - item 4 (24/11)  Obras: "CRIAR" não vale na retirada de saldo nem na retirada da transferência de saldo;
//   - item 6  saldo do SAP BLOQUEIA: a retirada do mês não passa do "pode remanejar" (menor entre o disponível do mês
//     e o acumulado até o mês, nunca abaixo de zero), e a retirada acumulada não passa do acumulado;
//   - seção 14  limite de valor por linha e nome de anexo tratado.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"fb_apu05/internal/sapodata"
)

const (
	// MaxDescricao: limite da descrição detalhada (antes aceitava 200 mil caracteres, N4 de 25/11).
	MaxDescricao = 2000
	// MaxFornecedor acompanha a coluna VARCHAR(255).
	MaxFornecedor = 255
	// MaxValorLinha: teto por linha/valor, contra erro de digitação (seção 14 do retorno de 25/11).
	MaxValorLinha = 100_000_000.00
	// MaxMesesPorLado: Transferência aceita de 1 a 12 meses em cada lado.
	MaxMesesPorLado = 12
)

var nomesMeses = [12]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

func mesAno(t time.Time) string { return fmt.Sprintf("%s/%d", nomesMeses[t.Month()-1], t.Year()) }

// validarDescricao: obrigatória (espaços não contam) e até MaxDescricao caracteres. A mensagem muda por formulário.
func validarDescricao(tipo, descricao string) string {
	d := strings.TrimSpace(descricao)
	if d == "" {
		switch tipo {
		case "imobilizado":
			return "Informe a descrição do imobilizado."
		case "obras":
			return "Informe o detalhe."
		default:
			return "Informe a descrição."
		}
	}
	if utf8.RuneCountInString(d) > MaxDescricao {
		return fmt.Sprintf("A descrição passa do limite de %d caracteres (tem %d).", MaxDescricao, utf8.RuneCountInString(d))
	}
	return ""
}

func validarFornecedorTamanho(fornecedor string) string {
	if utf8.RuneCountInString(strings.TrimSpace(fornecedor)) > MaxFornecedor {
		return fmt.Sprintf("O nome do fornecedor passa do limite de %d caracteres.", MaxFornecedor)
	}
	return ""
}

// validarValorMaximo: nenhum valor acima de MaxValorLinha.
func validarValorMaximo(valores ...float64) string {
	for _, v := range valores {
		if v > MaxValorLinha {
			return fmt.Sprintf("O valor %s passa do máximo aceito por linha (%s). Confira se não houve erro de digitação.", moedaBR(v), moedaBR(MaxValorLinha))
		}
	}
	return ""
}

// validarTipoCompra (A1): só Imobilizado, FL ou Externa.
func validarTipoCompra(tipo string) (string, string) {
	switch strings.TrimSpace(tipo) {
	case "FL", "fl":
		return "FL", ""
	case "Externa", "externa", "EXTERNA":
		return "Externa", ""
	}
	return "", "Selecione o tipo de compra (FL ou Externa)."
}

// totalImobilizado (A4): quantidade × unitário, arredondado a 2 casas.
func totalImobilizado(quantidade int, unitario float64) float64 {
	return math.Round(float64(quantidade)*unitario*100) / 100
}

// validarTransferenciaUmaContaPorLado (A2): cada lado tem um único CC e uma única conta; os meses não se repetem
// dentro do lado e são de 1 a 12. Quem precisa tirar de duas contas abre dois chamados.
func validarTransferenciaUmaContaPorLado(linhas []lancamentoValidado) string {
	for _, lado := range []string{"origem", "destino"} {
		noLado := map[string]string{"origem": "na origem", "destino": "no destino"}[lado]
		var cc, conta string
		meses := map[string]bool{}
		n := 0
		for _, l := range linhas {
			if l.Lado != lado {
				continue
			}
			n++
			if cc == "" {
				cc, conta = l.CentroCustoID, l.ContaID
			} else if l.CentroCustoID != cc || l.ContaID != conta {
				return fmt.Sprintf("A transferência aceita uma única conta %s; só os meses podem variar. Para usar outra conta, abra outro chamado.", noLado)
			}
			k := l.Mes.Format("2006-01")
			if meses[k] {
				return fmt.Sprintf("O mês %s aparece duas vezes %s. Use uma linha por mês.", mesAno(l.Mes), noLado)
			}
			meses[k] = true
		}
		if n > MaxMesesPorLado {
			return fmt.Sprintf("A transferência aceita no máximo %d meses %s.", MaxMesesPorLado, noLado)
		}
	}
	return ""
}

// validarMesesDeUmaConta (F2): 1 a 12 linhas, mesma conta e mesmo CC, sem repetir mês.
func validarMesesDeUmaConta(linhas []lancamentoValidado, rotulo string) string {
	if len(linhas) == 0 {
		return rotulo + " precisa de ao menos um mês."
	}
	if len(linhas) > MaxMesesPorLado {
		return fmt.Sprintf("%s aceita no máximo %d meses.", rotulo, MaxMesesPorLado)
	}
	meses := map[string]bool{}
	for _, l := range linhas {
		if l.ContaID != linhas[0].ContaID || l.CentroCustoID != linhas[0].CentroCustoID {
			return rotulo + " aceita uma única conta e um único centro de custo; só os meses podem variar."
		}
		k := l.Mes.Format("2006-01")
		if meses[k] {
			return fmt.Sprintf("O mês %s aparece duas vezes. Use uma linha por mês.", mesAno(l.Mes))
		}
		meses[k] = true
	}
	return ""
}

// validarCriarObras (item 4 de 24/11): "CRIAR" só vale onde a ordem nasce (Inclusão, FL e o lado de inclusão da
// transferência de saldo). Na retirada de saldo e na retirada da transferência de saldo a ordem precisa existir.
func validarCriarObras(classificacao string, linhas []obraLinhaValidado) string {
	for i, l := range linhas {
		if l.OrdemInvestimento != "CRIAR" {
			continue
		}
		if classificacao == "retirada_saldo" {
			return fmt.Sprintf("linha %d: na retirada de saldo a ordem de investimento precisa existir (\"CRIAR\" não vale): é dela que o saldo sai.", i+1)
		}
		if classificacao == classificacaoTransferenciaSaldo && l.Lado.Valid && l.Lado.String == "retirada" {
			return fmt.Sprintf("linha %d: na retirada da transferência de saldo a ordem de investimento precisa existir (\"CRIAR\" não vale): é dela que o saldo sai.", i+1)
		}
	}
	return ""
}

// nomeAnexoSeguro (seção 14): tira caminho, caracteres de controle e os proibidos em nome de arquivo do Windows,
// junta espaços e limita o tamanho preservando a extensão. Nunca devolve vazio.
func nomeAnexoSeguro(nome string) string {
	nome = strings.ReplaceAll(nome, "\\", "/")
	nome = filepath.Base(nome)
	var b strings.Builder
	for _, r := range nome {
		switch {
		case unicode.IsControl(r), strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	limpo := strings.Join(strings.Fields(b.String()), " ")
	limpo = strings.Trim(limpo, ". ")
	if limpo == "" {
		limpo = "anexo"
	}
	if utf8.RuneCountInString(limpo) > 150 {
		ext := filepath.Ext(limpo)
		if utf8.RuneCountInString(ext) > 10 {
			ext = ""
		}
		base := []rune(strings.TrimSuffix(limpo, ext))
		limpo = string(base[:150-utf8.RuneCountInString(ext)]) + ext
	}
	return limpo
}

// ---------------------------------------------------------------- saldo do SAP

// MesSaldo é uma linha da tabela "de janeiro até o mês escolhido" (item C). Disponível e acumulado já vêm SEM o
// que está reservado em chamados abertos (F3, 26/11).
type MesSaldo struct {
	Mes           string   `json:"mes"` // AAAA-MM
	Disponivel    float64  `json:"disponivel"`
	Acumulado     float64  `json:"disponivel_acumulado"`
	Reservado     float64  `json:"reservado"`
	Chamados      []string `json:"chamados_reserva"` // códigos GO-n que seguram valor neste mês
	PodeRemanejar float64  `json:"pode_remanejar"`
}

// podeRemanejar: menor entre o disponível do mês e o acumulado até o mês, nunca abaixo de zero (decisão 26/11).
func podeRemanejar(disponivelMes, acumulado float64) float64 {
	v := math.Min(disponivelMes, acumulado)
	if v < 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

// statusQueReservam (F3): a reserva nasce na abertura e vale enquanto o chamado está vivo (pendência mantém).
// Cancelamento, recusa e finalização com erro a desfazem; na finalização com SUCESSO o valor já está lançado no
// SAP, então a reserva também sai (nunca é descontado duas vezes).
const statusQueReservam = `('aberta', 'em_atendimento', 'pendente', 'aguardando_aprovacao')`

type consultaReservas interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// reservasDoAno: por mês, o que as ORIGENS de Transferências vivas seguram neste CC + conta + ano, e os códigos.
func reservasDoAno(ctx context.Context, q consultaReservas, ccID, contaID string, ano int, ignorar string) ([12]float64, [12][]string, error) {
	var val [12]float64
	var cods [12][]string
	if ignorar == "" {
		ignorar = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := q.QueryContext(ctx, `
		SELECT EXTRACT(MONTH FROM l.mes)::int, l.valor::float8, s.numero
		FROM solicitacao_lancamentos l JOIN solicitacoes s ON s.id = l.solicitacao_id
		WHERE s.tipo_solicitacao = 'transferencia' AND l.lado = 'origem' AND s.status IN `+statusQueReservam+`
		  AND l.centro_custo_id = $1 AND l.conta_id = $2 AND EXTRACT(YEAR FROM l.mes)::int = $3 AND s.id <> $4
		ORDER BY s.numero`, ccID, contaID, ano, ignorar)
	if err != nil {
		return val, cods, err
	}
	defer rows.Close()
	for rows.Next() {
		var m int
		var v float64
		var n int64
		if err := rows.Scan(&m, &v, &n); err != nil {
			return val, cods, err
		}
		if m >= 1 && m <= 12 {
			val[m-1] += v
			c := CodigoChamado(n)
			if len(cods[m-1]) == 0 || cods[m-1][len(cods[m-1])-1] != c {
				cods[m-1] = append(cods[m-1], c)
			}
		}
	}
	return val, cods, rows.Err()
}

// mesesDoSaldo: os 12 meses com o SAP menos o reservado (no mês e acumulado até o mês).
func mesesDoSaldo(s *sapodata.Saldo, ano int, res [12]float64, cods [12][]string) []MesSaldo {
	out := make([]MesSaldo, 12)
	var resAcum float64
	for n := 0; n < 12; n++ {
		resAcum += res[n]
		disp := s.Disponivel[n] - res[n]
		acum := s.DispAcum[n] - resAcum
		chs := cods[n]
		if chs == nil {
			chs = []string{}
		}
		out[n] = MesSaldo{
			Mes:           fmt.Sprintf("%04d-%02d", ano, n+1),
			Disponivel:    *r2f(disp),
			Acumulado:     *r2f(acum),
			Reservado:     *r2f(res[n]),
			Chamados:      chs,
			PodeRemanejar: podeRemanejar(disp, acum),
		}
	}
	return out
}

// saldoAtivoECfg: o serviço de saldo está ligado em Configurações? (desligado = nada é consultado nem bloqueado)
func saldoAtivoECfg(db *sql.DB) (bool, sapodata.Config, error) {
	m, err := lerConfiguracoes(db)
	if err != nil {
		return false, sapodata.Config{}, err
	}
	if m[cfgServico(sapodata.SaldoOrcamentario, "ativo")] != "true" {
		return false, sapodata.Config{}, nil
	}
	return true, credencialSaldo(m), nil
}

// conferirSaldoOrigem é trocável nos testes do handler (os de sqlmock não simulam o SAP).
var conferirSaldoOrigem = verificarSaldoOrigem

// verificarSaldoOrigem (item 6 + F3, bloqueio com reserva): roda DENTRO da transação que vai gravar o chamado. Trava
// cada CC + conta + ano da origem (pg_advisory_xact_lock, até o commit), soma o que os chamados vivos já reservam e
// confere, mês a mês, a retirada contra o "pode remanejar" já descontado e a retirada acumulada contra o acumulado.
// `ignorar` é o próprio chamado na edição. Devolve a mensagem de recusa (400) ou "". Se o SAP não responde, não
// bloqueia (registra no log).
func verificarSaldoOrigem(ctx context.Context, db *sql.DB, tx *sql.Tx, linhas []lancamentoValidado, ignorar string) (string, error) {
	ativo, cfg, err := saldoAtivoECfg(db)
	if err != nil || !ativo {
		return "", err
	}
	type chave struct {
		cc, conta string
		ano       int
	}
	porChave := map[chave]*[12]float64{}
	var ordem []chave
	for _, l := range linhas {
		if l.Lado != "origem" {
			continue
		}
		k := chave{l.CentroCustoID, l.ContaID, l.Mes.Year()}
		if porChave[k] == nil {
			porChave[k] = &[12]float64{}
			ordem = append(ordem, k)
		}
		porChave[k][l.Mes.Month()-1] += l.Valor
	}
	sort.Slice(ordem, func(a, b int) bool { // ordem fixa das travas: sem impasse entre duas aberturas
		return fmt.Sprint(ordem[a]) < fmt.Sprint(ordem[b])
	})
	for _, k := range ordem {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("saldo:%s:%s:%d", k.cc, k.conta, k.ano)); err != nil {
			return "", err
		}
	}
	for _, k := range ordem {
		var ccCod, contaCod string
		if err := tx.QueryRowContext(ctx, `SELECT codigo FROM centros_custo WHERE id = $1`, k.cc).Scan(&ccCod); err != nil {
			return "", err
		}
		if err := tx.QueryRowContext(ctx, `SELECT codigo FROM contas WHERE id = $1`, k.conta).Scan(&contaCod); err != nil {
			return "", err
		}
		objnr, errObj := sapodata.ObjnrCentroCusto(ccCod)
		if errObj != nil {
			continue
		}
		s, errSAP := saldoCacheado(ctx, cfg, objnr, contaCod, fmt.Sprint(k.ano))
		if errSAP != nil {
			log.Printf("[Saldo] Sem resposta do SAP (%s/%s/%d); segue sem bloquear: %v", ccCod, contaCod, k.ano, errSAP)
			continue
		}
		res, cods, err := reservasDoAno(ctx, tx, k.cc, k.conta, k.ano, ignorar)
		if err != nil {
			return "", err
		}
		meses := mesesDoSaldo(s, k.ano, res, cods)
		ret := porChave[k]
		var acum float64
		for n := 0; n < 12; n++ {
			acum += ret[n]
			if ret[n] <= 0 {
				continue
			}
			m := meses[n]
			mes := time.Date(k.ano, time.Month(n+1), 1, 0, 0, 0, 0, time.UTC)
			reservado := ""
			if len(m.Chamados) > 0 {
				reservado = fmt.Sprintf(" %s já reservado(s) em chamado(s) aberto(s): %s.", moedaBR(m.Reservado), strings.Join(m.Chamados, ", "))
			}
			if ret[n] > m.PodeRemanejar+0.005 {
				return fmt.Sprintf("Saldo insuficiente na origem em %s: a retirada de %s passa do que pode sair do mês (%s) em %s. Disponível no mês: %s · Acumulado até o mês: %s.%s",
					mesAno(mes), moedaBR(ret[n]), moedaBR(m.PodeRemanejar), moedaBR(ret[n]-m.PodeRemanejar), moedaBR(math.Max(0, m.Disponivel)), moedaBR(math.Max(0, m.Acumulado)), reservado), nil
			}
			if acum > m.Acumulado+0.005 {
				return fmt.Sprintf("Saldo insuficiente na origem até %s: a retirada acumulada de %s passa do acumulado até o mês (%s) em %s.%s",
					mesAno(mes), moedaBR(acum), moedaBR(math.Max(0, m.Acumulado)), moedaBR(acum-math.Max(0, m.Acumulado)), reservado), nil
			}
		}
	}
	return "", nil
}
