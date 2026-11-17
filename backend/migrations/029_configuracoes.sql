-- Configurações do sistema (17/11/2026): parâmetros editáveis por administradores em
-- Configurações, sem mexer em variável de ambiente.
--   promocao_email_extra   e-mails (separados por ;) que TAMBÉM recebem os pedidos de promoção
--                          (além dos administradores ativos)
--   sap_*                  integração SAP, leitura 1x/dia (padrão 06:00, horário de Recife):
--                          credencial única (usuário técnico Basic) e, por serviço, URL e liga/desliga:
--                          cc = centros de custo (API_COSTCENTER_SRV)
--                          gl = contas contábeis (API_GLACCOUNTINCHARTOFACCOUNTS_SRV)
-- A senha do SAP (sap_senha) é gravada CRIPTOGRAFADA (AES-256-GCM, backend/crypto) e nunca devolvida.
CREATE TABLE IF NOT EXISTS configuracoes (
    chave       VARCHAR(60)  PRIMARY KEY,
    valor       TEXT         NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by  UUID         REFERENCES usuarios(id) ON DELETE SET NULL
);

INSERT INTO configuracoes (chave, valor) VALUES
    ('promocao_email_extra',     ''),
    ('sap_horario',              '06:00'),
    ('sap_usuario',              ''),
    ('sap_senha',                ''),
    ('sap_cc_ativo',             'false'),
    ('sap_cc_url_base',          'https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV'),
    ('sap_cc_ultima_execucao',   ''),
    ('sap_cc_ultimo_resultado',  ''),
    ('sap_gl_ativo',             'false'),
    ('sap_gl_url_base',          'https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_GLACCOUNTINCHARTOFACCOUNTS_SRV'),
    ('sap_gl_ultima_execucao',   ''),
    ('sap_gl_ultimo_resultado',  '')
ON CONFLICT (chave) DO NOTHING;
