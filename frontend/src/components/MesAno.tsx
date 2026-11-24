import { useState } from 'react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

// Seletor de mês/ano (dois combos) no lugar do <input type="month">, que varia de navegador para navegador.
// value/onChange usam "AAAA-MM"; só emite valor quando mês e ano estão escolhidos (senão "").
const MESES = ['Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho', 'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'];

export function MesAno({ value, onChange, ano: anoFixo }: { value: string; onChange: (v: string) => void; ano?: string }) {
  const [vAno, vMes] = /^\d{4}-\d{2}/.test(value) ? [value.slice(0, 4), value.slice(5, 7)] : ['', ''];
  const [ano, setAno] = useState(vAno);
  const [mes, setMes] = useState(vMes);
  // acompanha mudanças externas (ex.: limpar o formulário)
  const anoAtual = value ? vAno : ano;
  const mesAtual = value ? vMes : mes;
  if (anoFixo) {
    // ano definido pela solicitação (regra do ano único): só o mês é escolhido
    const mesFixo = /^\d{4}-\d{2}/.test(value) ? value.slice(5, 7) : '';
    return (
      <Select value={mesFixo} onValueChange={(m) => onChange(`${anoFixo}-${m}`)}>
        <SelectTrigger aria-label="Mês"><SelectValue placeholder="Mês" /></SelectTrigger>
        <SelectContent>
          {MESES.map((nome, i) => {
            const v = String(i + 1).padStart(2, '0');
            return <SelectItem key={v} value={v}>{nome}</SelectItem>;
          })}
        </SelectContent>
      </Select>
    );
  }
  const base = new Date().getFullYear();
  const anos = Array.from({ length: 5 }, (_, i) => String(base - 1 + i));
  if (anoAtual && !anos.includes(anoAtual)) anos.push(anoAtual);

  const emitir = (a: string, m: string) => onChange(a && m ? `${a}-${m}` : '');
  return (
    <div className="flex gap-2">
      <div className="min-w-0 flex-1">
        <Select value={mesAtual} onValueChange={(m) => { setMes(m); emitir(anoAtual, m); }}>
          <SelectTrigger aria-label="Mês"><SelectValue placeholder="Mês" /></SelectTrigger>
          <SelectContent>
            {MESES.map((nome, i) => {
              const v = String(i + 1).padStart(2, '0');
              return <SelectItem key={v} value={v}>{nome}</SelectItem>;
            })}
          </SelectContent>
        </Select>
      </div>
      <div className="w-28">
        <Select value={anoAtual} onValueChange={(a) => { setAno(a); emitir(a, mesAtual); }}>
          <SelectTrigger aria-label="Ano"><SelectValue placeholder="Ano" /></SelectTrigger>
          <SelectContent>
            {anos.sort().map((a) => <SelectItem key={a} value={a}>{a}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}
