# Epic 5 Context: Painéis e Indicadores

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Give any authenticated user a consolidated view of request activity — volume by request type, by status, and by cost center (CC) — so they can track progress without opening each request individually. This closes the loop on the system's core workflow (open → approve → process → export) by surfacing aggregate outcomes, and it must do so without adding load to the transactional tables that the rest of the system depends on.

## Stories

- Story 5.1: Visualizar painel consolidado de solicitações

## Requirements & Constraints

- Panels must read exclusively from pre-calculated snapshots — never aggregate directly from live transactional tables.
- Minimum required indicators for this version: volume by request type, by status, and by cost center (CC). This is a floor, not a ceiling — any additional indicators are explicitly deferred to a future iteration and require business sign-off first (open item in the source planning docs: exact indicator scope beyond this minimum was never closed with the business).
- Panels are read-only for all authenticated users (no admin-only restriction called out for this feature).
- SLA/"is this late" status shown in any panel must come from the shared SLA module, not be recalculated locally (see Technical Decisions).

## Technical Decisions

- Panel data is served from a dedicated snapshot table (`painel_snapshots`), populated by a pre-calculation process — handlers for this epic only read from it, they never query or aggregate the transactional `solicitacoes`-family tables directly.
- Panel responses use their own aggregated format, distinct from the standard paginated list contract (`{items, pagina, tamanho}`) used everywhere else in the system. A panel is a set of pre-computed values, not a paginable list — the two response shapes must not be conflated.
- Any "is this request late?" / deadline logic shown in a panel must call the single shared SLA module (`EstaAtrasado`, `PrazoLimite`), the same one used by the admin queue (Epic 4). Panels must not implement their own copy of the business-day/holiday-calendar calculation.
- Handlers for this feature are read-only against snapshot data; no write path is introduced here.

## Cross-Story Dependencies

- Depends on the SLA/calendar module (shared with Epic 4's admin queue) for any deadline-related indicator — that module is Epic 4's responsibility, not built here.
- Depends on `painel_snapshots` being populated upstream; this epic only implements the read/display side, not the snapshot-calculation job itself (the job's trigger/schedule is not specified in the available planning artifacts).
- The exact set of indicators beyond the stated minimum (type/status/CC volume) is explicitly not finalized with the business — do not expand scope without confirming first.
