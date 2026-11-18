package handlers

// contas_sap_carga.go — carga diária (06:00) das contas contábeis lidas do SAP pelo gateway
// (internal/sapodata.LerContas) para a tabela contas. O SAP manda: o nome é sobrescrito a cada carga.
//
// O plano de contas da Ferreira Costa no SAP é o "0050" (sapodata.PlanoContas) e é esse mesmo valor que
// vai em contas.plano (a antiga divisão BIFC/SFC deixou de existir). Contas marcadas para eliminação ou bloqueadas para lançamento nem são lidas. Nada é
// apagado pela carga: conta que sumiu do SAP permanece no cadastro.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"fb_apu05/internal/sapodata"
)

// trocável nos testes
var lerContasSAP = sapodata.LerContas

// ResultadoCargaContas resume uma carga.
type ResultadoCargaContas struct {
	Lidas       int
	Criadas     int
	Atualizadas int
	Ignoradas   int
}

func (r ResultadoCargaContas) mensagem(plano string) string {
	m := fmt.Sprintf("carga ok — %d contas ativas do plano %s: %d criadas, %d atualizadas", r.Lidas, plano, r.Criadas, r.Atualizadas)
	if r.Ignoradas > 0 {
		m += fmt.Sprintf(", %d ignoradas (sem nome)", r.Ignoradas)
	}
	return m
}

func carregarContasSAP(ctx context.Context, db *sql.DB, cfg sapodata.Config) (ResultadoCargaContas, string, error) {
	var res ResultadoCargaContas
	plano := sapodata.PlanoContas()
	contas, err := lerContasSAP(ctx, cfg, plano)
	if err != nil {
		return res, plano, err
	}
	res.Lidas = len(contas)
	if len(contas) == 0 {
		// zero contas não é sucesso: pode ser filtro/permissão/plano errado
		return res, plano, fmt.Errorf("o SAP não devolveu nenhuma conta ativa no plano %s", plano)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, plano, fmt.Errorf("erro ao iniciar a gravação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range contas {
		if c.Nome == "" || len(c.Codigo) > 50 {
			res.Ignoradas++
			continue
		}
		nome := c.Nome
		if len(nome) > 255 {
			nome = nome[:255]
		}
		var inserida bool
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO contas (plano, codigo, nome) VALUES ($1, $2, $3)
			ON CONFLICT (plano, codigo) DO UPDATE SET nome = EXCLUDED.nome, updated_at = now()
			RETURNING (xmax = 0)`, plano, c.Codigo, nome).Scan(&inserida); err != nil {
			return res, plano, fmt.Errorf("erro ao gravar a conta %s: %w", c.Codigo, err)
		}
		if inserida {
			res.Criadas++
		} else {
			res.Atualizadas++
		}
	}
	if err := tx.Commit(); err != nil {
		return res, plano, fmt.Errorf("erro ao concluir a gravação: %w", err)
	}
	return res, plano, nil
}

// cargaGL roda a carga de contas e a transforma em Resultado para a tela de Configurações.
func cargaGL(ctx context.Context, db *sql.DB, cfg sapodata.Config) sapodata.Resultado {
	ini := time.Now()
	r, plano, err := carregarContasSAP(ctx, db, cfg)
	res := sapodata.Resultado{OK: err == nil, Total: r.Lidas, DuracaoMs: time.Since(ini).Milliseconds()}
	if err != nil {
		res.Mensagem = "carga falhou: " + err.Error()
	} else {
		res.Mensagem = r.mensagem(plano)
	}
	return res
}
