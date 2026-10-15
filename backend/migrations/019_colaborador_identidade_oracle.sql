-- Identidade de colaborador por código de empresa + matrícula (NUMCAD),
-- para casar cada usuário do FB_APU05 com sua linha em
-- SFC.SFC_VW_COLABORADORES (Oracle, servidor FCCORP) sem depender de e-mail
-- (a view não tem e-mail — só COD_EMPRESA, CENTRO_CUSTO, NUMCAD, NOMFUN).
--
-- Nullable: usuários provisionados via SSO (Story 1.2) continuam nascendo
-- sem esses campos; um administrador preenche depois (tela de Gestão de
-- Usuários) para que a sincronização diária passe a alcançá-los.
--
-- UNIQUE parcial (só quando os dois estão preenchidos): impede dois
-- usuários do FB_APU05 reivindicarem a mesma matrícula/empresa — collision
-- aqui significaria duas contas recebendo o CC de uma mesma pessoa física.
ALTER TABLE usuarios
    ADD COLUMN IF NOT EXISTS cod_empresa INTEGER,
    ADD COLUMN IF NOT EXISTS numcad      INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS idx_usuarios_cod_empresa_numcad
    ON usuarios (cod_empresa, numcad)
    WHERE cod_empresa IS NOT NULL AND numcad IS NOT NULL;
