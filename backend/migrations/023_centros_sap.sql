-- Centros (21/10) — carga via API SAP S/4HANA (pedido do Marlos), a ser
-- empurrada pelo lado ABAP (Maria Eduarda/Duda) contra POST
-- /api/erp-bridge/centros/sincronizar — ver handlers/centros_sap_sync.go.
--
-- "Centro" aqui é o conceito SAP de Plant/Werk (filial/local físico),
-- DIFERENTE de `divisoes` (agrupamento de centros de custo, migration 003)
-- e de `centros_custo` (que já tem sua própria carga CSV manual) — é uma
-- entidade nova, sem nenhuma relação de FK com as duas.
CREATE TABLE IF NOT EXISTS centros (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) NOT NULL UNIQUE,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
