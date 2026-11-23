import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Label } from '@/components/ui/label';

// Página PÚBLICA aberta pelo link do e-mail de aprovação (sem login: o token da URL é a credencial).
// Mostra a solicitação e só registra a decisão quando o aprovador confirma aqui (o link em si nunca decide).
interface Linha { lado: string; cc: string; conta: string; mes: string; valor: number }
interface Consulta {
  estado: 'pendente' | 'decidida' | 'expirada' | 'encerrada';
  decisao?: 'aprovada' | 'rejeitada';
  aprovador: string;
  resumo: { solicitante: string; total: number; linhas: Linha[] };
}

const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

export default function AprovarPorLink() {
  const { token = '' } = useParams();
  const [rejeitando, setRejeitando] = useState(false);
  const [motivo, setMotivo] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState('');
  const [feito, setFeito] = useState<'aprovada' | 'rejeitada' | null>(null);

  const q = useQuery({
    queryKey: ['aprovacao-link', token],
    retry: false,
    queryFn: async () => {
      const r = await fetch(`/api/aprovacao/${encodeURIComponent(token)}`);
      if (r.status === 404) throw new Error('Este link não é válido.');
      if (!r.ok) throw new Error('Não foi possível abrir a solicitação agora. Tente novamente em instantes.');
      return (await r.json()) as Consulta;
    },
  });

  async function decidir(decisao: 'aprovada' | 'rejeitada') {
    setEnviando(true);
    setErro('');
    try {
      const r = await fetch(`/api/aprovacao/${encodeURIComponent(token)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ decisao, motivo: motivo.trim() }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d?.error || 'Não foi possível registrar a decisão.');
      setFeito(decisao);
    } catch (e) {
      setErro((e as Error).message);
    } finally {
      setEnviando(false);
    }
  }

  const c = q.data;
  let corpo: React.ReactNode;
  if (q.isLoading) corpo = <p className="text-sm text-muted-foreground">Carregando…</p>;
  else if (q.error) corpo = <p className="text-sm text-destructive">{(q.error as Error).message}</p>;
  else if (feito)
    corpo = (
      <p className="text-sm">
        {feito === 'aprovada' ? 'Aprovação registrada. A solicitação segue para o atendimento.' : 'Rejeição registrada. O solicitante será avisado.'}{' '}
        Você já pode fechar esta página.
      </p>
    );
  else if (c && c.estado !== 'pendente')
    corpo = (
      <p className="text-sm">
        {c.estado === 'decidida' && `Esta solicitação já foi ${c.decisao === 'aprovada' ? 'aprovada' : 'rejeitada'} por este link.`}
        {c.estado === 'encerrada' && 'Esta solicitação já foi decidida por outra via.'}
        {c.estado === 'expirada' && 'Este link expirou. Peça ao solicitante ou ao atendimento para registrar a decisão.'}
      </p>
    );
  else if (c)
    corpo = (
      <div className="grid gap-4">
        <p className="text-sm">
          <strong>{c.resumo.solicitante}</strong> pede a transferência de orçamento de{' '}
          <strong>{moeda.format(c.resumo.total)}</strong>.
        </p>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead>
              <tr className="border-b text-muted-foreground">
                <th className="py-1 pr-2">Lado</th><th className="pr-2">Centro de custo</th><th className="pr-2">Conta</th><th className="pr-2">Mês</th><th className="text-right">Valor</th>
              </tr>
            </thead>
            <tbody>
              {c.resumo.linhas.map((l, i) => (
                <tr key={i} className="border-b">
                  <td className="py-1 pr-2">{l.lado}</td><td className="pr-2">{l.cc}</td><td className="pr-2">{l.conta}</td><td className="pr-2">{l.mes}</td>
                  <td className="text-right tabular-nums">{moeda.format(l.valor)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {rejeitando && (
          <div className="grid gap-1.5">
            <Label htmlFor="motivo">Motivo da rejeição</Label>
            <Textarea id="motivo" value={motivo} onChange={(e) => setMotivo(e.target.value)} />
          </div>
        )}
        {erro && <p className="text-sm text-destructive">{erro}</p>}
        <div className="flex flex-wrap gap-2">
          {!rejeitando ? (
            <>
              <Button disabled={enviando} onClick={() => decidir('aprovada')}>Aprovar</Button>
              <Button variant="outline" disabled={enviando} onClick={() => setRejeitando(true)}>Rejeitar</Button>
            </>
          ) : (
            <>
              <Button variant="destructive" disabled={enviando || motivo.trim().length < 3} onClick={() => decidir('rejeitada')}>Confirmar rejeição</Button>
              <Button variant="ghost" disabled={enviando} onClick={() => setRejeitando(false)}>Voltar</Button>
            </>
          )}
        </div>
      </div>
    );

  return (
    <div className="flex min-h-screen items-start justify-center bg-background px-4 py-10">
      <div className="w-full max-w-2xl rounded-lg border border-border bg-card p-6 shadow-sm">
        <h1 className="mb-4 text-base font-semibold text-card-foreground">Aprovação de transferência de orçamento</h1>
        {corpo}
      </div>
    </div>
  );
}
