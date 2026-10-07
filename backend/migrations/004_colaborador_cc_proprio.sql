-- Story 2.2 — Carga de colaborador × centro de custo (FR-3, AD-8).
--
-- `usuarios.cc_proprio_id`: o CC próprio do colaborador, vindo da carga
-- manual da base Senior (POST /api/admin/colaboradores/carga). Nullable —
-- "sem CC próprio" é um estado válido: um colaborador recém-provisionado via
-- SSO (Story 1.2) nasce sem CC, e só passa a ter um depois que uma carga de
-- colaboradores casar seu e-mail (ver Design Notes da spec desta story:
-- "aguardando_primeiro_login"/UPDATE-only, nunca INSERT).
--
-- Sem ON DELETE (deixa o default RESTRICT): um centro de custo referenciado
-- como CC próprio de algum colaborador não pode ser apagado silenciosamente
-- por baixo — mesma postura conservadora já adotada em
-- `centros_custo.divisao_id` (migration 003).
ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS cc_proprio_id UUID REFERENCES centros_custo(id);

CREATE INDEX IF NOT EXISTS idx_usuarios_cc_proprio_id ON usuarios (cc_proprio_id);
