import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { BuscaCombobox, type OpcaoBusca } from '@/components/BuscaCombobox';
import { MesAno } from '@/components/MesAno';
import { ValorInput } from '@/components/ValorInput';

// Edição do chamado pela Controladoria (resposta de 26/11, §3). O servidor confere tudo de novo: na Transferência,
// mudança de valor, conta, CC ou mês reconsulta o saldo com o mesmo bloqueio da abertura. O histórico guarda o antes e o
// depois e o solicitante (e quem está em cópia) recebe o que mudou por e-mail.

export interface LancEdicao { id?: string; lado: string; centro_custo_id?: string; conta_id?: string; mes: string; valor: number }
export interface ObraEdicao { id?: string; lado: string; subgrupo: string; ordem: string; valor: number }

interface Props {
  aberto: boolean;
  onFechar: () => void;
  onSalvo: () => void;
  id: string;
  versao: number;
  tipo: string;
  descricao: string | null;
  fornecedor: string | null;
  fornecedorCnpj?: string | null;
  tipoCompra?: string;
  quantidade?: number | null;
  valorUnitario?: number;
  lancamentos: LancEdicao[];
  obras: ObraEdicao[];
}

const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

export function EditarChamado(p: Props) {
  const { data: opcoes } = useQuery({
    queryKey: ['formulario-opcoes'],
    queryFn: async () => {
      const r = await fetch('/api/formulario/opcoes');
      return (await r.json()) as { contas: OpcaoBusca[]; centros_custo_destino?: (OpcaoBusca & { divisao_id: string })[] };
    },
    enabled: p.aberto,
  });
  const [descricao, setDescricao] = useState(p.descricao ?? '');
  const [fornecedor, setFornecedor] = useState(p.fornecedor ?? '');
  const [cnpj, setCnpj] = useState(p.fornecedorCnpj ?? '');
  const [tipoCompra, setTipoCompra] = useState(p.tipoCompra ?? '');
  const [quantidade, setQuantidade] = useState(p.quantidade ? String(p.quantidade) : '');
  const [unitario, setUnitario] = useState(p.valorUnitario != null ? String(p.valorUnitario) : '');
  const [linhas, setLinhas] = useState(p.lancamentos.map((l) => ({ ...l, valorTxt: String(l.valor) })));
  const [obras, setObras] = useState(p.obras.map((o) => ({ ...o, valorTxt: String(o.valor) })));
  const [erro, setErro] = useState('');
  const [salvando, setSalvando] = useState(false);

  const transferencia = p.tipo === 'transferencia';
  const imobilizado = p.tipo === 'imobilizado';
  const destinos = linhas.filter((l) => l.lado === 'destino');
  const destinoAuto = transferencia && destinos.length === 1;
  const somaOrigem = Math.round(linhas.filter((l) => l.lado === 'origem').reduce((a, l) => a + (parseFloat(l.valorTxt) || 0), 0) * 100) / 100;

  function setLinha(i: number, campo: string, v: string) {
    setLinhas((a) => a.map((l, j) => (j === i ? { ...l, [campo]: v } : l)));
  }

  async function salvar() {
    setErro('');
    const corpo: Record<string, unknown> = { versao: p.versao };
    if (descricao.trim() !== (p.descricao ?? '')) corpo.descricao = descricao;
    if (!imobilizado && fornecedor.trim() !== (p.fornecedor ?? '')) corpo.fornecedor = fornecedor;
    if (!imobilizado && cnpj.replace(/\D/g, '') !== (p.fornecedorCnpj ?? '')) corpo.fornecedor_cnpj = cnpj.replace(/\D/g, '');
    if (imobilizado) {
      if (tipoCompra !== (p.tipoCompra ?? '')) corpo.tipo_compra = tipoCompra;
      if (quantidade && Number(quantidade) !== p.quantidade) corpo.quantidade_material = parseInt(quantidade, 10);
      if (unitario && Math.abs(parseFloat(unitario) - (p.valorUnitario ?? 0)) > 0.004) corpo.valor_unitario = parseFloat(unitario);
    }
    const lancs = linhas
      .map((l, i) => {
        const o = p.lancamentos[i];
        const m: Record<string, unknown> = { id: l.id };
        if (l.conta_id && l.conta_id !== o.conta_id) m.conta_id = l.conta_id;
        if (l.centro_custo_id && l.centro_custo_id !== o.centro_custo_id) m.centro_custo_id = l.centro_custo_id;
        if (l.mes && l.mes !== o.mes) m.mes = `${l.mes}-01`;
        const v = parseFloat(l.valorTxt);
        if (!imobilizado && !(destinoAuto && l.lado === 'destino') && v && Math.abs(v - o.valor) > 0.004) m.valor = v;
        return Object.keys(m).length > 1 ? m : null;
      })
      .filter(Boolean);
    if (lancs.length) corpo.lancamentos = lancs;
    const obs = obras
      .map((o, i) => {
        const a = p.obras[i];
        const m: Record<string, unknown> = { id: o.id };
        const v = parseFloat(o.valorTxt);
        if (v && Math.abs(v - a.valor) > 0.004) m.valor = v;
        if (o.ordem.trim() && o.ordem.trim() !== a.ordem) m.ordem_investimento = o.ordem.trim();
        return Object.keys(m).length > 1 ? m : null;
      })
      .filter(Boolean);
    if (obs.length) corpo.obras_linhas = obs;
    if (Object.keys(corpo).length === 1) {
      setErro('Nada mudou: altere algum campo antes de salvar.');
      return;
    }
    setSalvando(true);
    try {
      const r = await fetch(`/api/chamados/${p.id}/edicao`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(corpo) });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d?.erro?.mensagem || d?.error || `Erro HTTP ${r.status}`);
      toast.success(`Chamado atualizado (${(d.mudancas ?? []).length} mudança(s)). O solicitante foi avisado.`);
      p.onSalvo();
    } catch (e) {
      setErro(e instanceof Error ? e.message : 'Erro ao salvar');
    } finally {
      setSalvando(false);
    }
  }

  return (
    <Dialog open={p.aberto} onOpenChange={(o) => !o && p.onFechar()}>
      <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Editar o chamado</DialogTitle>
          <DialogDescription>O histórico guarda o valor antigo e o novo de cada campo, e o solicitante recebe o que mudou por e-mail.</DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          <div className="grid gap-1.5">
            <Label>{p.tipo === 'obras' ? 'Detalhe' : 'Descrição'}</Label>
            <Textarea rows={3} value={descricao} maxLength={2000} onChange={(e) => setDescricao(e.target.value)} />
          </div>
          {!imobilizado && (
            <div className="grid gap-2 sm:grid-cols-[1fr_200px]">
              <div className="grid gap-1.5"><Label>Fornecedor</Label><Input value={fornecedor} maxLength={255} onChange={(e) => setFornecedor(e.target.value)} /></div>
              <div className="grid gap-1.5"><Label>CNPJ</Label><Input value={cnpj} inputMode="numeric" onChange={(e) => setCnpj(e.target.value.replace(/\D/g, '').slice(0, 14))} /></div>
            </div>
          )}
          {imobilizado && (
            <div className="grid gap-2 sm:grid-cols-3">
              <div className="grid gap-1.5">
                <Label>Tipo de compra</Label>
                <Select value={tipoCompra} onValueChange={setTipoCompra}>
                  <SelectTrigger><SelectValue placeholder="FL ou Externa" /></SelectTrigger>
                  <SelectContent><SelectItem value="FL">FL</SelectItem><SelectItem value="Externa">Externa</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="grid gap-1.5"><Label>Quantidade</Label><Input inputMode="numeric" value={quantidade} onChange={(e) => setQuantidade(e.target.value.replace(/\D/g, ''))} /></div>
              <div className="grid gap-1.5"><Label>Valor unitário</Label><ValorInput value={unitario} onChange={setUnitario} /></div>
              <p className="text-xs text-fg-helper sm:col-span-3">Total: {moeda.format(Math.round((parseInt(quantidade, 10) || 0) * (parseFloat(unitario) || 0) * 100) / 100)}</p>
            </div>
          )}

          {linhas.length > 0 && !imobilizado && (
            <div className="grid gap-2">
              <h3 className="text-sm font-semibold">Linhas</h3>
              {linhas.map((l, i) => (
                <div key={l.id ?? i} className="grid gap-2 rounded-md border border-border p-2 sm:grid-cols-2">
                  <p className="text-xs font-medium sm:col-span-2">{l.lado === 'origem' ? 'Origem' : l.lado === 'destino' ? 'Destino' : 'Linha'}</p>
                  <div className="grid min-w-0 gap-1"><Label className="text-xs">Centro de custo</Label>
                    <BuscaCombobox value={l.centro_custo_id ?? ''} onChange={(v) => setLinha(i, 'centro_custo_id', v)} opcoes={opcoes?.centros_custo_destino ?? []} placeholder="Centro de custo" vazio="Nenhum centro de custo encontrado." />
                  </div>
                  <div className="grid min-w-0 gap-1"><Label className="text-xs">Conta</Label>
                    <BuscaCombobox value={l.conta_id ?? ''} onChange={(v) => setLinha(i, 'conta_id', v)} opcoes={opcoes?.contas ?? []} />
                  </div>
                  <div className="grid gap-1"><Label className="text-xs">Mês</Label><MesAno value={l.mes} onChange={(v) => setLinha(i, 'mes', v)} /></div>
                  <div className="grid gap-1"><Label className="text-xs">Valor</Label>
                    {destinoAuto && l.lado === 'destino'
                      ? <div className="flex h-10 items-center rounded-md border border-input bg-[var(--bg-input-disabled)] px-3 text-sm tabular-nums">{moeda.format(somaOrigem)}</div>
                      : <ValorInput value={l.valorTxt} onChange={(v) => setLinha(i, 'valorTxt', v)} />}
                  </div>
                </div>
              ))}
              {transferencia && <p className="text-xs text-fg-helper">Ao salvar, o saldo da origem é consultado de novo no SAP; se algum mês ficar sem saldo, a edição é recusada.</p>}
            </div>
          )}

          {obras.length > 0 && (
            <div className="grid gap-2">
              <h3 className="text-sm font-semibold">Linhas da obra</h3>
              {obras.map((o, i) => (
                <div key={o.id ?? i} className="grid gap-2 rounded-md border border-border p-2 sm:grid-cols-[1fr_10rem_10rem] sm:items-end">
                  <p className="text-xs">{o.lado === 'retirada' ? 'Retirada · ' : o.lado === 'inclusao' ? 'Inclusão · ' : ''}{o.subgrupo}</p>
                  <div className="grid gap-1"><Label className="text-xs">Ordem</Label><Input value={o.ordem} onChange={(e) => setObras((a) => a.map((x, j) => (j === i ? { ...x, ordem: e.target.value } : x)))} /></div>
                  <div className="grid gap-1"><Label className="text-xs">Valor</Label><ValorInput value={o.valorTxt} onChange={(v) => setObras((a) => a.map((x, j) => (j === i ? { ...x, valorTxt: v } : x)))} /></div>
                </div>
              ))}
            </div>
          )}

          {erro && <p role="alert" className="rounded-md border border-danger-border bg-danger-bg px-3 py-2 text-sm text-danger-text">{erro}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={p.onFechar}>Cancelar</Button>
          <Button onClick={salvar} disabled={salvando}>{salvando ? 'Salvando…' : 'Salvar alterações'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
