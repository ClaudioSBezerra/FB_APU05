- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Runner de migrations continua para a próxima migration mesmo quando uma falha, sem parar — padrão herdado deliberadamente do FB_APU02; revisitar quando a Story 2.1 trouxer migrations reais com dependência de ordem.
  evidence: `backend/main.go` (`onDBConnected`) loga e segue em erro de `Exec`; hoje inofensivo porque `migrations/` está vazia, mas relevante assim que houver sequência real.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Runner de migrations e a stack `docker-compose` completa nunca foram exercitados de ponta a ponta nesta story.
  evidence: Docker não está instalado no sandbox de desenvolvimento; a spec proíbe explicitamente montar CI/CD nesta story. Settles when a future deploy/CI story runs `docker compose up --wait` + `curl /api/health` against this exact compose file.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-scaffold-inicial-do-projeto.md`
  summary: Ambiente de teste do frontend (`vite.config.ts`, `test.environment: 'node'`) não está pronto para testes de componente React (sem `jsdom`/`@testing-library/react`).
  evidence: Nenhum teste existe ainda no frontend; só vira problema quando a primeira story escrever um teste de componente.
