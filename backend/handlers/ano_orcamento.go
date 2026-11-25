package handlers

// ano_orcamento.go — regra do ANO do orçamento nas solicitações de Transferência, Inclusão e Inclusão SFC
// (pedido do Wilker, 24/11/2026). Não há movimentação entre anos; a única exceção é o fechamento de janeiro:
//   - fora da janela, só o ano corrente;
//   - até a data-limite (padrão 31/01, parâmetro `orcamento_ano_anterior_ate` = "MM-DD"), vale o corrente OU o anterior;
//   - a solicitação inteira é de um único ano (na transferência, origem e destino no mesmo ano);
//   - "hoje" e "ano corrente" vêm do relógio do SERVIDOR em America/Recife, nunca do navegador.
// A regra vale na criação. Solicitação já gravada com o ano anterior segue o fluxo (aprovação, exportação) com o
// ano gravado, mesmo depois da data-limite.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // fuso America/Recife embutido, independente do que a imagem traz
)

const limiteAnoAnteriorPadrao = "01-31"

var locRecife = func() *time.Location {
	l, err := time.LoadLocation("America/Recife")
	if err != nil {
		return time.FixedZone("-03", -3*3600) // Recife não tem horário de verão: UTC-3 o ano todo
	}
	return l
}()

// agoraRecife é o relógio da regra (trocável nos testes).
var agoraRecife = func() time.Time { return time.Now().In(locRecife) }

// tipoUsaAnoUnico: os formulários que seguem a regra. Obras e Imobilizado não mudam.
func tipoUsaAnoUnico(tipo string) bool {
	return tipo == "transferencia" || tipo == "inclusao" || tipo == "inclusao_sfc"
}

// parseLimiteAnoAnterior converte "MM-DD" (ex.: "01-31"); inválido vira o padrão.
func parseLimiteAnoAnterior(v string) (mes time.Month, dia int) {
	v = strings.TrimSpace(v)
	if len(v) == 5 && v[2] == '-' {
		m, e1 := strconv.Atoi(v[:2])
		d, e2 := strconv.Atoi(v[3:])
		if e1 == nil && e2 == nil && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
			return time.Month(m), d
		}
	}
	return time.January, 31
}

// anosPermitidos devolve [ano corrente] e, enquanto o dia ainda não passou da data-limite (inclusive, até
// 23:59:59 em Recife), também o ano anterior. O corrente vem primeiro (é o padrão da tela).
func anosPermitidos(agora time.Time, limiteMMDD string) []int {
	agora = agora.In(locRecife)
	ano := agora.Year()
	mes, dia := parseLimiteAnoAnterior(limiteMMDD)
	// último instante da janela: fim do dia-limite do ano corrente
	fim := time.Date(ano, mes, dia, 23, 59, 59, 999999999, locRecife)
	if !agora.After(fim) {
		return []int{ano, ano - 1}
	}
	return []int{ano}
}

// limiteAnoAnteriorValido: "MM-DD" de um dia que existe (29/02 vale: o ano bissexto decide na hora).
func limiteAnoAnteriorValido(v string) bool {
	if len(v) != 5 || v[2] != '-' {
		return false
	}
	m, e1 := strconv.Atoi(v[:2])
	d, e2 := strconv.Atoi(v[3:])
	if e1 != nil || e2 != nil || m < 1 || m > 12 || d < 1 {
		return false
	}
	t := time.Date(2024, time.Month(m), d, 0, 0, 0, 0, time.UTC) // 2024 é bissexto
	return int(t.Month()) == m && t.Day() == d
}

func limiteAnoAnteriorConfigurado(q consultaRow) string {
	var v string
	if err := q.QueryRow(`SELECT valor FROM configuracoes WHERE chave = 'orcamento_ano_anterior_ate'`).Scan(&v); err != nil {
		return limiteAnoAnteriorPadrao
	}
	return v
}

func anosPermitidosAgora(q consultaRow) []int {
	return anosPermitidos(agoraRecife(), limiteAnoAnteriorConfigurado(q))
}

func listaAnos(anos []int) string {
	s := make([]string, len(anos))
	for i, a := range anos {
		s[i] = strconv.Itoa(a)
	}
	return strings.Join(s, " ou ")
}

// validarAnoDoOrcamento: todas as linhas no MESMO ano e esse ano entre os permitidos. Mensagens pensadas para
// o usuário final.
func validarAnoDoOrcamento(tipo string, linhas []lancamentoValidado, permitidos []int) string {
	if !tipoUsaAnoUnico(tipo) || len(linhas) == 0 {
		return ""
	}
	ano := linhas[0].Mes.Year()
	for _, l := range linhas {
		if l.Mes.Year() != ano {
			if tipo == "transferencia" {
				return "A origem e o destino da transferência precisam estar no mesmo ano; não é possível transferir de um ano para outro."
			}
			return "Todas as linhas da solicitação precisam ser do mesmo ano."
		}
	}
	for _, p := range permitidos {
		if p == ano {
			return ""
		}
	}
	return fmt.Sprintf("O ano %d não é permitido para movimentar o orçamento. Neste momento só é possível usar %s.", ano, listaAnos(permitidos))
}

// Obras (retorno da homologação, item 7): retirada e transferência de saldo aceitam o ano ANTERIOR e o atual
// (diferente da regra de janeiro dos formulários de verba). Inclusão e FL não têm ano.
func classificacaoUsaAno(c string) bool { return c == "retirada_saldo" || c == "transferencia_saldo" }

// anosObras devolve [ano anterior, ano atual].
func anosObras(anoCorrente int) []int { return []int{anoCorrente - 1, anoCorrente} }

// resolverAnoObras: 0 = ano atual; fora de {anterior, atual} é recusado com mensagem clara.
func resolverAnoObras(pedido, anoCorrente int) (int, string) {
	if pedido == 0 {
		return anoCorrente, ""
	}
	for _, a := range anosObras(anoCorrente) {
		if pedido == a {
			return pedido, ""
		}
	}
	return 0, fmt.Sprintf("O ano %d não é permitido para Obras. Escolha %d ou %d.", pedido, anoCorrente-1, anoCorrente)
}
