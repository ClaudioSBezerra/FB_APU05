---
title: Addendum — FB_APU05 PRD
status: final
created: 2026-10-06
updated: 2026-10-06
---

# Addendum

Detalhe técnico que informa a arquitetura, mas não pertence à narrativa do PRD.

## Restrições técnicas do gerador de exportação SAP (FR-15)

Lições aprendidas de bugs reais do robô/gerador atual (VBA + `.xlsm`), que o novo gerador precisa preservar:

- **Preservar `vbaProject.bin` byte a byte.** O projeto VBA é binário compilado; o gerador nunca deve tocá-lo — qualquer correção de macro exige reaplicação manual no modelo, não pode ser injetada pelo gerador.
- **Nunca usar replacement de string tipo `$1`/`$&` sem escapar.** Texto livre do usuário (título, descrição) pode conter sequências que o mecanismo de substituição de string interpreta como grupo de regex, corrompendo fórmulas do Excel (`$B$100` virando referência quebrada). Todo conteúdo gerado deve passar por função de escape, nunca por substituição direta.
- **Remover `xl/calcChain.xml` ao reescrever linhas.** Deixar o `calcChain` desatualizado após reescrever linhas faz o Excel pedir reparo a cada abertura.
- **A aba de rascunho (`SAP Export` ou equivalente) precisa sair sempre visível**, nunca `veryHidden` — se a macro não conseguir colar numa aba oculta, a simulação para sem copiar nada, e o sintoma relatado ("não copia nada") aponta para o lugar errado.

## Integração com a base de colaboradores (Senior) — FR-3

É uma integração nova, sem precedente em nenhum sistema da Ferreira Costa hoje. Para V1, a carga é manual: planilha Excel de colaboradores e de colaboradores-por-centro-de-custo, no mesmo padrão da carga inicial de cadastros mestres já especificada no pacote original (CSV, UTF-8 BOM, `;`). Não há acesso direto a banco/view nem API em tempo real nesta fase — isso fica como possível evolução futura, fora do escopo atual.

## Histórico do motor de aprovação — por que "nunca inventar aprovador" é regra, não escolha de estilo

Até agosto de 2026, o sistema de referência tinha uma escada de contingência que, para pares filial×CC sem estratégia cadastrada no SAP acima da janela do gerente, resolvia o aprovador para um cargo que não existe na estrutura real da empresa. A interface nunca refletiu essa escada — sempre mostrou "sem alçada cadastrada" nesses casos —, criando uma divergência silenciosa entre o que o banco gravava e o que o usuário via. A correção foi alinhar o back-end à interface: bloquear com mensagem explícita em vez de resolver para um cargo fictício. O FB_APU05 deve nascer já com esse alinhamento (FR-10), sem repetir o período de divergência.

Essa mesma fonte também descreve papéis que resolvem para o próprio cargo por desenho (ex.: um papel colegiado sem pessoa física associada) — caso que não deve ser confundido com lacuna de cadastro real (ver FR-10).

## Dado sensível na fonte original

O pacote de origem (dicionário de dados e carga inicial) contém nomes completos, matrículas e valores de alçada nominal de diretores reais, classificados pela própria fonte como uso corporativo restrito, com instrução explícita de não publicar fora dos destinatários internos. Este PRD e o addendum do Brief tratam esses casos sempre por **cargo/papel**, nunca por nome de pessoa — o mesmo vale para qualquer artefato futuro (arquitetura, épicos, stories) que derive deste PRD, dado que o repositório do projeto é público.
