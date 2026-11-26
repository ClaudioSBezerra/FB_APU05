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
import { buscaCC, formatarCC } from '@/lib/centroCusto';
import { OrdemDaLinha, nomeSubgrupo, tipoDaLinha, type TipoOrdem } from '@/components/ObrasLinha';

// Edição do chamado pela Controladoria (resposta de 26/11, §3). O servidor confere tudo de novo: na Transferência,
// mudança de valor, conta, CC ou mês reconsulta o saldo com o mesmo bloqueio da abertura. O histórico guarda o antes e o
// depois e o solicitante (e quem está em cópia) recebe o que mudou por e-mail.

export interface LancEdicao { id?: string; lado: string; centro_custo_id?: string; conta_id?: string; mes: string; valor: number }
export interface ObraEdicao {
  id?: string; lado: string; subgrupo: string; ordem: string; valor: number;
  local_obra_id?: string; subgrupo_despesa_id?: string; tipo?: string;
}

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
  classificacao?: string | null;
  anoOrcamento?: number;
}

const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

export function EditarChamado(p: Props) {
  const { data: opcoes } = useQuery({
    queryKey: ['formulario-opcoes'],
    queryFn: async () => {
      const r = await fetch('/api/formulario/opcoes');
      return (await r.json()) as {
        contas: OpcaoBusca[];
        centros_custo_destino?: (OpcaoBusca & { divisao_id: string })[];
        locais_obra?: (OpcaoBusca & { tipo_estrutura?: string })[];
        subgrupos_despesa?: (OpcaoBusca & { familias?: string[] })[];
        anos_obras?: number[];
        ano_corrente?: number;
      };
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
  // F15 (26/11): Obras completa — classificação, ano, local e as linhas (incluir e remover), com as regras da abertura
  type LinhaEd = { subgrupoDespesaId: string; ordemInvestimento: string; valorTxt: string; lado: 'retirada' | 'inclusao'; tipo: TipoOrdem };
  const linhasIniciais: LinhaEd[] = p.obras.map((o) => ({
    subgrupoDespesaId: o.subgrupo_despesa_id ?? '', ordemInvestimento: o.ordem, valorTxt: String(o.valor),
    lado: o.lado === 'inclusao' ? 'inclusao' : 'retirada', tipo: (o.tipo === 'FL' || o.tipo === 'Externa' ? o.tipo : '') as TipoOrdem,
  }));
  const [obClassif, setObClassif] = useState(p.classificacao ?? 'inclusao');
  const [obAno, setObAno] = useState(p.anoOrcamento ? String(p.anoOrcamento) : '');
  const [obLocal, setObLocal] = useState(p.obras[0]?.local_obra_id ?? '');
  const [obLinhas, setObLinhas] = useState<LinhaEd[]>(linhasIniciais);
  const familia = (opcoes?.locais_obra ?? []).find((l) => l.id === obLocal)?.tipo_estrutura ?? '';
  const subgruposObra = (opcoes?.subgrupos_despesa ?? [])
    .filter((sg) => !familia || (sg.familias ?? []).includes(familia))
    .map((sg) => ({ ...sg, nome: nomeSubgrupo(sg.codigo, sg.nome), busca: sg.nome }));
  const setLinhaOb = (i: number, campo: keyof LinhaEd, v: string) =>
    setObLinhas((a) => a.map((l, j) => (j === i ? { ...l, [campo]: v, ...(campo === 'subgrupoDespesaId' || campo === 'tipo' ? { ordemInvestimento: '' } : {}) } : l)));
  const usaTipo = obClassif === 'retirada_saldo' || obClassif === 'transferencia_saldo';
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
    if (p.tipo === 'obras') {
      const obrasNovo = {
        classificacao: obClassif,
        ano_orcamento: Number(obAno) || 0,
        local_obra_id: obLocal,
        linhas: obLinhas.map((l) => ({
          subgrupo_despesa_id: l.subgrupoDespesaId,
          ordem_investimento: l.ordemInvestimento,
          valor: parseFloat(l.valorTxt) || 0,
          tipo: tipoDaLinha(obClassif, l),
          ...(obClassif === 'transferencia_saldo' ? { lado: l.lado } : {}),
        })),
      };
      const mudouObras =
        obClassif !== (p.classificacao ?? 'inclusao') || obAno !== (p.anoOrcamento ? String(p.anoOrcamento) : '') ||
        obLocal !== (p.obras[0]?.local_obra_id ?? '') || JSON.stringify(obLinhas) !== JSON.stringify(linhasIniciais);
      if (mudouObras) corpo.obras = obrasNovo;
    }
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
                    <BuscaCombobox value={l.centro_custo_id ?? ''} onChange={(v) => setLinha(i, 'centro_custo_id', v)} opcoes={(opcoes?.centros_custo_destino ?? []).map((c) => ({ ...c, codigo: formatarCC(c.codigo), busca: buscaCC(c.codigo) }))} placeholder="Centro de custo" vazio="Nenhum centro de custo encontrado." />
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

          {p.tipo === 'obras' && (
            <div className="grid gap-3">
              <h3 className="text-sm font-semibold">Obras</h3>
              <div className="grid gap-2 sm:grid-cols-3">
                <div className="grid gap-1">
                  <Label className="text-xs">Classificação</Label>
                  <Select value={obClassif} onValueChange={(v) => { setObClassif(v); setObLinhas((a) => a.map((l) => ({ ...l, ordemInvestimento: '' }))); }}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="inclusao">Inclusão</SelectItem>
                      <SelectItem value="fl" disabled={familia === 'Fortaleza'}>FL{familia === 'Fortaleza' ? ' (Fortaleza não tem FL)' : ''}</SelectItem>
                      <SelectItem value="retirada_saldo">Retirada de saldo</SelectItem>
                      <SelectItem value="transferencia_saldo">Transferência de saldo</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {usaTipo && (
                  <div className="grid gap-1">
                    <Label className="text-xs">Ano do orçamento</Label>
                    <Select value={obAno || String(opcoes?.ano_corrente ?? '')} onValueChange={setObAno}>
                      <SelectTrigger><SelectValue placeholder="Ano" /></SelectTrigger>
                      <SelectContent>{(opcoes?.anos_obras ?? []).map((a) => <SelectItem key={a} value={String(a)}>{a}</SelectItem>)}</SelectContent>
                    </Select>
                  </div>
                )}
                <div className="grid gap-1">
                  <Label className="text-xs">Local de obra</Label>
                  <Select value={obLocal} onValueChange={(v) => { setObLocal(v); setObLinhas((a) => a.map((l) => ({ ...l, ordemInvestimento: '', subgrupoDespesaId: '' }))); }}>
                    <SelectTrigger><SelectValue placeholder="Local" /></SelectTrigger>
                    <SelectContent>{(opcoes?.locais_obra ?? []).map((l) => <SelectItem key={l.id} value={l.id}>{l.nome}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
              </div>
              {obLinhas.map((l, i) => (
                <div key={i} className="grid gap-2 rounded-md border border-border p-2 sm:grid-cols-2">
                  <div className="flex items-center justify-between sm:col-span-2">
                    <span className="text-xs font-medium">Linha {i + 1}</span>
                    {obLinhas.length > 1 && <Button size="sm" variant="ghost" onClick={() => setObLinhas((a) => a.filter((_, j) => j !== i))}>Remover linha</Button>}
                  </div>
                  {obClassif === 'transferencia_saldo' && (
                    <div className="grid gap-1">
                      <Label className="text-xs">Lado</Label>
                      <Select value={l.lado} onValueChange={(v) => setLinhaOb(i, 'lado', v)}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent><SelectItem value="retirada">Retirada</SelectItem><SelectItem value="inclusao">Inclusão</SelectItem></SelectContent>
                      </Select>
                    </div>
                  )}
                  {usaTipo && (
                    <div className="grid gap-1">
                      <Label className="text-xs">Tipo da ordem</Label>
                      <Select value={familia === 'Fortaleza' ? 'Externa' : l.tipo} onValueChange={(v) => setLinhaOb(i, 'tipo', v)} disabled={familia === 'Fortaleza'}>
                        <SelectTrigger><SelectValue placeholder="FL ou Externa" /></SelectTrigger>
                        <SelectContent><SelectItem value="Externa">Externa</SelectItem><SelectItem value="FL">FL</SelectItem></SelectContent>
                      </Select>
                    </div>
                  )}
                  <div className="grid min-w-0 gap-1 sm:col-span-2">
                    <Label className="text-xs">Subgrupo</Label>
                    <BuscaCombobox value={l.subgrupoDespesaId} onChange={(v) => setLinhaOb(i, 'subgrupoDespesaId', v)} opcoes={subgruposObra}
                      placeholder="Escolha o subgrupo" vazio="Nenhum subgrupo nesta estrutura." ariaLabel="Subgrupo" />
                  </div>
                  <div className="grid gap-1">
                    <Label className="text-xs">Ordem</Label>
                    <OrdemDaLinha localObraId={obLocal} subgrupoDespesaId={l.subgrupoDespesaId} classificacao={obClassif} lado={l.lado}
                      tipo={familia === 'Fortaleza' && usaTipo ? 'Externa' : tipoDaLinha(obClassif, l)}
                      value={l.ordemInvestimento} onChange={(v) => setLinhaOb(i, 'ordemInvestimento', v)} />
                  </div>
                  <div className="grid gap-1"><Label className="text-xs">Valor</Label><ValorInput value={l.valorTxt} onChange={(v) => setLinhaOb(i, 'valorTxt', v)} /></div>
                </div>
              ))}
              <Button size="sm" variant="outline" className="justify-self-start"
                onClick={() => setObLinhas((a) => [...a, { subgrupoDespesaId: '', ordemInvestimento: '', valorTxt: '', lado: 'retirada', tipo: familia === 'Fortaleza' ? 'Externa' : '' }])}>
                + linha
              </Button>
              <p className="text-xs text-fg-helper">O servidor confere de novo a estrutura da obra, as ordens (inclusive bloqueadas) e o "CRIAR", como na abertura.</p>
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
