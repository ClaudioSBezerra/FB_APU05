import { useState } from 'react';
import { Input } from '@/components/ui/input';

// Campo de valor em reais. Aceita o que o usuário digita no Brasil: 5000, 5.000, 5.000,50, 1.234,56 e R$ 1.234,56.
// value/onChange usam o texto com ponto decimal ("1234.56"), que o resto do formulário converte com parseFloat.
// Enquanto o campo está em foco mostra exatamente o que foi digitado; ao sair mostra "R$ 1.234,56".
const fmt = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

/** Converte o texto digitado em "1234.56" (ou "" se não houver número). Exportada para teste. */
export function paraDecimal(entrada: string): string {
  const t = entrada.replace(/R\$/gi, '').replace(/[^\d.,]/g, '');
  if (!/\d/.test(t)) return '';
  let int: string;
  let dec = '';
  const virg = t.lastIndexOf(',');
  if (virg >= 0) {
    // vírgula = separador decimal; os pontos antes dela são de milhar
    int = t.slice(0, virg).replace(/[.,]/g, '');
    dec = t.slice(virg + 1).replace(/[.,]/g, '');
  } else {
    const pontos = (t.match(/\./g) ?? []).length;
    const ult = t.lastIndexOf('.');
    const depois = ult >= 0 ? t.slice(ult + 1) : '';
    if (pontos === 0) int = t;
    else if (pontos > 1 || depois.length === 3) int = t.replace(/\./g, ''); // 5.000 / 1.234.567 = milhar
    else {
      int = t.slice(0, ult); // 5.5 / 12.50 = decimal
      dec = depois;
    }
  }
  int = int.replace(/^0+(?=\d)/, '');
  dec = dec.slice(0, 2);
  return dec !== '' || virg >= 0 ? `${int || '0'}.${dec}` : int;
}

export function ValorInput({ value, onChange, id }: { value: string; onChange: (v: string) => void; id?: string }) {
  const [foco, setFoco] = useState(false);
  const [texto, setTexto] = useState('');
  const n = parseFloat(value);
  const formatado = value !== '' && !Number.isNaN(n) ? fmt.format(n) : '';
  return (
    <Input
      id={id}
      inputMode="decimal"
      placeholder="R$ 0,00"
      value={foco ? texto : formatado}
      onFocus={() => {
        setTexto(value === '' ? '' : value.replace('.', ','));
        setFoco(true);
      }}
      onBlur={() => setFoco(false)}
      onChange={(e) => {
        setTexto(e.target.value);
        onChange(paraDecimal(e.target.value));
      }}
    />
  );
}
