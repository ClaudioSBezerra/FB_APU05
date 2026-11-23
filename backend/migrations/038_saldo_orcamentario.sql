-- Saldo orçamentário (API de Trava orçamentária, 23/11/2026): serviço "bg" nas Configurações do SAP.
-- Consulta NA HORA (não é leitura das 06:00). Desligado até a URL/credencial estarem prontas. QAS por padrão.
INSERT INTO configuracoes (chave, valor) VALUES
    ('sap_bg_ativo',          'false'),
    ('sap_bg_url_base',       'https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_trava_orcamentaria/srvd_a2x/sap/zsd_trava_orcamentaria/0001'),
    ('sap_bg_ultima_execucao',  ''),
    ('sap_bg_ultimo_resultado', '')
ON CONFLICT (chave) DO NOTHING;
