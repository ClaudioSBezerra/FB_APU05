-- Story 4.3 — Gerar lote Despesa (Epic 4, FR-15, AD-3/AD-4/AD-12).
--
-- `exportacoes_sap`: 1 linha por SOLICITAÇÃO incluída num lote gerado —
-- nunca 1 linha por lote (permite checar "risco de verba duplicada" por
-- solicitacao_id individual, I/O Matrix da spec, sem precisar desmontar um
-- JSON de lote). `lote_id` agrupa as linhas de uma mesma chamada bem-
-- sucedida de GerarLoteDespesa (mesmo valor repetido entre as linhas de um
-- lote) — gerado em Go via `SELECT gen_random_uuid()` DENTRO da transação
-- de internal/exportacao.gravarLote, nunca como DEFAULT desta coluna (um
-- DEFAULT geraria um valor DIFERENTE por INSERT; este lote precisa do
-- MESMO valor para todas as suas linhas).
--
-- Idempotência (Boundaries "Always" da spec 4.3): UNIQUE (administrador_id,
-- chave_idempotencia, solicitacao_id) garante que repetir a mesma chave
-- para a mesma solicitação nunca insere uma 2ª linha — o INSERT de
-- internal/exportacao usa ON CONFLICT DO NOTHING sobre exatamente esta
-- constraint. Índice SEPARADO em solicitacao_id (a UNIQUE acima não serve
-- de índice útil para buscar só por solicitacao_id, já que
-- administrador_id é a primeira coluna) — é o lookup de "verba duplicada":
-- toda solicitação do lote que já tenha uma linha status='GERADO' de uma
-- chave_idempotencia DIFERENTE da atual.
CREATE TABLE IF NOT EXISTS exportacoes_sap (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lote_id UUID NOT NULL,
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    administrador_id UUID NOT NULL REFERENCES usuarios(id),
    chave_idempotencia VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'GERADO' CHECK (status IN ('GERADO', 'FINALIZADO')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (administrador_id, chave_idempotencia, solicitacao_id)
);

CREATE INDEX IF NOT EXISTS idx_exportacoes_sap_solicitacao_id ON exportacoes_sap (solicitacao_id);

-- `solicitacao_comentarios.tipo` ganha 'aviso_verba_duplicada' (Story 4.3)
-- — 1 comentário por solicitação em risco, gravado só quando o
-- administrador explicitamente ignora o aviso (I/O Matrix da spec: nada é
-- gravado quando o aviso NÃO é ignorado). Não existe "ADD VALUE" para um
-- CHECK (ao contrário de um enum nativo do Postgres) — por isso a
-- constraint é recriada do zero com o valor novo, mesmo padrão que a
-- Story 4.2 já teria usado se precisasse (ali só foi preciso CREATE, não
-- havia constraint anterior).
ALTER TABLE solicitacao_comentarios DROP CONSTRAINT solicitacao_comentarios_tipo_check;
ALTER TABLE solicitacao_comentarios ADD CONSTRAINT solicitacao_comentarios_tipo_check
    CHECK (tipo IN ('pendencia', 'comentario', 'aviso_verba_duplicada'));

-- AD-4 (mesmo motivo do REVOKE em `solicitacoes`, migration 010): só a
-- conexão PRIVILEGIADA (fb_apu05_privilegiado, usada exclusivamente por
-- internal/exportacao) pode gravar em exportacoes_sap. `fb_apu05_app`
-- herdaria ALL PRIVILEGES nesta tabela nova via ALTER DEFAULT PRIVILEGES
-- (migration 010) se nada fosse feito aqui — por isso o REVOKE ALL
-- explícito abaixo, não uma lista de operações.
REVOKE ALL ON exportacoes_sap FROM fb_apu05_app;
GRANT SELECT, INSERT, UPDATE ON exportacoes_sap TO fb_apu05_privilegiado;

-- internal/exportacao (ExportadorVBA.GerarLoteDespesa) precisa ler estas
-- tabelas pela conexão PRIVILEGIADA para montar o `.xlsm`: linhas de
-- solicitacao_lancamentos + os códigos de negócio de
-- contas/centros_custo/divisoes/classes_imobilizado que entram na planilha
-- "SAP Export" (nunca os UUIDs internos, Design Notes da spec), e o nome
-- do solicitante em usuarios (rastreabilidade humana no arquivo). Nenhuma
-- destas 6 tabelas tinha GRANT explícito para fb_apu05_privilegiado até
-- aqui (só fb_apu05_app, via ALTER DEFAULT PRIVILEGES da migration 010) —
-- `solicitacoes` já tinha SELECT concedido pela migration 010 e por isso
-- não está nesta lista.
GRANT SELECT ON solicitacao_lancamentos, contas, centros_custo, divisoes, classes_imobilizado, usuarios TO fb_apu05_privilegiado;
