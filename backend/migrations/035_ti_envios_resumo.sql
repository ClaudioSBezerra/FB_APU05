-- Controle de Despesas: registro dos resumos (PDF) enviados por e-mail pelo ADM — quem enviou, para quem e quando.
-- O PDF em si NÃO é guardado (só nome e tamanho): contém dados de gasto por gestor.
CREATE TABLE IF NOT EXISTS ti_envios_resumo (
    id           BIGSERIAL    PRIMARY KEY,
    usuario_id   UUID         REFERENCES usuarios(id),
    destinatario VARCHAR(255) NOT NULL,
    assunto      VARCHAR(255) NOT NULL,
    arquivo      VARCHAR(255) NOT NULL DEFAULT '',
    bytes        INTEGER      NOT NULL DEFAULT 0,
    ok           BOOLEAN      NOT NULL,
    erro         VARCHAR(255) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
