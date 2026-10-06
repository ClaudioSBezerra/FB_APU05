---
name: 'Review — Versões do Stack (AD-9)'
type: review
target: architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md#Stack
reviewer: Claude (agente de verificação)
created: 2026-10-06
method: busca na web (não só conhecimento de treinamento), data de referência 2026-10-06
---

# Review — Versões do Stack herdado (AD-9)

Escopo: checar cada tecnologia/versão da tabela **Stack** do Architecture Spine quanto a (1) existência, (2) suporte/razoabilidade atual, (3) grau de defasagem frente ao padrão consolidado do ecossistema, na data de hoje (2026-10-06).

Importante: a premissa de que o FB_APU02 existe e roda essas versões em produção (commit mais recente 2026-10-05) **não foi questionada** — isso já é dado como verificado por inspeção direta (AD-9). O que esta review avalia é se herdar essas versões ainda é uma base tecnológica razoável hoje, dado o estado do ecossistema externo.

## Resumo por item

| Tecnologia | Versão no Spine | Veredito | Defasagem frente ao padrão atual |
| --- | --- | --- | --- |
| Go | 1.22 | ⚠️ Atenção | Sem patches de segurança upstream desde ~fev/2025 |
| PostgreSQL | 15 | ✅ OK | Pequena (dentro da janela de suporte até nov/2027) |
| React | 18.3.1 | ⚠️ Atenção | Moderada/alta (congelado; 19.x é o padrão há ~2 anos) |
| TypeScript | 5.2.2 | ⚠️ Atenção | Alta (3 majors atrás: 6.0 e 7.0 já lançados) |
| Vite | 5.2 | ⚠️ Atenção | Alta (3 majors atrás: Vite 8 é o atual) |
| Tailwind CSS 3.4.x + shadcn/ui | 3.4.x | ✅ OK (com nota) | 1 major atrás, mas v3 não está abandonado nem quebrado |
| golang-jwt | v5 | ✅ OK | Nenhuma — é a major atual |
| bcrypt | custo 14 | ✅ OK | Nenhuma — dentro da faixa recomendada em 2026 (12–14) |

## Detalhamento e fontes

### Go 1.22

- Go 1.22.0 foi lançado em 2024-02-06. Fonte: [Release History](https://go.dev/doc/devel/release).
- Política oficial do projeto Go: cada major release é suportado (recebe correções de segurança) **apenas até existirem duas majors mais novas lançadas**. Fonte: [Release History](https://go.dev/doc/devel/release), confirmado via [endoflife.date/go](https://endoflife.date/go).
- A versão atual em 2026-10-06 é a série **1.26** (1.26.0 lançado em 2026-02-10, com patches até 1.26.4 em 2026-06-02). Fonte: busca web sobre release history do Go.
- Consequência: Go 1.22 está **sem suporte a correções de segurança do time do Go desde o lançamento do Go 1.24** (~fevereiro/2025) — já são duas majors mais novas (1.23, 1.24) superadas, e agora quatro (1.23–1.26).
- Go como linguagem **não está abandonado** — ao contrário, está em desenvolvimento ativo e saudável (GC novo em 1.26, etc.). O problema é específico da versão 1.22, não do ecossistema.
- **Nota para o Architecture Spine:** isso é uma lacuna real de segurança upstream (não hipotética) — vale registrar como débito técnico conhecido e não apenas "versão só um pouco antiga". Se o FB_APU02 também roda 1.22 sem correções desde 2025, essa exposição já existe lá; herdar replica o risco, não o cria.

### PostgreSQL 15

- PostgreSQL 15 foi lançado em outubro/2022, com EOL previsto para **novembro/2027** (ciclo de 5 anos de suporte). Fonte: busca web citando políticas de versionamento do PostgreSQL ([instaclustr.com](https://www.instaclustr.com/education/postgresql/postgres-versions-supported-releases-eol-dates-upgrades/), [herodevs.com](https://www.herodevs.com/blog-posts/postgresql-eol-dates-every-versions-release-end-of-life-timeline)).
- Em 2026, as séries 18, 17, 16, 15 e 14 ainda recebem patches de segurança — PostgreSQL 15 está confortavelmente dentro da janela de suporte.
- Veredito: razoável. Não é a versão mais nova (18 é a atual), mas está longe de ser abandonada e tem mais de um ano de suporte pela frente.

### React 18.3.1

- React 18.3.1 foi lançado em 2024-04-26 e está hoje **"congelado"**, recebendo apenas backports de segurança — React não tem fim de vida rígido por versão, mas 18.x não recebe novas features. Fonte: busca web (versio.io, docs.herodevs.com).
- React 19 é o padrão desde dezembro/2024, e já está em **19.2.x** em 2026 (patches até junho/2026). Fonte: busca web sobre versões do React em 2026.
- Veredito: React **não está abandonado**, mas a versão herdada (18.3.1) está cerca de 2 anos atrás do padrão consolidado do ecossistema (19.x), que já é o que bibliotecas companheiras (ex.: shadcn/ui) tratam como referência atual.

### TypeScript 5.2.2

- TypeScript 5.2.2 é de agosto/2023.
- Em 2026, o estado da arte é **TypeScript 7.0** (lançado 2026-07-08, reescrita do compilador em Go, ~10x mais rápido), precedido por TypeScript 6.0 (2026-03-23) como ponte. Fonte: busca web (codersera.com, i-programmer.info).
- Veredito: defasagem alta — 3 majors atrás do padrão atual (5.2 → 6.0 → 7.0, além de toda a série 5.x intermediária). TypeScript mantém forte compatibilidade retroativa historicamente, então o risco prático é menor do que o número sugere, mas é a maior distância de qualquer item do stack frente ao "consolidado como padrão".

### Vite 5.2

- Vite 5.2 é de 2024.
- Em 2026, o atual é **Vite 8** (8.0.8 em abril/2026, unificando o bundler com Rolldown). Fonte: busca web (voidzero.dev, grokipedia.com).
- Veredito: defasagem alta — 3 majors atrás (5 → 6 → 7 → 8). Vite 5 não está abandonado (ainda é amplamente usado e não há indício de vulnerabilidades não corrigidas), mas não é mais o padrão corrente.

### Tailwind CSS 3.4.x + shadcn/ui

- Tailwind CSS v4 chegou no início de 2025 e em 2026 é o padrão recomendado para **projetos novos**; v3 continua funcional e sem urgência de migração para quem já está nela. Fonte: busca web (dev.to, itsourcecode.com).
- shadcn/ui confirma em 2026 **compatibilidade mantida com Tailwind v3 e v4** — "non-breaking: apps existentes com Tailwind v3 e React 18 continuam funcionando". v4 é a recomendação só para setups novos. Fonte: busca web sobre shadcn/ui e Tailwind v3/v4.
- Veredito: razoável. Está um major atrás, mas diferente de React/TS/Vite, aqui há confirmação explícita do próprio ecossistema (shadcn/ui) de que a combinação v3 + React 18 é suportada ativamente, não apenas tolerada.

### golang-jwt v5

- A major atual é a v5, com release mais recente **v5.3.1** (aceito no Debian unstable em 2026-02-23), projeto ativo e mantido por uma equipe dedicada após a migração do jwt-go original. Fonte: busca web (pkg.go.dev, Debian tracker, ecosyste.ms).
- Veredito: ok — é a versão mais atual da biblioteca, nenhuma major mais nova em produção.

### bcrypt (custo 14)

- Recomendações de 2026 (OWASP e análises de segurança) situam o piso aceitável em custo 10, o padrão moderno em custo 12, e **custo 13–14 como preferido para sistemas novos**. Fonte: busca web (securityboulevard.com, guptadeepak.com).
- Veredito: ok — custo 14 está alinhado com a recomendação mais rigorosa vigente em 2026, não é um valor desatualizado.

## Conclusão

Nenhuma tecnologia da tabela está abandonada, descontinuada ou substituída por algo incompatível — todas existem, têm projetos ativos, e nenhuma migração é forçada (sem quebra iminente). Dito isso, **quatro itens estão numa defasagem relevante frente ao que hoje é o padrão consolidado do ecossistema**: Go 1.22 (sem patches de segurança upstream há ~1,5 ano), TypeScript 5.2.2 (3 majors atrás), Vite 5.2 (3 majors atrás) e React 18.3.1 (~2 anos atrás do 19.x). PostgreSQL 15, Tailwind 3.4.x/shadcn e bcrypt custo 14 estão em zona confortável. golang-jwt v5 está no estado da arte.

Dado que o AD-9 justifica explicitamente a herança dessas versões por **paridade operacional com o FB_APU02** (evitar fragmentação de conhecimento e padrões de segurança já validados em produção), a recomendação desta review **não é alterar AD-9** — a decisão de consistência é defensável e o risco de Go 1.22 sem patch já existe hoje no sistema irmão, não é introduzido pelo FB_APU05. A recomendação é registrar explicitamente no Spine (ou num Deferred) que **Go 1.22 sem patches de segurança upstream é um débito técnico conhecido e compartilhado entre os dois sistemas**, e que a reavaliação de Go/React/TypeScript/Vite deveria ser feita em conjunto para ambos os sistemas (não isoladamente no FB_APU05), para não duplicar o trabalho de divergência que o próprio AD-9 tenta evitar.
