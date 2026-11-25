import { useState } from 'react';
import { Input } from '@/components/ui/input';

// Campo de valor em reais com máscara: enquanto digita aceita "500000,50"; ao sair mostra "R$ 500.000,50".
// value/onChange usam o texto com ponto decimal ("500000.50"), que o resto do formulário já converte com parseFloat.
const fmt = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

export function ValorInput({ value, onChange, id }: { value: string; onChange: (v: string) => void; id?: string }) {
  const [foco, setFoco] = useState(false);
  const n = parseFloat(value);
  const mostrado = foco ? value.replace('.', ',') : value !== '' && !Number.isNaN(n) ? fmt.format(n) : '';
  return (
    <Input
      id={id}
      inputMode="decimal"
      placeholder="R$ 0,00"
      value={mostrado}
      onFocus={() => setFoco(true)}
      onBlur={() => setFoco(false)}
      onChange={(e) => {
        // só dígitos e uma vírgula/ponto decimal, com no máximo 2 casas
        const t = e.target.value.replace(/[^\d.,]/g, '').replace(',', '.');
        const [int, ...resto] = t.split('.');
        onChange(resto.length ? `${int}.${resto.join('').slice(0, 2)}` : int);
      }}
    />
  );
}
