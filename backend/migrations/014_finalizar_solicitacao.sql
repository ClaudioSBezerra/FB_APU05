-- Story 4.5 — Finalizar solicitação (Epic 4, FR-14, AD-3/AD-4/AD-5).
--
-- internal/exportacao.ExportadorVBA.Finalizar (finalizar.go) grava 2
-- colunas novas pela conexão PRIVILEGIADA que ainda não tinham GRANT:
-- INSERT em `obra_ordens` (a ordem real nasce só aqui, na finalização —
-- comentário da própria migration 009) e UPDATE da coluna
-- `ordem_investimento` em `solicitacao_obras_linhas` (hoje só tem SELECT,
-- concedido pela migration 013 para a Story 4.4). Nenhum GRANT novo em
-- `solicitacoes`/`exportacoes_sap` — a escrita de status/versao
-- (migration 010) e a migração GERADO->FINALIZADO (migration 012) já
-- estão cobertas pelos GRANTs existentes.
GRANT INSERT ON obra_ordens TO fb_apu05_privilegiado;
GRANT UPDATE (ordem_investimento) ON solicitacao_obras_linhas TO fb_apu05_privilegiado;
