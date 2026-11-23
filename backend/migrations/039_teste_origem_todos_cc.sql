-- Fase de testes (23/11/2026, pedido do Wilker): na TRANSFERÊNCIA, a origem pode ser qualquer centro de custo.
-- Ligado agora; desligue em Configurações quando a regra definitiva for decidida (volta a valer só o CC próprio + exceções).
INSERT INTO configuracoes (chave, valor) VALUES ('teste_origem_todos_cc', 'true')
ON CONFLICT (chave) DO NOTHING;
