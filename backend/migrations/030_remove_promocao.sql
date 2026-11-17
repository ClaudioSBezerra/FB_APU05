-- Remove o "Solicitar promoção" (17/11/2026): a promoção para Atendente é manual, feita por um
-- administrador em Gestão de Usuários. As migrations 028/029 já foram aplicadas, então a limpeza vem
-- numa migration nova. A tabela só é apagada se estiver vazia (nunca descarta pedidos gravados).
-- A contagem usa SQL dinâmico: assim a migration também roda sem erro quando a tabela já não existe.
DO $$
DECLARE linhas bigint;
BEGIN
    IF to_regclass('usuarios_promocao_pedidos') IS NOT NULL THEN
        EXECUTE 'SELECT count(*) FROM usuarios_promocao_pedidos' INTO linhas;
        IF linhas = 0 THEN
            DROP TABLE usuarios_promocao_pedidos;
        END IF;
    END IF;
END $$;

DELETE FROM configuracoes WHERE chave = 'promocao_email_extra';
