# Review — Rubric Walker (Good-Spine Checklist)

**Target:** `ARCHITECTURE-SPINE.md` (FB_APU05 — Módulo Controladoria)
**Checklist:** `.claude/skills/bmad-architecture/references/reviewer-gate.md` § "Good-spine checklist"
**Deterministic lint:** `lint_spine.py` → `ok: true`, 0 findings (placeholders, duplicate AD IDs, missing Binds/Prevents/Rule, unpinned Stack versions all clean).

**Verdict:** Solid, well-sourced domain core (AD-1 through AD-8 genuinely ratify the PRD and prevent the divergences they name), but the operational envelope and two PRD-flagged highest-risk mechanisms (SLA engine, XLSM binary generation) are under-decided, and AD-10 mischaracterizes the sibling system's real deployment topology — conditional pass, not ready to hand to epics without fixes.

---

## Method

Cross-checked every `AD` and every Deferred bullet against: the PRD (`prd.md` + `addendum.md`), the Brief (`brief.md` + `addendum.md`), the two reference docs (`01-STACK-E-PADROES-FB_APU02.md`, `02-REQUISITOS-NEGOCIO-ENVIO-TI.md`), and — where the spine claims something is "verified by direct inspection" of the sibling system — the actual FB_APU02 repository at `/home/claudio/projetos/FB_APU02` (go.mod, package.json, docker-compose files, `.github/workflows/*.yml`, `docs/deployment-guide.md`, `backend/iam/*.go`, `backend/handlers/auth_sso.go`, `git log`).

---

## Findings

### HIGH-1 — AD-10 (Deploy) mischaracterizes the sibling system it claims to replicate

**Location:** AD-10 (lines 86–90) + Structural Seed "Deployment & Environments" diagram (lines 132–140).

AD-10's Rule states: *"Coolify (Docker + Traefik) para ambiente de teste; GitHub Actions testa, builda imagens e publica (GHCR), deploy self-hosted em servidor AWS do cliente com health-check — mesmo pipeline do FB_APU02."* Marked `[ADOPTED]`, i.e. asserted as already-decided fact, with "Prevents: um segundo pipeline de deploy divergente do FB_APU02."

Verified against the real FB_APU02 repo, this inverts the sibling's actual roles:
- `docs/deployment-guide.md` (FB_APU02's own authoritative doc): Coolify is explicitly the **production** orchestrator (`fctax.fcxlabs.com`, `apuracao.fbtax.cloud` — both prod domains), not a test environment.
- `.github/workflows/deploy-production.yml` is titled **"Deploy to Production (Coolify + Hostinger)"**.
- `.github/workflows/deploy-staging.yml` is titled **"Deploy to STAGING (Azure)"**, triggered on `v*-rc*` tags — a third environment the spine never mentions at all.
- `.github/workflows/deploy-cliente-aws.yml` is not an *alternative* to Coolify; it's `workflow_run`-chained to fire **after** `deploy-production.yml` succeeds, pulling the same GHCR images to the client's own self-hosted AWS box (`172.16.249.77:3006`) as a secondary target.

So the real shape is: Coolify = primary production; Azure = staging/test; client-AWS = secondary production mirror, chained after Coolify. The spine's model (Coolify=test, AWS=prod) matches none of the three. This also pre-empts PRD Questão Aberta 4 ("Topologia de hospedagem, domínio, proxy, observabilidade, SLA técnico" — left open by the PRD) with an `[ADOPTED]` claim that isn't actually backed by the inspection it cites. Epics built against this AD risk wiring the wrong environment as "test" and missing a staging pipeline entirely.

**Fix suggestion:** Either (a) re-verify and correct AD-10's Rule against `docs/deployment-guide.md` + the three workflow files, or (b) downgrade AD-10 from `[ADOPTED]` to a Deferred/open item consistent with PRD Questão Aberta 4, since the topology genuinely isn't settled yet.

### HIGH-2 — SLA/business-calendar engine: a cross-cutting dimension left completely silent

**Location:** absent from AD-1…AD-10, Consistency Conventions (lines 108–114), Capability → Architecture Map (lines 178–189), and Deferred (lines 191–199).

PRD §5.1 (line 259–260): every solicitação carries a 48-business-hour SLA, computed against a holiday calendar (PE/federal, `FR-16` registry) in `America/Recife`, consumed by at least two épicos — Fila do Administrador (FR-12–14, queue sorting/aging) and Painéis (FR-17, "volume by status" presumably includes SLA breach state). This is structurally the same kind of shared-computation divergence risk that AD-2/AD-3/AD-4 exist to close for aprovação/exportação (five handlers that must not each reimplement a rule) — yet no equivalent single-port decision exists for the SLA clock. Nothing says which module owns "is this solicitação late," whether it's computed on read or materialized, or how `FR-16`'s editable holiday registry is wired into it.

Note this is distinct from PRD Open Question 10 (the SLA **pause rule**, a business decision correctly left open) — the architectural placement of the calendar/SLA computation itself is a different, unaddressed question, and the spine conflates "business rule not finalized" with "no architecture needed here."

**Fix suggestion:** Add an AD (or at minimum a Consistency Convention row) naming a single SLA/business-calendar module that Fila do Administrador and Painéis both call, and add a Capability Map row/reference for it.

### HIGH-3 — XLSM/VBA binary-generation technique: the PRD's own named highest-risk mechanism, left undecided

**Location:** FR-15 feature NFR (PRD line 233) + addendum lines 14–19, vs. AD-3 (lines 44–48), Stack (lines 116–128), Structural Seed directory tree (lines 153–176).

The Design Paradigm section itself calls exportação "o maior risco de esforço do produto" (line 25, citing PRD §5.3). The addendum documents four specific, previously-shipped bugs from the legacy generator (regex-unsafe string substitution corrupting Excel formulas, stale `calcChain.xml` forcing repair dialogs, a hidden-sheet macro failure, and the hard constraint to never touch `vbaProject.bin`). AD-3 assigns `ExportadorVBA` to `backend/internal/exportacao` (Go), per the directory tree — a different runtime than the reference app's browser-side `sapExport.js`/`xlsx`-based generator this logic was ported from. Yet:
- The Stack table (lines 118–128) lists no OOXML/ZIP manipulation library for Go (the frontend `xlsx` package FB_APU02 actually uses isn't carried over, nor is a Go equivalent named).
- No AD or Deferred bullet addresses *how* byte-exact `vbaProject.bin` preservation, calcChain removal, or string-escaping will be implemented in the new runtime.

This is exactly the kind of named, high-stakes technical unknown the altitude should either decide or explicitly open as a question — right now it's neither; it's silent.

**Fix suggestion:** Add an open item under Deferred naming the Go-side OOXML approach as undecided (or decide it, e.g. "manipulate the .xlsm as a ZIP/XML archive via `archive/zip` + raw part rewrites, never touch the `vbaProject.bin` part, route all user text through an escape function before substitution").

### MEDIUM-1 — Anexos (attachments): a multi-épico capability with no AD, no Capability Map row

**Location:** Deferred bullet 1 (line 193) is the only mention.

FR-8 (Imobilizado) makes at least one anexo mandatory ("Solicitação sem ao menos um anexo (cotação) não pode ser enviada," PRD line 160); the general request journey (UJ-1, PRD line 34) also attaches files; the data model carries a dedicated `solicitacao_anexos` table (reference doc line 21). This spans multiple of the five request-type handlers. The spine's only treatment is Deferred bullet 1, which scopes *just* "antivírus/DLP, quarentena" (PRD Questão Aberta 3) — the advanced security layer — not the base mechanism (storage location, metadata+hash convention, one shared upload module vs. five independent ones). Without a Capability Map row or AD, nothing stops each request-type handler from inventing its own upload/storage convention.

**Fix suggestion:** Add a Capability Map row for anexos (governed by a new or existing AD, e.g. extend AD-1's "handlers never do X directly" pattern to attachment storage) distinct from the already-correctly-deferred AV/DLP question.

### MEDIUM-2 — Stack table is materially incomplete relative to AD-9's own claim

**Location:** AD-9 (lines 80–84) + Stack table (lines 116–128).

AD-9's Rule says versions are "idênticas às do FB_APU02 em produção ativa ... ver tabela Stack," positioning the table as the authoritative inherited-stack record. But `01-STACK-E-PADROES-FB_APU02.md` (§1, lines 7–9) documents several libraries in real production use that the Stack table omits entirely: React Router DOM 6.22.3, TanStack Query 5.90, React Hook Form 7.71 + Zod 4.3.6, Recharts, Sonner, `xlsx`, and the backend's actual Postgres driver `github.com/lib/pq` v1.11.2. Given this is a forms-heavy, five-request-type domain, form handling and data-fetching are exactly where independent épicos would otherwise diverge — the same divergence class AD-9 exists to close. Confirmed still current in FB_APU02's `go.mod`/`package.json` as of commit `f561a64` (2026-10-05, matching AD-9's citation).

**Fix suggestion:** Extend the Stack table with the omitted libraries, or explicitly note in AD-9 that only backend-core/infra versions are pinned here and frontend data/forms libraries are intentionally left to a later layer (and say where).

### MEDIUM-3 — AD-4's "proibido UPDATE direto" has no enforcement backstop, and the sibling system has a documented history of exactly this failure mode

**Location:** AD-4 (lines 50–54).

The Rule ("toda escrita nesses campos passa exclusivamente pelos módulos... proibido UPDATE direto") is stated as a hard prevention but has no technical gate named (no DB trigger, no restricted grants, no lint rule) — it relies entirely on code-review discipline. `01-STACK-E-PADROES-FB_APU02.md` §2 (line 24) documents that FB_APU02 already has this exact failure mode for its analogous invariant: "Multi-tenancy por `company_id`... RLS existe mas não é aplicado em produção — anti-padrão documentado, evitar repetir no FB_APU05." AD-4 is at real risk of becoming the same kind of declared-but-unenforced invariant unless it names a concrete mechanism.

**Fix suggestion:** Either add a lightweight DB-level backstop (e.g., `REVOKE UPDATE (aprovador_snapshot, versao, ...) ON solicitacoes FROM app_role` + a dedicated role/grant for the domain module's DB user) or explicitly note the reliance on code review and point to it as a watch-item for retros.

### LOW-1 — golang-jwt pinned loosely

**Location:** Stack table, line 126.

Table says `v5`; FB_APU02's `go.mod` pins `v5.3.1`. Passes `lint_spine.py` (which doesn't flag major-only pins), and is low-stakes since `go.sum` will lock the real version — but sits oddly next to AD-9's claim of "versões idênticas," which implies patch-level fidelity was actually checked. Tighten to `v5.3.1` for consistency with the AD-9 narrative.

### LOW-2 — Capability Map row for Painéis doesn't carry its own read-only invariant

**Location:** Capability → Architecture Map, row "4.8 Painéis e Indicadores" (line 189).

FR-17's testable consequence ("Painéis leem de snapshots pré-calculados, não de consulta direta às tabelas transacionais," PRD line 253) is a real architectural constraint, but the row's "Governado por" column is `—` and nothing elsewhere names it. Not a blocker (PRD §7.3 already defers the full scope), but once the scope is set this constraint should get an AD or convention entry rather than living only in the PRD.

### LOW-3 — Deploy pipeline count ambiguity compounds HIGH-1

**Location:** Structural Seed diagram (lines 132–140).

The diagram shows exactly two stages (Coolify "teste" → AWS "produção"). Given HIGH-1, once AD-10 is corrected the diagram will likely need a third lane for whatever FB_APU05's actual staging/rc pipeline turns out to be. Flagged here only so the diagram isn't patched piecemeal when AD-10 is fixed.

---

## Checklist coverage summary

| Checklist criterion | Verdict |
| --- | --- |
| Fixes real divergence points for epics, misses none | Partial — aprovação/exportação/lock/auth/stack/single-tenant are solidly fixed (AD-1–AD-9); SLA engine, anexos, and XLSM-generation technique are missed (HIGH-2, HIGH-3, MEDIUM-1) |
| Every AD's Rule is enforceable and prevents its stated divergence | Mostly yes; AD-4 lacks a technical backstop (MEDIUM-3) |
| Nothing in Deferred lets two units diverge silently | Mostly yes; the anexos Deferred bullet is scoped too narrowly, leaving the base mechanism un-decided (MEDIUM-1) |
| Named tech verified-current | **Pass** — Go 1.22, PostgreSQL 15, React 18.3.1, TypeScript 5.2.2, Vite 5.2, Tailwind 3.4.x, bcrypt cost 14 all confirmed byte-for-byte against FB_APU02's `go.mod`/`package.json`/`docker-compose.yml`; AD-9's cited commit date (2026-10-05) matches `git log` exactly. Only gap is completeness, not currency (MEDIUM-2) |
| Ratifies rather than contradicts the brownfield FB_APU02 code | Mostly yes — AD-6 (Keycloak/`email_verified`), AD-7 (company_id), AD-9 all verified accurate against actual source; AD-10 does not (HIGH-1) |
| Covers the PRD's Capability → Architecture Map capabilities | Formally complete (all 8 PRD feature groups / 17 FRs mapped), but two sub-capabilities embedded within those groups (SLA, anexos) aren't actually governed by anything (HIGH-2, MEDIUM-1) |
| Every dimension this altitude owns is decided/deferred/open — especially the operational/environmental envelope | Deployment & environments section exists but is factually wrong (HIGH-1); most other operational gaps (HA/backup, rollback, hosting topology/SLA técnico, anexo AV/DLP) are properly carried into Deferred as PRD-sourced open questions — that part of the envelope is handled correctly |

## Findings by severity

- Critical: 0
- High: 3
- Medium: 3
- Low: 3
