package handlers

// colaboradores_oracle_sync.go — sincronização diária de centro de custo a
// partir de SFC.SFC_VW_COLABORADORES (Oracle, servidor FCCORP), via o
// conector ERP Bridge (handlers/erp_bridge.go fornece as credenciais
// Oracle; este arquivo é o destino do que o conector lê de lá e envia de
// volta). Query de origem acordada com o negócio:
//
//	SELECT x.cod_empresa, x.centro_custo, x.numcad, x.nomfun
//	FROM SFC.SFC_VW_COLABORADORES x
//	WHERE x.sitafa <> '7'
//
// Minimização por LGPD: só 4 colunas saem do Oracle (nenhum CPF, cargo,
// local, matrícula fiscal etc. — a view tem bem mais colunas que isso) e
// `nomfun` nunca é persistido aqui — serve só para mensagem de log quando
// uma linha não casa com ninguém no FB_APU05, para o administrador conseguir
// identificar quem falta cadastrar. O único dado gravado é
// `usuarios.cc_proprio_id`.
//
// Diferente de CarregarColaboradoresHandler (Story 2.2, colaboradores.go,
// casa por e-mail via CSV subido manualmente): aqui o casamento é por
// (cod_empresa, numcad) — a view não tem e-mail — e o corpo é JSON vindo do
// conector, não CSV subido por um humano. Mesmo efeito final (atualiza só
// cc_proprio_id, nunca cria usuário, nunca grava em cadastro_historico).
//
// POST /api/erp-bridge/colaboradores/sincronizar — autenticado via
// X-API-Key (erpBridgeAPIKeyValida), mesmo padrão de
// ERPBridgeCredentialsHandler — é o conector quem chama, nunca o navegador.

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type colaboradorOracleLinha struct {
	CodEmpresa  int    `json:"cod_empresa"`
	CentroCusto string `json:"centro_custo"`
	Numcad      int    `json:"numcad"`
	Nomfun      string `json:"nomfun"`
}

type sincronizarColaboradoresRequest struct {
	Colaboradores []colaboradorOracleLinha `json:"colaboradores"`
}

// SincronizarColaboradoresOracleHandler — ver cabeçalho do arquivo.
func SincronizarColaboradoresOracleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		if !erpBridgeAPIKeyValida(db, r) {
			jsonErr(w, http.StatusUnauthorized, "X-API-Key ausente ou inválida")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		var req sincronizarColaboradoresRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if len(req.Colaboradores) == 0 {
			jsonErr(w, http.StatusBadRequest, "campo 'colaboradores' não pode ser vazio")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[ColaboradoresOracleSync] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		atualizados := 0
		ignorados := 0
		for _, linha := range req.Colaboradores {
			if linha.CodEmpresa <= 0 || linha.Numcad <= 0 || linha.CentroCusto == "" {
				ignorados++
				continue
			}

			resultado, err := tx.Exec(`
				UPDATE usuarios u
				SET cc_proprio_id = cc.id, updated_at = now()
				FROM centros_custo cc
				WHERE u.cod_empresa = $1 AND u.numcad = $2
				  AND cc.codigo = $3
				  AND u.cc_proprio_id IS DISTINCT FROM cc.id
			`, linha.CodEmpresa, linha.Numcad, linha.CentroCusto)
			if err != nil {
				log.Printf("[ColaboradoresOracleSync] Erro ao atualizar cod_empresa=%d numcad=%d: %v", linha.CodEmpresa, linha.Numcad, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			linhasAfetadas, _ := resultado.RowsAffected()
			if linhasAfetadas > 0 {
				atualizados++
			} else {
				// Sem usuário com esse (cod_empresa, numcad), sem CC com esse
				// código, ou já estava com o CC correto — nenhum dos três
				// casos é erro (Boundaries "Always" da Story 2.2, mesmo
				// princípio aqui): só loga para o administrador identificar
				// quem falta cadastrar/corrigir, nunca aborta o lote inteiro.
				log.Printf("[ColaboradoresOracleSync] Ignorado: cod_empresa=%d numcad=%d nome=%q centro_custo=%q (sem usuário correspondente, CC desconhecido, ou já sincronizado)",
					linha.CodEmpresa, linha.Numcad, linha.Nomfun, linha.CentroCusto)
				ignorados++
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[ColaboradoresOracleSync] Erro ao commitar sincronização: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[ColaboradoresOracleSync] Sincronização concluída: %d atualizados, %d ignorados (total %d)",
			atualizados, ignorados, len(req.Colaboradores))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"atualizados": atualizados,
			"ignorados":   ignorados,
			"total":       len(req.Colaboradores),
		})
	}
}
