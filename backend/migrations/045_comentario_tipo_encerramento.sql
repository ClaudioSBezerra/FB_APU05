-- Finalização registra um evento no histórico (retorno da homologação, itens 1 e 16): tipo 'encerramento'.
ALTER TABLE solicitacao_comentarios DROP CONSTRAINT IF EXISTS solicitacao_comentarios_tipo_check;
ALTER TABLE solicitacao_comentarios ADD CONSTRAINT solicitacao_comentarios_tipo_check
    CHECK (tipo IN ('pendencia', 'comentario', 'encerramento'));
