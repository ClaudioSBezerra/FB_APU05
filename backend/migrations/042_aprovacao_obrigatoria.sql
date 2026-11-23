-- 23/11/2026 (sugestão do Marlos): hoje o atendimento recebe a autorização do gerente por fora e documenta.
-- Desligada (padrão): a alçada deixa de ser obrigatória e TODA solicitação entra na fila do atendente.
-- Ligada: volta o fluxo anterior (alçada obrigatória; transferência > limite e imobilizado aguardam aprovação por e-mail).
INSERT INTO configuracoes (chave, valor) VALUES ('aprovacao_obrigatoria', 'false')
ON CONFLICT (chave) DO NOTHING;
