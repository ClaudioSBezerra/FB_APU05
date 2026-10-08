-- Story 4.1 — Assumir solicitação da fila (fecha o comentário deixado pela
-- migration 006: "a primeira story que fizer UPDATE nessas colunas é quem
-- deve introduzir a separação de roles").
--
-- (a) `administrador_id`: quem assumiu a solicitação da fila — preenchido
-- exclusivamente por internal/fila.Assumir (Boundaries "Always" da spec:
-- nunca direto em handler). Nulável: toda solicitação nasce sem
-- administrador, só ganha um ao ser assumida.
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS administrador_id UUID REFERENCES usuarios(id);

CREATE INDEX IF NOT EXISTS idx_solicitacoes_administrador_id ON solicitacoes (administrador_id);

-- A listagem da fila (ListarFilaHandler) filtra `status = 'aberta'` e
-- ordena por `created_at` — mesmo padrão das colunas de FK já indexadas
-- nesta tabela (migration 006: solicitante_id, centro_custo_id).
CREATE INDEX IF NOT EXISTS idx_solicitacoes_status_created_at ON solicitacoes (status, created_at);

-- (b) Separação de roles (AD-4) — backstop TÉCNICO, não convenção de code
-- review. Por que a role atual (POSTGRES_USER/DATABASE_URL) não serve para
-- o lado restrito: ela é owner das tabelas e, no Postgres, owners (e
-- superusers) ignoram checagem de GRANT/REVOKE nos seus próprios objetos —
-- revogar UPDATE dela não teria efeito nenhum (Design Notes da spec). Por
-- isso o hot-path geral da aplicação migra para uma role NOVA e
-- não-superuser (`fb_apu05_app`); a role antiga continua existindo só para
-- rodar migrations (ela é quem executa este arquivo agora).
--
-- Senha dev default 'changeme' — mesmo padrão de JWT_SECRET/ENCRYPTION_KEY
-- (.env.example). Esta migration é texto estático (não lê env var em tempo
-- de execução, ao contrário do backend em Go); em produção, rotacionar com
-- `ALTER ROLE ... PASSWORD` manualmente depois do deploy, mantendo
-- DATABASE_URL_APP/DATABASE_URL_PRIVILEGIADA em sincronia com o valor
-- rotacionado.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        CREATE ROLE fb_apu05_app LOGIN PASSWORD 'changeme';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_privilegiado') THEN
        CREATE ROLE fb_apu05_privilegiado LOGIN PASSWORD 'changeme';
    END IF;
END
$$;

-- `fb_apu05_app` espelha o acesso irrestrito que a role antiga tinha (por
-- ser superuser/owner) em todas as tabelas já existentes...
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO fb_apu05_app;

-- ...e nas tabelas de migrations FUTURAS: ALTER DEFAULT PRIVILEGES sem
-- "FOR ROLE" aplica à role que EXECUTA este comando — a mesma role que roda
-- todas as migrations via DATABASE_URL (onDBConnected, main.go) — então
-- toda CREATE TABLE de uma migration futura já nasce com GRANT ALL para
-- fb_apu05_app, sem precisar repetir este bloco em cada migration nova.
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO fb_apu05_app;

-- O backstop técnico do AD-4 em si: só a conexão PRIVILEGIADA
-- (fb_apu05_privilegiado, usada exclusivamente por internal/fila) pode
-- gravar nestas 4 colunas de `solicitacoes` — qualquer UPDATE vindo da
-- conexão geral da aplicação (fb_apu05_app) é recusado pelo PRÓPRIO BANCO,
-- nunca só por revisão de código.
--
-- REVOKE UPDATE em LISTA DE COLUNAS (ex. "REVOKE UPDATE (status, ...) ON
-- solicitacoes FROM fb_apu05_app") NÃO seria suficiente aqui e foi
-- verificado manualmente contra um Postgres real antes de fechar esta
-- migration: o GRANT ALL PRIVILEGES acima concede UPDATE no nível da
-- TABELA (entrada de ACL própria, cobrindo todas as colunas, inclusive
-- futuras); um REVOKE por coluna só remove a entrada de ACL por coluna,
-- nunca essa entrada de tabela — o UPDATE continuaria permitido via ela.
-- Por isso o REVOKE abaixo é da tabela inteira (nenhum handler existente
-- faz UPDATE em `solicitacoes` por nenhuma coluna — todas as escritas
-- atuais são INSERT, Code Map da spec), não uma lista de colunas.
REVOKE UPDATE ON solicitacoes FROM fb_apu05_app;

GRANT SELECT, UPDATE (aprovador_snapshot, versao, status, administrador_id) ON solicitacoes TO fb_apu05_privilegiado;
