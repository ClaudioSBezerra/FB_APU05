-- Perfil "atendente" (17/11/2026): opera a Fila de atendimento (assumir, pendência, finalizar,
-- gerar lote) e abre Nova solicitação; não administra usuários, cadastros nem integrações.
-- "administrador" continua sendo o administrador do ambiente (tudo, inclusive a fila).
--
-- O CHECK de usuarios.perfil nasceu na migration 001 (solicitante | administrador). Como o nome
-- da constraint não foi fixado lá, localiza e troca por catálogo — idempotente.
DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT c.conname
        FROM pg_constraint c
        WHERE c.conrelid = 'usuarios'::regclass
          AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) ILIKE '%perfil%'
          AND pg_get_constraintdef(c.oid) NOT ILIKE '%atendente%'
    LOOP
        EXECUTE format('ALTER TABLE usuarios DROP CONSTRAINT %I', r.conname);
    END LOOP;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint c
        WHERE c.conrelid = 'usuarios'::regclass AND c.conname = 'usuarios_perfil_check'
    ) THEN
        ALTER TABLE usuarios
            ADD CONSTRAINT usuarios_perfil_check
            CHECK (perfil IN ('solicitante', 'atendente', 'administrador'));
    END IF;
END $$;
