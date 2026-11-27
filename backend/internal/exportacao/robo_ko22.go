package exportacao

// robo_ko22.go — lote de Obras no formato do "Robo KO22 Obras (novo).xlsm" (27/11/2026): aba `incluir`, cabeçalho
// na linha 5, dados da 6 à 100 (máx. 95). O app escreve A–E; o robô escreve F–L.
//   A Ordem · B Ano (exercício da linha) · C Tipo (inclusao, fl, retirada_saldo, transferencia_saldo)
//   D Valor COM SINAL (retirada < 0) · E Chamado (GO-n, vai no texto do F8 da KO22)

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	abaRoboKO22       = "incluir"
	primeiraLinhaKO22 = 6
	ultimaLinhaKO22   = 100
	maxLinhasLoteKO22 = ultimaLinhaKO22 - primeiraLinhaKO22 + 1
	codigoChamadoFmt  = "GO-%d"
)

// ErrLoteRobo: o lote não cabe nas regras do robô (mensagem já em português, vira 400).
type ErrLoteRobo struct{ Msg string }

func (e *ErrLoteRobo) Error() string { return e.Msg }

func errorsIsRobo(err error) bool {
	var e *ErrLoteRobo
	return errors.As(err, &e)
}

// valorETipoKO22: o sinal e o tipo que o robô espera, a partir da classificação e do lado da linha.
func valorETipoKO22(classificacao, lado string, valor float64) (float64, string) {
	switch classificacao {
	case "retirada_saldo":
		return -valor, "retirada_saldo"
	case "transferencia_saldo":
		if lado == "retirada" {
			return -valor, "transferencia_saldo"
		}
		return valor, "transferencia_saldo"
	case "fl":
		return valor, "fl"
	}
	return valor, "inclusao"
}

func montarLoteRoboKO22(db *sql.DB, templateBytes []byte, linhas []linhaExportacaoObra) ([]byte, error) {
	if len(linhas) > maxLinhasLoteKO22 {
		return nil, &ErrLoteRobo{Msg: fmt.Sprintf("o robô KO22 aceita até %d linhas por lote; este tem %d — divida em lotes menores", maxLinhasLoteKO22, len(linhas))}
	}
	type cab struct {
		codigo string
		ano    int
	}
	cabs := map[string]cab{}
	cel := map[string]celulaRobo{}
	vistas := map[string]bool{}
	for i, l := range linhas {
		c, ok := cabs[l.SolicitacaoID]
		if !ok {
			var numero int64
			var ano sql.NullInt64
			if err := db.QueryRow(`SELECT numero, ano_orcamento FROM solicitacoes WHERE id = $1`, l.SolicitacaoID).Scan(&numero, &ano); err != nil {
				return nil, fmt.Errorf("exportacao: ler número/ano de %s: %w", l.SolicitacaoID, err)
			}
			c = cab{codigo: fmt.Sprintf(codigoChamadoFmt, numero), ano: int(ano.Int64)}
			if !ano.Valid || c.ano == 0 {
				c.ano = time.Now().Year()
			}
			cabs[l.SolicitacaoID] = c
		}
		valor, tipo := valorETipoKO22(l.Classificacao, l.Lado.String, l.Valor)
		ordem := strings.TrimSpace(l.OrdemInvestimento)
		chave := fmt.Sprintf("%s|%d|%.2f|%s", ordem, c.ano, valor, c.codigo)
		if vistas[chave] {
			return nil, &ErrLoteRobo{Msg: fmt.Sprintf("o chamado %s tem duas linhas iguais (ordem %s, %d, valor %.2f) — o robô recusa duplicidade; corrija o chamado", c.codigo, ordem, c.ano, valor)}
		}
		vistas[chave] = true
		n := strconv.Itoa(primeiraLinhaKO22 + i)
		if v, err := strconv.ParseFloat(ordem, 64); err == nil && !strings.ContainsAny(ordem, ".,eE+-") {
			cel["A"+n] = num(v)
		} else {
			cel["A"+n] = txt(ordem)
		}
		cel["B"+n] = num(float64(c.ano))
		cel["C"+n] = txt(tipo)
		cel["D"+n] = num(float64(int64(valor*100+sinalValor(valor)*0.5)) / 100)
		cel["E"+n] = txt(c.codigo)
	}
	return montarXLSMRobo(templateBytes, []edicaoAba{{Aba: abaRoboKO22, Celulas: cel, LimparLinhas: [2]int{primeiraLinhaKO22, ultimaLinhaKO22}}})
}

func sinalValor(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
