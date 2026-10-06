# FB_APU05 — Módulo Controladoria (Ferreira Costa)

Sistema de gestão de solicitações orçamentárias/investimento (transferência, inclusão, inclusão SFC, imobilizado e obras), com fluxo de aprovação por alçadas e exportação para SAP.

## Status

Projeto em fase de kickoff. Gestão conduzida via [BMAD Method](https://bmadcode.com/) (`_bmad/`) e Kanban no [GitHub Projects #6](https://github.com/users/ClaudioSBezerra/projects/6).

## Documentação

- `docs/referencia/01-STACK-E-PADROES-FB_APU02.md` — stack, segurança, padrão de telas e banco de dados de referência (projeto irmão FB_APU02).
- `docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md` — síntese dos requisitos de negócio recebidos da área de TI/Controladoria.
- `_bmad-output/planning-artifacts/` — Project Brief, PRD, Arquitetura e Epics gerados pelo BMAD (conforme as fases avançam).

## Stack (decisão inicial)

- Backend: Go + PostgreSQL (SQL puro, sem ORM), seguindo os padrões já validados em produção no FB_APU02.
- Frontend: React + TypeScript + Vite + shadcn/ui.
- Infra: Docker + Coolify/Traefik.

Decisões detalhadas e justificativas serão registradas no PRD e na Arquitetura conforme o BMAD avança pelas fases Analyst → PM → Architect → Epics.

## BMAD

Instalado via `npx bmad-method` (v6.12.1), módulos: `core`, `bmm`, `bmb`, `cis`, `bmad-loop`, `tea`. Skills disponíveis em `.claude/skills/` (gerado localmente, não versionado — rode `npx bmad-method install` para recriar).
