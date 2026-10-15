import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

// Tela de abertura de solicitação (Epic 3 — Stories 3.1 a 3.5). Cobre os 5
// tipos aceitos por POST /api/solicitacoes (handlers/solicitacoes.go):
// transferencia, inclusao, inclusao_sfc, imobilizado, obras. Os combos são
// populados por GET /api/formulario/opcoes/autorizadores/obra-ordens
// (handlers/formulario.go) — o servidor é sempre a fonte da verdade: esta
// tela nunca decide aprovador nem valida CC, só monta o payload que
// AbrirSolicitacaoHandler já valida de verdade.

type TipoSolicitacao = 'transferencia' | 'inclusao' | 'inclusao_sfc' | 'imobilizado' | 'obras';

interface OpcaoItem {
  id: string;
  codigo: string;
  nome: string;
}

interface CentroCustoOpcao extends OpcaoItem {
  divisao_id: string;
}

interface OpcoesFormulario {
  centros_custo: CentroCustoOpcao[];
  divisoes: OpcaoItem[];
  contas_bifc: OpcaoItem[];
  contas_sfc: OpcaoItem[];
  classes_imobilizado: OpcaoItem[];
  locais_obra: OpcaoItem[];
  subgrupos_despesa: OpcaoItem[];
}

interface LinhaLancamento {
  lado: 'origem' | 'destino';
  contaId: string;
  classeImobilizadoId: string;
  mes: string;
  valor: string;
}

interface LinhaObra {
  localObraId: string;
  subgrupoDespesaId: string;
  ordemInvestimento: string;
  valor: string;
  lado: 'retirada' | 'inclusao';
}

const classificacoesObras = [
  { value: 'inclusao', label: 'Inclusão' },
  { value: 'fl', label: 'FL' },
  { value: 'retirada_saldo', label: 'Retirada de saldo' },
  { value: 'transferencia_saldo', label: 'Transferência de saldo' },
];

function novaLinhaLancamento(lado: 'origem' | 'destino'): LinhaLancamento {
  return { lado, contaId: '', classeImobilizadoId: '', mes: '', valor: '' };
}

function novaLinhaObra(): LinhaObra {
  return { localObraId: '', subgrupoDespesaId: '', ordemInvestimento: '', valor: '', lado: 'retirada' };
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

function fileParaBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const resultado = reader.result as string;
      resolve(resultado.split(',')[1] ?? '');
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

// OrdemInvestimentoSelect — combo de ordem de investimento para uma linha de
// Obras: lista as ordens já existentes para o par local/subgrupo escolhido
// (GET /api/formulario/obra-ordens) mais a opção fixa "CRIAR" (Story 3.5 —
// só nasce de fato na finalização, Epic 4).
function OrdemInvestimentoSelect({
  localObraId,
  subgrupoDespesaId,
  value,
  onChange,
}: {
  localObraId: string;
  subgrupoDespesaId: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const habilitado = Boolean(localObraId && subgrupoDespesaId);
  const { data } = useQuery({
    queryKey: ['obra-ordens', localObraId, subgrupoDespesaId],
    queryFn: () =>
      fetchJSON<{ items: string[] }>(
        `/api/formulario/obra-ordens?local_obra_id=${localObraId}&subgrupo_despesa_id=${subgrupoDespesaId}`,
      ),
    enabled: habilitado,
  });

  return (
    <Select value={value} onValueChange={onChange} disabled={!habilitado}>
      <SelectTrigger className="h-9">
        <SelectValue placeholder={habilitado ? 'Ordem de investimento' : 'Escolha local e subgrupo'} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="CRIAR">CRIAR (nova ordem, só na finalização)</SelectItem>
        {(data?.items ?? []).map((numero) => (
          <SelectItem key={numero} value={numero}>
            {numero}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

const AbrirSolicitacao = () => {
  const [tipo, setTipo] = useState<TipoSolicitacao>('transferencia');
  const [centroCustoId, setCentroCustoId] = useState('');
  const [linhas, setLinhas] = useState<LinhaLancamento[]>([
    novaLinhaLancamento('origem'),
    novaLinhaLancamento('destino'),
  ]);
  const [autorizadorId, setAutorizadorId] = useState('');
  const [anexo, setAnexo] = useState<{ nome: string; contentType: string; base64: string } | null>(null);
  const [classificacao, setClassificacao] = useState('inclusao');
  const [obrasLinhas, setObrasLinhas] = useState<LinhaObra[]>([novaLinhaObra()]);
  const [enviando, setEnviando] = useState(false);

  const { data: opcoes, error: opcoesError } = useQuery({
    queryKey: ['formulario-opcoes'],
    queryFn: () => fetchJSON<OpcoesFormulario>('/api/formulario/opcoes'),
  });

  // Ao trocar de tipo, reseta os campos específicos do tipo anterior — evita
  // enviar lixo de um tipo para outro (ex.: anexo de Imobilizado sobrando ao
  // trocar para Transferência). Direto no handler da troca (não num efeito):
  // é uma resposta a uma ação do usuário, não uma sincronização com sistema
  // externo.
  function handleTipoChange(novoTipo: TipoSolicitacao) {
    setTipo(novoTipo);
    setAutorizadorId('');
    setAnexo(null);
    if (novoTipo === 'transferencia') {
      setLinhas([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
    } else if (novoTipo === 'obras') {
      setObrasLinhas([novaLinhaObra()]);
    } else {
      setLinhas([novaLinhaLancamento('destino')]);
    }
  }

  const centroCustoSelecionado = opcoes?.centros_custo.find((cc) => cc.id === centroCustoId);

  const valorParaAutorizador = useMemo(() => {
    if (tipo === 'obras') {
      if (classificacao === 'transferencia_saldo') {
        const somaRetirada = obrasLinhas
          .filter((l) => l.lado === 'retirada')
          .reduce((acc, l) => acc + (parseFloat(l.valor) || 0), 0);
        const somaInclusao = obrasLinhas
          .filter((l) => l.lado === 'inclusao')
          .reduce((acc, l) => acc + (parseFloat(l.valor) || 0), 0);
        return Math.max(somaRetirada, somaInclusao);
      }
      return obrasLinhas.reduce((acc, l) => acc + (parseFloat(l.valor) || 0), 0);
    }
    return parseFloat(linhas[0]?.valor ?? '') || 0;
  }, [tipo, classificacao, obrasLinhas, linhas]);

  const precisaAutorizador = tipo === 'inclusao' || tipo === 'imobilizado' || tipo === 'obras';
  const { data: autorizadores } = useQuery({
    queryKey: ['autorizadores', tipo, centroCustoSelecionado?.codigo, valorParaAutorizador],
    queryFn: () => {
      const params = new URLSearchParams({ tipo, valor: String(valorParaAutorizador) });
      if (tipo !== 'obras' && centroCustoSelecionado) {
        params.set('centro_custo_codigo', centroCustoSelecionado.codigo);
      }
      return fetchJSON<{ items: { colaborador_id: string; nome: string }[] }>(
        `/api/formulario/autorizadores?${params.toString()}`,
      );
    },
    enabled:
      precisaAutorizador &&
      valorParaAutorizador > 0 &&
      (tipo === 'obras' || Boolean(centroCustoSelecionado)),
  });

  function atualizarLinha(indice: number, campo: keyof LinhaLancamento, valor: string) {
    setLinhas((atual) => atual.map((linha, i) => (i === indice ? { ...linha, [campo]: valor } : linha)));
  }

  function atualizarLinhaObra(indice: number, campo: keyof LinhaObra, valor: string) {
    setObrasLinhas((atual) => atual.map((linha, i) => (i === indice ? { ...linha, [campo]: valor } : linha)));
  }

  async function handleAnexoChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const base64 = await fileParaBase64(file);
    setAnexo({ nome: file.name, contentType: file.type || 'application/octet-stream', base64 });
  }

  function resetarFormulario() {
    setCentroCustoId('');
    setAutorizadorId('');
    setAnexo(null);
    if (tipo === 'transferencia') {
      setLinhas([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
    } else if (tipo === 'obras') {
      setClassificacao('inclusao');
      setObrasLinhas([novaLinhaObra()]);
    } else {
      setLinhas([novaLinhaLancamento('destino')]);
    }
  }

  async function handleSubmit() {
    setEnviando(true);
    try {
      let payload: Record<string, unknown>;

      if (tipo === 'obras') {
        payload = {
          tipo_solicitacao: 'obras',
          classificacao,
          autorizador_id: autorizadorId,
          obras_linhas: obrasLinhas.map((l) => ({
            local_obra_id: l.localObraId,
            subgrupo_despesa_id: l.subgrupoDespesaId,
            ordem_investimento: l.ordemInvestimento,
            valor: parseFloat(l.valor) || 0,
            ...(classificacao === 'transferencia_saldo' ? { lado: l.lado } : {}),
          })),
        };
      } else {
        const divisaoId = centroCustoSelecionado?.divisao_id ?? '';
        payload = {
          tipo_solicitacao: tipo,
          ...(tipo === 'inclusao' || tipo === 'imobilizado' ? { autorizador_id: autorizadorId } : {}),
          ...(tipo === 'imobilizado' && anexo
            ? {
                anexos: [
                  { nome_arquivo: anexo.nome, content_type: anexo.contentType, conteudo_base64: anexo.base64 },
                ],
              }
            : {}),
          lancamentos: linhas.map((l) => ({
            lado: l.lado,
            divisao_id: divisaoId,
            centro_custo_id: centroCustoId,
            valor: parseFloat(l.valor) || 0,
            ...(tipo === 'imobilizado'
              ? { classe_imobilizado_id: l.classeImobilizadoId }
              : { conta_id: l.contaId, mes: l.mes ? `${l.mes}-01` : '' }),
          })),
        };
      }

      const resp = await fetchJSON<{ id: string; aprovador_snapshot: { nome: string; motivo: string } }>(
        '/api/solicitacoes',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        },
      );

      toast.success(
        `Solicitação aberta com sucesso. Aprovador: ${resp.aprovador_snapshot.nome} (${resp.aprovador_snapshot.motivo})`,
      );
      resetarFormulario();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao abrir solicitação');
    } finally {
      setEnviando(false);
    }
  }

  const contasDisponiveis = tipo === 'inclusao_sfc' ? opcoes?.contas_sfc ?? [] : opcoes?.contas_bifc ?? [];
  const podeEnviar =
    tipo === 'obras'
      ? Boolean(autorizadorId) && obrasLinhas.every((l) => l.localObraId && l.subgrupoDespesaId && l.ordemInvestimento && l.valor)
      : Boolean(centroCustoId) &&
        linhas.every((l) => l.valor && (tipo === 'imobilizado' ? l.classeImobilizadoId : l.contaId && l.mes)) &&
        (tipo !== 'inclusao' && tipo !== 'imobilizado' ? true : Boolean(autorizadorId)) &&
        (tipo !== 'imobilizado' || Boolean(anexo));

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 p-6">
      <h1 className="text-lg font-semibold text-foreground">Nova solicitação</h1>

      {opcoesError && (
        <p className="text-sm text-destructive">{(opcoesError as Error).message}</p>
      )}

      <div className="grid gap-1.5">
        <Label>Tipo de solicitação</Label>
        <Select value={tipo} onValueChange={(v) => handleTipoChange(v as TipoSolicitacao)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="transferencia">Transferência</SelectItem>
            <SelectItem value="inclusao">Inclusão</SelectItem>
            <SelectItem value="inclusao_sfc">Inclusão SFC</SelectItem>
            <SelectItem value="imobilizado">Imobilizado</SelectItem>
            <SelectItem value="obras">Obras</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {tipo !== 'obras' && (
        <div className="grid gap-1.5">
          <Label>Centro de custo</Label>
          <Select value={centroCustoId} onValueChange={setCentroCustoId}>
            <SelectTrigger>
              <SelectValue placeholder="Escolha o centro de custo" />
            </SelectTrigger>
            <SelectContent>
              {(opcoes?.centros_custo ?? []).map((cc) => (
                <SelectItem key={cc.id} value={cc.id}>
                  {cc.codigo} — {cc.nome}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {opcoes && opcoes.centros_custo.length === 0 && (
            <p className="text-xs text-destructive">
              Seu usuário não tem nenhum centro de custo autorizado ainda — fale com o administrador.
            </p>
          )}
        </div>
      )}

      {tipo !== 'obras' &&
        linhas.map((linha, i) => (
          <div key={i} className="grid grid-cols-2 gap-3 rounded-md border border-border p-4">
            {tipo === 'transferencia' && (
              <div className="col-span-2 text-sm font-medium capitalize">{linha.lado}</div>
            )}
            {tipo === 'imobilizado' ? (
              <div className="col-span-2 grid gap-1.5">
                <Label>Classe de imobilizado</Label>
                <Select value={linha.classeImobilizadoId} onValueChange={(v) => atualizarLinha(i, 'classeImobilizadoId', v)}>
                  <SelectTrigger>
                    <SelectValue placeholder="Escolha a classe" />
                  </SelectTrigger>
                  <SelectContent>
                    {(opcoes?.classes_imobilizado ?? []).map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        {c.codigo} — {c.nome}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : (
              <>
                <div className="grid gap-1.5">
                  <Label>Conta ({tipo === 'inclusao_sfc' ? 'SFC' : 'BIFC'})</Label>
                  <Select value={linha.contaId} onValueChange={(v) => atualizarLinha(i, 'contaId', v)}>
                    <SelectTrigger>
                      <SelectValue placeholder="Escolha a conta" />
                    </SelectTrigger>
                    <SelectContent>
                      {contasDisponiveis.map((c) => (
                        <SelectItem key={c.id} value={c.id}>
                          {c.codigo} — {c.nome}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1.5">
                  <Label>Mês de competência</Label>
                  <Input type="month" value={linha.mes} onChange={(e) => atualizarLinha(i, 'mes', e.target.value)} />
                </div>
              </>
            )}
            <div className="grid gap-1.5">
              <Label>Valor (R$)</Label>
              <Input
                type="number"
                step="0.01"
                min="0"
                value={linha.valor}
                onChange={(e) => atualizarLinha(i, 'valor', e.target.value)}
              />
            </div>
          </div>
        ))}

      {tipo === 'transferencia' && (
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => setLinhas((a) => [...a, novaLinhaLancamento('origem')])}>
            + linha de origem
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => setLinhas((a) => [...a, novaLinhaLancamento('destino')])}>
            + linha de destino
          </Button>
        </div>
      )}

      {tipo === 'imobilizado' && (
        <div className="grid gap-1.5">
          <Label>Anexo de cotação (obrigatório)</Label>
          <Input type="file" onChange={handleAnexoChange} />
          {anexo && <p className="text-xs text-muted-foreground">Selecionado: {anexo.nome}</p>}
        </div>
      )}

      {tipo === 'obras' && (
        <>
          <div className="grid gap-1.5">
            <Label>Classificação</Label>
            <Select value={classificacao} onValueChange={setClassificacao}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {classificacoesObras.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {obrasLinhas.map((linha, i) => (
            <div key={i} className="grid grid-cols-2 gap-3 rounded-md border border-border p-4">
              <div className="grid gap-1.5">
                <Label>Local de obra</Label>
                <Select value={linha.localObraId} onValueChange={(v) => atualizarLinhaObra(i, 'localObraId', v)}>
                  <SelectTrigger>
                    <SelectValue placeholder="Escolha" />
                  </SelectTrigger>
                  <SelectContent>
                    {(opcoes?.locais_obra ?? []).map((o) => (
                      <SelectItem key={o.id} value={o.id}>
                        {o.codigo} — {o.nome}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-1.5">
                <Label>Subgrupo de despesa</Label>
                <Select value={linha.subgrupoDespesaId} onValueChange={(v) => atualizarLinhaObra(i, 'subgrupoDespesaId', v)}>
                  <SelectTrigger>
                    <SelectValue placeholder="Escolha" />
                  </SelectTrigger>
                  <SelectContent>
                    {(opcoes?.subgrupos_despesa ?? []).map((o) => (
                      <SelectItem key={o.id} value={o.id}>
                        {o.codigo} — {o.nome}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-1.5">
                <Label>Ordem de investimento</Label>
                <OrdemInvestimentoSelect
                  localObraId={linha.localObraId}
                  subgrupoDespesaId={linha.subgrupoDespesaId}
                  value={linha.ordemInvestimento}
                  onChange={(v) => atualizarLinhaObra(i, 'ordemInvestimento', v)}
                />
              </div>
              <div className="grid gap-1.5">
                <Label>Valor (R$)</Label>
                <Input
                  type="number"
                  step="0.01"
                  min="0"
                  value={linha.valor}
                  onChange={(e) => atualizarLinhaObra(i, 'valor', e.target.value)}
                />
              </div>
              {classificacao === 'transferencia_saldo' && (
                <div className="col-span-2 grid gap-1.5">
                  <Label>Lado</Label>
                  <Select value={linha.lado} onValueChange={(v) => atualizarLinhaObra(i, 'lado', v)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="retirada">Retirada</SelectItem>
                      <SelectItem value="inclusao">Inclusão</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>
          ))}

          <Button type="button" variant="outline" size="sm" onClick={() => setObrasLinhas((a) => [...a, novaLinhaObra()])}>
            + linha
          </Button>
        </>
      )}

      {precisaAutorizador && (
        <div className="grid gap-1.5">
          <Label>Autorizador</Label>
          <Select value={autorizadorId} onValueChange={setAutorizadorId}>
            <SelectTrigger>
              <SelectValue placeholder="Escolha o autorizador" />
            </SelectTrigger>
            <SelectContent>
              {(autorizadores?.items ?? []).map((a) => (
                <SelectItem key={a.colaborador_id} value={a.colaborador_id}>
                  {a.nome}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {autorizadores && autorizadores.items.length === 0 && valorParaAutorizador > 0 && (
            <p className="text-xs text-destructive">
              Nenhum autorizador cadastrado para esse CC/faixa de valor.
            </p>
          )}
        </div>
      )}

      <Button onClick={handleSubmit} disabled={!podeEnviar || enviando}>
        {enviando ? 'Enviando...' : 'Abrir solicitação'}
      </Button>
    </div>
  );
};

export default AbrirSolicitacao;
