package handlers

// centros_sap_carga.go — carga diária dos Centros (Plant) lidos do SAP pelo gateway
// (internal/sapodata.LerCentros) para a tabela centros. O SAP manda: nome, CNPJ, cidade, UF, grupo de contas e
// empresa são sobrescritos a cada carga. Centro que sumiu do SAP permanece no cadastro. Convive com o envio
// (push) por /api/erp-bridge/centros/sincronizar: os dois gravam pela mesma chave (codigo).

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"fb_apu05/internal/sapodata"
)

// trocável nos testes
var lerCentrosSAP = sapodata.LerCentros

type ResultadoCargaCentros struct{ Lidos, Criados, Atualizados, Ignorados int }

func (r ResultadoCargaCentros) mensagem() string {
	m := fmt.Sprintf("carga ok — %d centros no SAP: %d criados, %d atualizados", r.Lidos, r.Criados, r.Atualizados)
	if r.Ignorados > 0 {
		m += fmt.Sprintf(", %d ignorados (sem código ou nome)", r.Ignorados)
	}
	return m
}

func corta(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func carregarCentrosSAP(ctx context.Context, db *sql.DB, cfg sapodata.Config) (ResultadoCargaCentros, error) {
	var res ResultadoCargaCentros
	cs, err := lerCentrosSAP(ctx, cfg)
	if err != nil {
		return res, err
	}
	res.Lidos = len(cs)
	if len(cs) == 0 {
		return res, fmt.Errorf("o SAP não devolveu nenhum centro")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("erro ao iniciar a gravação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range cs {
		if c.Codigo == "" || c.Nome == "" || len(c.Codigo) > 50 {
			res.Ignorados++
			continue
		}
		var inserida bool
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO centros (codigo, nome, cnpj, cidade, uf, grupo_contas, empresa)
			VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''))
			ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome, cnpj = EXCLUDED.cnpj, cidade = EXCLUDED.cidade,
				uf = EXCLUDED.uf, grupo_contas = EXCLUDED.grupo_contas, empresa = EXCLUDED.empresa, updated_at = now()
			RETURNING (xmax = 0)`,
			c.Codigo, corta(c.Nome, 255), corta(c.CNPJ, 14), corta(c.Cidade, 100), corta(c.UF, 2), corta(c.GrupoContas, 10), corta(c.Empresa, 10)).Scan(&inserida); err != nil {
			return res, fmt.Errorf("erro ao gravar o centro %s: %w", c.Codigo, err)
		}
		if inserida {
			res.Criados++
		} else {
			res.Atualizados++
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("erro ao concluir a gravação: %w", err)
	}
	return res, nil
}

// cargaCentros roda a carga e a transforma em Resultado para a tela de Configurações.
func cargaCentros(ctx context.Context, db *sql.DB, cfg sapodata.Config) sapodata.Resultado {
	ini := time.Now()
	r, err := carregarCentrosSAP(ctx, db, cfg)
	res := sapodata.Resultado{OK: err == nil, Total: r.Lidos, DuracaoMs: time.Since(ini).Milliseconds()}
	if err != nil {
		res.Mensagem = "carga falhou: " + err.Error()
	} else {
		res.Mensagem = r.mensagem()
	}
	return res
}
