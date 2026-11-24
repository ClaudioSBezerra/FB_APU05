-- Data-limite (MM-DD) até a qual, no começo do ano, ainda se pode movimentar o orçamento do ano ANTERIOR
-- (fechamento de janeiro). Fora disso só o ano corrente. Ajustável em Configurações, sem deploy.
INSERT INTO configuracoes (chave, valor) VALUES ('orcamento_ano_anterior_ate', '01-31')
ON CONFLICT (chave) DO NOTHING;
