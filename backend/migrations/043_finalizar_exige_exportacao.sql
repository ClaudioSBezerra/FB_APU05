-- Enquanto o robô SAP (.xlsm) não está ligado, não há como gerar lote; por isso finalizar com SUCESSO não exige
-- exportação por padrão. Ligar a chave volta a exigir (finalizar com erro nunca exige).
INSERT INTO configuracoes (chave, valor) VALUES ('finalizar_exige_exportacao', 'false')
ON CONFLICT (chave) DO NOTHING;
