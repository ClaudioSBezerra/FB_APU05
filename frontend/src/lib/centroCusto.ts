// F13 (26/11): formato único do centro de custo. O código do SAP é a divisão SEM os zeros à esquerda + os 4 dígitos
// do CC (1016000 = divisão 0101 + 6000; 816000 = 0081 + 6000; 10016000 = 1001 + 6000). Os 4 últimos sozinhos não
// identificam o CC (6000 existe em 63 divisões), então a tela mostra sempre "0101 · 6000". CC de 4 dígitos é
// corporativo (sem divisão).

export function partesCC(codigo: string): { divisao: string; cc: string } {
  const c = (codigo ?? '').trim();
  if (/^\d{5,}$/.test(c)) return { divisao: c.slice(0, -4).replace(/^0+/, '').padStart(4, '0'), cc: c.slice(-4) };
  return { divisao: '', cc: c };
}

/** "0101 · 6000", "Corporativo · 6000" ou o código como veio. */
export function formatarCC(codigo?: string | null): string {
  if (!codigo) return '—';
  const { divisao, cc } = partesCC(codigo);
  if (divisao) return `${divisao} · ${cc}`;
  if (/^\d{4}$/.test(cc)) return `Corporativo · ${cc}`;
  return cc;
}

/** Texto extra para a busca achar o CC por 4 dígitos, pelo código com e sem zero à esquerda e pela divisão. */
export function buscaCC(codigo: string): string {
  const { divisao, cc } = partesCC(codigo);
  if (!divisao) return `${cc} corporativo`;
  const semZero = divisao.replace(/^0+/, '');
  return `${divisao} ${cc} ${semZero}${cc} ${divisao}${cc} ${codigo}`;
}
