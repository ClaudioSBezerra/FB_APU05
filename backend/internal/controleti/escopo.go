package controleti

// escopo.go — acesso do gestor ao painel (Welton, 19/11/2026): o ADM (Welton e Wanessa) vê tudo; o gestor
// só LÊ e só vê os próprios centros de custo (e/ou grupos de empresa, ex.: RetailX). O filtro é feito
// AQUI, no servidor, antes de o modelo sair — o navegador do gestor nunca recebe as linhas dos outros.

import (
	"database/sql"
	"fmt"
)

const (
	NivelADM    = "adm"
	NivelGestor = "gestor"
)

// Escopo é o que um gestor enxerga: CCs (4 dígitos) e grupos de empresa (FC, RX, IMP).
type Escopo struct {
	CCs    map[string]bool
	Grupos map[string]bool
}

// Vazio diz se o escopo não libera nada.
func (e Escopo) Vazio() bool { return len(e.CCs) == 0 && len(e.Grupos) == 0 }

func (e Escopo) permite(cc, grp string) bool { return e.CCs[cc] || e.Grupos[grp] }

// Lista devolve CCs e grupos em listas ordenadas (para a rota /eu).
func (e Escopo) Lista() (ccs, grupos []string) {
	ccs, grupos = []string{}, []string{}
	for _, k := range chavesOrdenadas(e.CCs) {
		ccs = append(ccs, k)
	}
	for _, k := range chavesOrdenadas(e.Grupos) {
		grupos = append(grupos, k)
	}
	return
}

// CarregarEscopo lê o escopo do gestor (vazio se ele não for gestor).
func CarregarEscopo(db *sql.DB, usuarioID string) (Escopo, error) {
	e := Escopo{CCs: map[string]bool{}, Grupos: map[string]bool{}}
	rows, err := db.Query(`SELECT s.tipo, s.valor FROM ti_gestor_escopo s JOIN usuarios u ON u.id = s.usuario_id
		WHERE s.usuario_id = $1 AND u.ativo`, usuarioID)
	if err != nil {
		return e, fmt.Errorf("ler escopo do gestor: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tipo, valor string
		if err := rows.Scan(&tipo, &valor); err != nil {
			return e, err
		}
		if tipo == "cc" {
			e.CCs[valor] = true
		} else {
			e.Grupos[valor] = true
		}
	}
	return e, rows.Err()
}

// FiltrarModelo devolve uma CÓPIA do modelo só com o que o escopo permite; sem CNPJ/BP (dado pessoal: só
// o ADM vê), sem as partidas órfãs da FBL1N e sem os totais do SAP que somam outras áreas.
func FiltrarModelo(m *Modelo, e Escopo) *Modelo {
	out := &Modelo{Meta: m.Meta, Budget: []BudgetRow{}, CF: []CFRow{}, Lanc: []LancRow{}}
	out.Meta.Ignorado, out.Meta.Sources = map[string]float64{}, nil
	out.Meta.TotSap, out.Meta.TotNosso = 0, 0
	out.Meta.FileName = ""
	for _, b := range m.Budget {
		if e.permite(b.CC, b.Grp) {
			out.Budget = append(out.Budget, b)
		}
	}
	for _, c := range m.CF {
		if e.permite(c.CC, c.Grp) {
			out.CF = append(out.CF, c)
		}
	}
	for _, l := range m.Lanc {
		if e.permite(l.CC, l.Grp) {
			l.BP, l.BPs, l.CNPJ = "", nil, ""
			out.Lanc = append(out.Lanc, l)
		}
	}
	if m.Pag != nil {
		out.Pag = &PagModelo{Meta: m.Pag.Meta, Orphans: []Orfa{}}
	}
	return out
}
