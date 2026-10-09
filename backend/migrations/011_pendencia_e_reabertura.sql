-- Story 4.2 — Pendência e reabertura (Epic 4). Primeira tabela de
-- histórico/comentário do FB_APU05: `solicitacao_comentarios` guarda tanto
-- o motivo de uma pendência (administrador -> solicitante) quanto a
-- resposta do solicitante (que também reabre a solicitação, ver
-- internal/fila.Comentar) — mesma linha de histórico para os dois sentidos
-- (Intent da spec).
CREATE TABLE IF NOT EXISTS solicitacao_comentarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    autor_id UUID NOT NULL REFERENCES usuarios(id),
    tipo VARCHAR(20) NOT NULL CHECK (tipo IN ('pendencia', 'comentario')),
    texto TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GET de detalhe (ObterSolicitacaoHandler) lê o histórico inteiro de uma
-- solicitação ordenado por created_at ASC, id ASC (tie-break — mesmo motivo
-- de idx_solicitacoes_status_created_at/ListarFilaHandler na Story 4.1:
-- sem ele, 2 comentários com o mesmo created_at podem trocar de ordem entre
-- chamadas) — índice composto cobre os 2 campos do ORDER BY.
CREATE INDEX IF NOT EXISTS idx_solicitacao_comentarios_solicitacao_id_created_at
    ON solicitacao_comentarios (solicitacao_id, created_at, id);

-- `fb_apu05_app` já herda `ALL PRIVILEGES` nesta tabela nova via
-- `ALTER DEFAULT PRIVILEGES` (migration 010) — não precisa de GRANT
-- explícito aqui. `fb_apu05_privilegiado` só tem o que é concedido
-- linha a linha (ao contrário de fb_apu05_app): MarcarPendencia/Comentar
-- (internal/fila) só fazem INSERT nesta tabela pela conexão privilegiada,
-- na MESMA transação do UPDATE em `solicitacoes` — por isso só INSERT é
-- concedido (nenhum código lê solicitacao_comentarios por essa conexão;
-- ObterSolicitacaoHandler lê pela conexão GERAL). Sem UPDATE/DELETE hoje
-- por convenção (nenhum handler edita/remove comentário depois de
-- inserido) — isso NÃO é um backstop técnico como o REVOKE do AD-4 em
-- `solicitacoes` (migration 010): fb_apu05_app continua com UPDATE/DELETE
-- nesta tabela via ALTER DEFAULT PRIVILEGES, simplesmente nenhum caminho de
-- código os usa.
GRANT INSERT ON solicitacao_comentarios TO fb_apu05_privilegiado;
