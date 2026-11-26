-- Realizado (KSB1) e Compromissado (KSB2) pelas APIs do SAP (documentação da TI SAP, nov/2026): serviços "k1" e "k2"
-- em Configurações → Integração SAP. Nascem DESLIGADOS com a URL de QAS do documento; quando a rota do gateway (Kong)
-- for liberada, troca-se a URL e a chave no card de cada um.
INSERT INTO configuracoes (chave, valor) VALUES
    ('sap_k1_ativo',            'false'),
    ('sap_k1_url_base',         'https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_cc_actual/srvd_a2x/sap/zsd_cc_actual/0001'),
    ('sap_k1_ultima_execucao',  ''),
    ('sap_k1_ultimo_resultado', ''),
    ('sap_k2_ativo',            'false'),
    ('sap_k2_url_base',         'https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_cc_commitment/srvd_a2x/sap/zsd_cc_commitment/0001'),
    ('sap_k2_ultima_execucao',  ''),
    ('sap_k2_ultimo_resultado', '')
ON CONFLICT (chave) DO NOTHING;
