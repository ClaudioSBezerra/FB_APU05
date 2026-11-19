package controleti

// store_sap.go — grava e lê as exportações do SAP (KSB1/KSB2) e liga ao modelo.

import (
	"database/sql"
	"fmt"
	"time"
	"unicode/utf8"
)

// cortar limita o texto a n caracteres (as colunas têm tamanho fixo; um valor maior
// derrubaria a carga inteira).
func cortar(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func usuarioOuNulo(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func primeiroDia(ym string) string { return ym + "-01" }

// ImportarKSB1 grava a KSB1: substitui as linhas cujos meses de lançamento o arquivo cobre
// (os outros meses ficam), numa transação.
func ImportarKSB1(db *sql.DB, linhas []KSB1Linha, leitura ResumoLeituraSAP, usuarioID, arquivo string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ini, fim := primeiroDia(leitura.PrimeiroMes), UltimoDia(leitura.UltimoMes)
	var cargaID int64
	if err := tx.QueryRow(`
		INSERT INTO ti_cargas_sap (tipo, arquivo, linhas, aproveitadas, periodo_ini, periodo_fim, usuario_id)
		VALUES ('ksb1', $1, $2, $3, $4, $5, $6) RETURNING id
	`, cortar(arquivo, 255), leitura.Linhas, leitura.Aproveitadas, ini, fim, usuarioOuNulo(usuarioID)).Scan(&cargaID); err != nil {
		return 0, fmt.Errorf("registrar carga: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM ti_ksb1 WHERE data_lancamento BETWEEN $1 AND $2`, ini, fim); err != nil {
		return 0, fmt.Errorf("limpar período da KSB1: %w", err)
	}
	if err := insereEmLotes(tx, "ti_ksb1",
		[]string{"carga_id", "pedido", "cc_sap", "conta", "fornecedor", "cnpj", "texto", "valor", "data_lancamento", "doc_ref", "estornado", "doc_estorno", "ref_estorno"},
		len(linhas), func(i int) []any {
			l := linhas[i]
			return []any{cargaID, cortar(l.Pedido, 30), cortar(l.CCSap, 20), cortar(l.Conta, 20), cortar(l.Fornecedor, 255), cortar(l.CNPJ, 20),
				l.Texto, l.Valor, l.DataLanc, cortar(l.DocRef, 30), l.Estornado, cortar(l.DocEstorno, 30), cortar(l.RefEstorno, 30)}
		}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar carga: %w", err)
	}
	return cargaID, nil
}

// ImportarKSB2 grava a KSB2: é uma foto do saldo em aberto, então substitui tudo.
func ImportarKSB2(db *sql.DB, linhas []KSB2Linha, leitura ResumoLeituraSAP, usuarioID, arquivo string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cargaID int64
	if err := tx.QueryRow(`
		INSERT INTO ti_cargas_sap (tipo, arquivo, linhas, aproveitadas, usuario_id)
		VALUES ('ksb2', $1, $2, $3, $4) RETURNING id
	`, cortar(arquivo, 255), leitura.Linhas, leitura.Aproveitadas, usuarioOuNulo(usuarioID)).Scan(&cargaID); err != nil {
		return 0, fmt.Errorf("registrar carga: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM ti_ksb2`); err != nil {
		return 0, fmt.Errorf("limpar a KSB2: %w", err)
	}
	if err := insereEmLotes(tx, "ti_ksb2",
		[]string{"carga_id", "pedido", "item", "cc_sap", "conta", "fornecedor", "valor", "data_doc", "texto"},
		len(linhas), func(i int) []any {
			l := linhas[i]
			return []any{cargaID, cortar(l.Pedido, 30), cortar(l.Item, 10), cortar(l.CCSap, 20), cortar(l.Conta, 20), cortar(l.Fornecedor, 255),
				l.Valor, nuloSeVazio(l.DataDoc), l.Texto}
		}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar carga: %w", err)
	}
	return cargaID, nil
}

// CarregarKSB1 devolve todas as linhas da KSB1 guardadas.
func CarregarKSB1(db *sql.DB) ([]KSB1Linha, error) {
	rows, err := db.Query(`
		SELECT pedido, cc_sap, conta, fornecedor, cnpj, texto, valor::float8,
		       to_char(data_lancamento, 'YYYY-MM-DD'), doc_ref, estornado, doc_estorno, ref_estorno
		FROM ti_ksb1 ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("ler ti_ksb1: %w", err)
	}
	defer rows.Close()
	var out []KSB1Linha
	for rows.Next() {
		var l KSB1Linha
		if err := rows.Scan(&l.Pedido, &l.CCSap, &l.Conta, &l.Fornecedor, &l.CNPJ, &l.Texto, &l.Valor, &l.DataLanc, &l.DocRef, &l.Estornado, &l.DocEstorno, &l.RefEstorno); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// carregarSAP junta ao modelo a KSB1, os saldos da KSB2 e a configuração do SAP.
func carregarSAP(db *sql.DB, in *Entrada) error {
	ksb1, err := CarregarKSB1(db)
	if err != nil {
		return err
	}
	in.KSB1 = ksb1

	rows, err := db.Query(`SELECT pedido, SUM(valor)::float8 FROM ti_ksb2 GROUP BY pedido`)
	if err != nil {
		return fmt.Errorf("ler saldos da KSB2: %w", err)
	}
	defer rows.Close()
	in.SaldosSAP = map[string]float64{}
	for rows.Next() {
		var pedido string
		var saldo float64
		if err := rows.Scan(&pedido, &saldo); err != nil {
			return err
		}
		in.SaldosSAP[pedido] = r2(saldo)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var quando sql.NullTime
	_ = db.QueryRow(`SELECT created_at FROM ti_cargas_sap WHERE tipo = 'ksb2' ORDER BY id DESC LIMIT 1`).Scan(&quando)
	if quando.Valid {
		in.KSB2Em = quando.Time.Format(time.RFC3339)
	}
	_ = db.QueryRow(`SELECT valor FROM ti_config WHERE chave = 'sap_oficial_desde'`).Scan(&in.SapDesde)
	return carregarFBL1N(db, in)
}

// ImportarFBL1N grava a FBL1N: é uma foto das partidas no dia da exportação, então substitui tudo.
func ImportarFBL1N(db *sql.DB, linhas []PartidaFBL1N, leitura ResumoFBL1N, usuarioID, arquivo string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cargaID int64
	if err := tx.QueryRow(`
		INSERT INTO ti_cargas_sap (tipo, arquivo, linhas, aproveitadas, usuario_id)
		VALUES ('fbl1n', $1, $2, $3, $4) RETURNING id
	`, cortar(arquivo, 255), leitura.Linhas, leitura.Aproveitadas, usuarioOuNulo(usuarioID)).Scan(&cargaID); err != nil {
		return 0, fmt.Errorf("registrar carga: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM ti_fbl1n`); err != nil {
		return 0, fmt.Errorf("limpar a FBL1N: %w", err)
	}
	if err := insereEmLotes(tx, "ti_fbl1n",
		[]string{"carga_id", "empresa", "bp", "fornecedor", "tipo_doc", "referencia", "valor", "documento", "dt_lanc", "vencimento", "dt_comp", "doc_comp", "bloqueio", "divisao", "texto"},
		len(linhas), func(i int) []any {
			l := linhas[i]
			return []any{cargaID, cortar(l.Empresa, 10), cortar(l.BP, 20), cortar(l.Fornecedor, 255), cortar(l.Tipo, 10), cortar(l.Ref, 60),
				l.Valor, cortar(l.Doc, 30), nuloSeVazio(l.DtLanc), nuloSeVazio(l.Venc), nuloSeVazio(l.DtComp), cortar(l.DocComp, 30),
				l.Bloq, cortar(l.Divisao, 10), cortar(l.Texto, 80)}
		}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar carga: %w", err)
	}
	return cargaID, nil
}

// carregarFBL1N junta ao modelo as partidas guardadas e a origem da carga.
func carregarFBL1N(db *sql.DB, in *Entrada) error {
	rows, err := db.Query(`
		SELECT empresa, bp, fornecedor, tipo_doc, referencia, valor::float8, documento,
		       COALESCE(to_char(dt_lanc, 'YYYY-MM-DD'), ''), COALESCE(to_char(vencimento, 'YYYY-MM-DD'), ''),
		       COALESCE(to_char(dt_comp, 'YYYY-MM-DD'), ''), doc_comp, bloqueio, divisao, texto
		FROM ti_fbl1n ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("ler ti_fbl1n: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PartidaFBL1N
		if err := rows.Scan(&p.Empresa, &p.BP, &p.Fornecedor, &p.Tipo, &p.Ref, &p.Valor, &p.Doc, &p.DtLanc, &p.Venc, &p.DtComp, &p.DocComp, &p.Bloq, &p.Divisao, &p.Texto); err != nil {
			return err
		}
		in.FBL1N = append(in.FBL1N, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var quando sql.NullTime
	var arquivo string
	_ = db.QueryRow(`SELECT arquivo, created_at FROM ti_cargas_sap WHERE tipo = 'fbl1n' ORDER BY id DESC LIMIT 1`).Scan(&arquivo, &quando)
	in.FBL1NFonte = arquivo
	if quando.Valid {
		in.FBL1NEm = quando.Time.Format(time.RFC3339)
	}
	return nil
}
