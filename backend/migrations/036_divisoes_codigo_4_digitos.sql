-- Códigos de divisão (filial) com 4 caracteres, como no SAP (19/11/2026): "91" -> "0091", "501" -> "0501".
-- Vale para o cadastro de Divisões e para a filial gravada nos centros de custo; código com letras
-- (ex.: SEM-DIVISAO) e os que já têm 4 ou mais caracteres não mudam. Se já existir a versão de 4
-- caracteres, a linha antiga é mantida (a chave é única) e o ajuste fica por conta de quem cadastrou.
-- Quando o nome da divisão era só o próprio código (criada pela carga do SAP sem nome melhor), ele acompanha.
UPDATE divisoes d
   SET nome = CASE WHEN d.nome = d.codigo THEN lpad(d.codigo, 4, '0') ELSE d.nome END,
       codigo = lpad(d.codigo, 4, '0'),
       updated_at = now()
 WHERE d.codigo ~ '^[0-9]{1,3}$'
   AND NOT EXISTS (SELECT 1 FROM divisoes x WHERE x.codigo = lpad(d.codigo, 4, '0'));

UPDATE centros_custo SET filial = lpad(filial, 4, '0') WHERE filial ~ '^[0-9]{1,3}$';
