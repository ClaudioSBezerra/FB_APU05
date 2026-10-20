-- Fornecedor da despesa e descrição detalhada (20/10) — paridade com o
-- formulário legado do Jira Service Management ("Solicitação de
-- Transferência de Verba de Orçamento de Despesas SAP"), que já pede os
-- dois campos. Opcionais aqui (NULL permitido): ainda não foi decidido se
-- vão ser obrigatórios em todos os 5 tipos de solicitação, então a
-- validação de "campo obrigatório" fica de fora por enquanto — só a coluna.
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS fornecedor VARCHAR(255);
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS descricao TEXT;
