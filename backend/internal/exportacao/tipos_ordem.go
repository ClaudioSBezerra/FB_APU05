package exportacao

// tipos_ordem.go — tipo de despesa e tipo de faturamento de uma ordem de Obras (F8, 26/11/2026), como na versão de
// referência (ESTRUTURA-ORDEM-INVESTIMENTO §4, tipoFaturamentoDe):
//   - o TIPO DE DESPESA sai da classificação do chamado: FL = aquisição interna; Inclusão, Retirada e Transferência
//     de saldo = aquisição externa;
//   - o TIPO DE FATURAMENTO nunca é digitado: copia o de uma ordem "irmã" (mesmo nó da estrutura, mesma obra e
//     mesma despesa); sem irmã, grupo M–R é "direto"; em A–L, famílias Nova e Anexo Central dão
//     "aquisicao_externa" (ou "fl" na despesa interna) e as outras dão "direto" (ou "diversos" na interna).

import (
	"database/sql"
	"strings"
)

const (
	DespesaExterna = "aquisicao_externa"
	DespesaInterna = "aquisicao_interna"
)

// DespesaDaClassificacao: FL é aquisição interna; as demais, externa.
func DespesaDaClassificacao(classificacao string) string {
	if classificacao == "fl" {
		return DespesaInterna
	}
	return DespesaExterna
}

// NoDaEstrutura: "A14.2" → "A14" (o código do subgrupo é a posição na estrutura + um número de ordem).
func NoDaEstrutura(codigoSubgrupo string) string {
	if i := strings.Index(codigoSubgrupo, "."); i > 0 {
		return codigoSubgrupo[:i]
	}
	return codigoSubgrupo
}

type consulta interface {
	QueryRow(query string, args ...any) *sql.Row
}

// FaturamentoDerivado devolve o tipo de faturamento de uma ordem NOVA. Erros de leitura viram a regra padrão.
func FaturamentoDerivado(q consulta, localID, subgrupoID, despesa string) string {
	var codigo, familia string
	_ = q.QueryRow(`SELECT sg.codigo, COALESCE(lo.tipo_estrutura, '') FROM subgrupos_despesa sg, locais_obra lo WHERE sg.id = $1 AND lo.id = $2`,
		subgrupoID, localID).Scan(&codigo, &familia)
	no := NoDaEstrutura(codigo)
	var irmao string
	if no != "" && q.QueryRow(`
		SELECT o.tipo_faturamento FROM obra_ordens o JOIN subgrupos_despesa sg ON sg.id = o.subgrupo_despesa_id
		WHERE o.local_obra_id = $1 AND split_part(sg.codigo, '.', 1) = $2 AND o.tipo_despesa = $3 AND o.tipo_faturamento IS NOT NULL
		ORDER BY o.created_at LIMIT 1`, localID, no, despesa).Scan(&irmao) == nil && irmao != "" {
		return irmao
	}
	return FaturamentoPadrao(no, familia, despesa)
}

// FaturamentoPadrao: a regra sem ordem irmã.
func FaturamentoPadrao(no, familia, despesa string) string {
	letra := ""
	if no != "" {
		letra = strings.ToUpper(no[:1])
	}
	if letra >= "M" && letra <= "R" {
		return "direto"
	}
	interna := despesa == DespesaInterna
	if familia == "Nova" || familia == "Anexo Central" {
		if interna {
			return "fl"
		}
		return "aquisicao_externa"
	}
	if interna {
		return "diversos"
	}
	return "direto"
}

// RotuloFaturamento: o bloco de colunas da planilha em que a ordem entra (como na referência).
func RotuloFaturamento(fat string) string {
	switch fat {
	case "aquisicao_externa":
		return "aquisição externa · colunas D/E"
	case "fl":
		return "FL · colunas F/G"
	case "direto":
		return "faturamento direto · colunas D/E"
	case "sub_contratada":
		return "faturamento sub-contratada · colunas F/G"
	case "diversos":
		return "faturamento diversos · colunas H/I"
	}
	return fat
}

// RotuloDespesa: como a referência mostra o tipo de despesa.
func RotuloDespesa(d string) string {
	if d == DespesaInterna {
		return "aquisição interna (FL)"
	}
	return "aquisição externa (contrato)"
}
