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
				return fmt.Sprintf("A transferência aceita uma única conta na %s; só os meses podem variar. Para usar outra conta, abra outro chamado.", lado)
			}
			k := l.Mes.Format("2006-01")
			if meses[k] {
				return fmt.Sprintf("O mês %s aparece duas vezes na %s. Use uma linha por mês.", mesAno(l.Mes), lado)
			}
			meses[k] = true
		}
		if n > MaxMesesPorLado {
			return fmt.Sprintf("A %s aceita no máximo %d meses.", lado, MaxMesesPorLado)
		}
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

// MesSaldo é uma linha da tabela "de janeiro até o mês escolhido" (item C).
type MesSaldo struct {
	Mes           string  `json:"mes"` // AAAA-MM
	Disponivel    float64 `json:"disponivel"`
	Acumulado     float64 `json:"disponivel_acumulado"`
	PodeRemanejar float64 `json:"pode_remanejar"`
}

// podeRemanejar: menor entre o disponível do mês e o acumulado até o mês, nunca abaixo de zero (decisão 26/11).
func podeRemanejar(disponivelMes, acumulado float64) float64 {
	v := math.Min(disponivelMes, acumulado)
	if v < 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func mesesDoSaldo(s *sapodata.Saldo, ano int) []MesSaldo {
	out := make([]MesSaldo, 12)
	for n := 0; n < 12; n++ {
		out[n] = MesSaldo{
			Mes:           fmt.Sprintf("%04d-%02d", ano, n+1),
			Disponivel:    *r2f(s.Disponivel[n]),
			Acumulado:     *r2f(s.DispAcum[n]),
			PodeRemanejar: podeRemanejar(s.Disponivel[n], s.DispAcum[n]),
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

// verificarSaldoOrigem (item 6, bloqueio): para cada CC+conta+ano da ORIGEM, a retirada de cada mês não passa do
// "pode remanejar" do mês e a retirada acumulada até o mês não passa do acumulado. Devolve a mensagem de recusa
// (400) ou "". Se o SAP não responde, não bloqueia (não há como conferir): registra no log e segue — a mesma
// conferência é refeita por quem atende.
func verificarSaldoOrigem(ctx context.Context, db *sql.DB, linhas []lancamentoValidado) (string, error) {
	ativo, cfg, err := saldoAtivoECfg(db)
	if err != nil || !ativo {
		return "", err
	}
	type chave struct {
		cc, conta string
		ano       int
	}
	porChave := map[chave]*[12]float64{}
	ordem := []chave{}
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
	for _, k := range ordem {
		var ccCod, contaCod string
		if err := db.QueryRow(`SELECT codigo FROM centros_custo WHERE id = $1`, k.cc).Scan(&ccCod); err != nil {
			return "", err
		}
		if err := db.QueryRow(`SELECT codigo FROM contas WHERE id = $1`, k.conta).Scan(&contaCod); err != nil {
			return "", err
		}
		objnr, errObj := sapodata.ObjnrCentroCusto(ccCod)
		if errObj != nil {
			continue
		}
		s, errSAP := saldoCacheado(ctx, cfg, objnr, contaCod, fmt.Sprint(k.ano))
		if errSAP != nil {
			log.Printf("[Saldo] Sem resposta do SAP na abertura (%s/%s/%d); segue sem bloquear: %v", ccCod, contaCod, k.ano, errSAP)
			continue
		}
		ret := porChave[k]
		var acum float64
		for n := 0; n < 12; n++ {
			acum += ret[n]
			if ret[n] <= 0 {
				continue
			}
			mes := time.Date(k.ano, time.Month(n+1), 1, 0, 0, 0, 0, time.UTC)
			pode := podeRemanejar(s.Disponivel[n], s.DispAcum[n])
			if ret[n] > pode+0.005 {
				return fmt.Sprintf("Saldo insuficiente na origem em %s: a retirada de %s passa do que pode sair do mês (%s) em %s. Disponível no mês: %s · Acumulado até o mês: %s.",
					mesAno(mes), moedaBR(ret[n]), moedaBR(pode), moedaBR(ret[n]-pode), moedaBR(math.Max(0, s.Disponivel[n])), moedaBR(math.Max(0, s.DispAcum[n]))), nil
			}
			if acum > s.DispAcum[n]+0.005 {
				return fmt.Sprintf("Saldo insuficiente na origem até %s: a retirada acumulada de %s passa do acumulado até o mês (%s) em %s.",
					mesAno(mes), moedaBR(acum), moedaBR(math.Max(0, s.DispAcum[n])), moedaBR(acum-math.Max(0, s.DispAcum[n]))), nil
			}
		}
	}
	return "", nil
}
