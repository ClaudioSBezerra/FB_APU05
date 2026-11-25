-- Eventos do atendimento no histórico (retorno da homologação, item 16): assumiu e reabriu.
ALTER TABLE solicitacao_comentarios DROP CONSTRAINT IF EXISTS solicitacao_comentarios_tipo_check;
ALTER TABLE solicitacao_comentarios ADD CONSTRAINT solicitacao_comentarios_tipo_check
    CHECK (tipo IN ('pendencia', 'comentario', 'encerramento', 'evento'));
