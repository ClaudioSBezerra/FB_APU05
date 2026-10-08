// Package anexos é o módulo único de upload/leitura de anexo de cotação
// (Story 3.4, AD-13) — Salvar/Recuperar é a ÚNICA porta de persistência de
// `solicitacao_anexos`; nenhum handler grava bytes de anexo por conta
// própria (Boundaries "Always" da spec 3.4). Mesmo padrão isolado de
// `internal/aprovacao`: sem dependência de net/http nem de driver de banco
// específico na superfície pública — só database/sql.
package anexos

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// DBTX é o subconjunto mínimo de *sql.DB/*sql.Tx que este módulo precisa —
// mesma convenção local de `internal/aprovacao.DBTX` (interface mínima
// satisfeita estruturalmente por *sql.Tx, sem import cruzado entre os dois
// domínios de internal/): só QueryRow, já que tanto o INSERT...RETURNING de
// Salvar quanto o SELECT de Recuperar dispensam Exec. Permite ao handler
// passar a MESMA transação que grava `solicitacoes`/`solicitacao_lancamentos`
// (tudo atômico — Boundaries "Always" da spec 3.4).
type DBTX interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// ArquivoDecodificado é um anexo já validado estruturalmente pelo handler
// (base64 decodificado, nome não vazio) — entrada de Salvar.
type ArquivoDecodificado struct {
	NomeArquivo string
	ContentType string
	Dados       []byte
}

// AnexoRef é o resultado de Salvar — id gravado + hash SHA-256 calculado a
// partir do conteúdo decodificado (nunca recebido do cliente).
type AnexoRef struct {
	ID         string
	HashSHA256 string
}

// AnexoMeta é o metadado devolvido por Recuperar junto do conteúdo.
type AnexoMeta struct {
	NomeArquivo string
	ContentType string
}

// ErrAnexoNaoEncontrado é o sentinela devolvido por Recuperar quando o
// anexoID não existe em `solicitacao_anexos`.
var ErrAnexoNaoEncontrado = errors.New("anexo não encontrado")

// Salvar grava um anexo em `solicitacao_anexos` na transação informada —
// único ponto de escrita de anexo do sistema (AD-13). Computa o hash
// SHA-256 do conteúdo decodificado (nunca confia num hash enviado pelo
// cliente) e devolve a referência gravada.
func Salvar(tx DBTX, solicitacaoID string, nomeArquivo, contentType string, dados []byte) (AnexoRef, error) {
	hash := sha256.Sum256(dados)
	hashHex := hex.EncodeToString(hash[:])

	var id string
	err := tx.QueryRow(`
		INSERT INTO solicitacao_anexos (solicitacao_id, nome_arquivo, content_type, tamanho_bytes, hash_sha256, dados)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, solicitacaoID, nomeArquivo, contentType, len(dados), hashHex, dados).Scan(&id)
	if err != nil {
		return AnexoRef{}, fmt.Errorf("anexos: solicitacao_anexos (insert): %w", err)
	}

	return AnexoRef{ID: id, HashSHA256: hashHex}, nil
}

// Recuperar lê um anexo de `solicitacao_anexos` pelo id. Implementado para
// completar o contrato declarado por AD-13 (Salvar/Recuperar) — nenhum FR/AC
// da Story 3.4 exige download via HTTP, então este método fica disponível
// para uso futuro (ex. fila do administrador revisando a cotação, Epic
// 4/5) sem rota própria roteada nesta story (Boundaries "Never" da spec).
func Recuperar(tx DBTX, anexoID string) (io.Reader, AnexoMeta, error) {
	var nomeArquivo, contentType string
	var dados []byte
	err := tx.QueryRow(`
		SELECT nome_arquivo, content_type, dados FROM solicitacao_anexos WHERE id = $1
	`, anexoID).Scan(&nomeArquivo, &contentType, &dados)
	if err == sql.ErrNoRows {
		return nil, AnexoMeta{}, ErrAnexoNaoEncontrado
	}
	if err != nil {
		return nil, AnexoMeta{}, fmt.Errorf("anexos: solicitacao_anexos (%s): %w", anexoID, err)
	}

	return bytes.NewReader(dados), AnexoMeta{NomeArquivo: nomeArquivo, ContentType: contentType}, nil
}
