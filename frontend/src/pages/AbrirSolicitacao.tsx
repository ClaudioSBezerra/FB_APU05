import { useEffect, useMemo, useRef, useState } from 'react';
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Search } from 'lucide-react';
import { MesAno } from '@/components/MesAno';
import { useSaldoOrcamento, type SaldoLinha } from '@/hooks/useSaldoOrcamento';

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
  // Todos os CCs (destino de Transferência, 11/11) — centros_custo traz só os autorizados.
  centros_custo_destino?: CentroCustoOpcao[];
  // Origem da Transferência: todos os CCs na fase de testes; senão igual a centros_custo.
  centros_custo_origem?: CentroCustoOpcao[];
  divisoes: OpcaoItem[];
  contas: OpcaoItem[]; // plano de contas do SAP
  classes_imobilizado: OpcaoItem[];
  locais_obra: OpcaoItem[];
  subgrupos_despesa: OpcaoItem[];
}

// Saldo orçamentário do SAP (aviso, nunca trava): mostra o disponível ACUMULADO até o mês da linha.
// "Saldo Não Identificado" quando o SAP não responde ou não foi possível consultar.
const moedaSaldo = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

function SaldoDaLinha({ saldo, carregando }: { saldo?: SaldoLinha; carregando: boolean }) {
  if (!saldo) return carregando ? <p className="text-xs text-fg-helper">Consultando o saldo no SAP…</p> : null;
  if (saldo.status === 'nao_identificado') {
    return (
      <p className="text-xs text-fg-helper" title={saldo.motivo}>
        Saldo Não Identificado
      </p>
    );
  }
  const acum = saldo.disponivel_acumulado ?? 0;
  const tit = `Planejado ${moedaSaldo.format(saldo.planejado ?? 0)} · realizado ${moedaSaldo.format(saldo.realizado ?? 0)} · comprometido ${moedaSaldo.format(saldo.comprometido ?? 0)} no mês`;
  return (
    <p className={saldo.excede ? 'text-xs font-medium text-danger-text' : 'text-xs text-fg-secondary'} title={tit} role={saldo.excede ? 'alert' : 'status'}>
      Saldo disponível até este mês (acumulado): <span className="font-medium tabular-nums">{moedaSaldo.format(acum)}</span>
      {saldo.status === 'sem_lancamentos' && ' · sem orçamento nem lançamentos no SAP'}
      {saldo.excede && saldo.falta != null && (
        <>
          {' '}
          · ⚠ retirada acumulada de {moedaSaldo.format(saldo.retirada_acumulada ?? 0)} passa do saldo em <span className="tabular-nums">{moedaSaldo.format(saldo.falta)}</span> (aviso: o envio não é bloqueado)
        </>
      )}
    </p>
  );
}

interface LinhaLancamento {
  lado: 'origem' | 'destino';
  contaId: string;
  classeImobilizadoId: string;
  mes: string;
  valor: string;
}

// O local de obra NÃO fica na linha: é escolhido uma vez para a solicitação
// inteira (obraLocalId) e repetido em cada linha só no payload — o backend
// continua gravando local_obra_id por linha (solicitacao_obras_linhas).
interface LinhaObra {
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
  return { subgrupoDespesaId: '', ordemInvestimento: '', valor: '', lado: 'retirada' };
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
        <SelectValue placeholder={habilitado ? 'Ordem de investimento' : !localObraId ? 'Escolha o local de obra' : 'Escolha o subgrupo'} />
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

// Busca sem acento/caixa: "manutencao" acha "Manutenção".
const normalizar = (t: string) => t.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase();

const reais = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

// ContaCombobox (11/11, item 1) — escolha de conta com busca por qualquer
// parte do código OU da descrição (todos os termos digitados precisam
// aparecer, em qualquer ordem). Teclado: ↑/↓ navega, Enter escolhe, Esc fecha.
function ContaCombobox({
  value,
  onChange,
  opcoes,
  placeholder = 'Escolha a conta',
  vazio = 'Nenhuma conta encontrada.',
  limparRotulo,
}: {
  value: string;
  onChange: (id: string) => void;
  opcoes: OpcaoItem[];
  placeholder?: string;
  vazio?: string;
  // quando informado, a primeira opção da lista limpa a escolha (value '') com este texto
  limparRotulo?: string;
}) {
  const [aberto, setAberto] = useState(false);
  const [busca, setBusca] = useState('');
  const [indice, setIndice] = useState(0);
  const raiz = useRef<HTMLDivElement>(null);
  // índice normalizado uma vez por lista (listas de milhares de itens: não refazer a cada tecla)
  const indiceBusca = useMemo(() => {
    const base: OpcaoItem[] = limparRotulo ? [{ id: '', codigo: '', nome: limparRotulo }, ...opcoes] : opcoes;
    return base.map((o) => ({ o, alvo: normalizar(`${o.codigo} ${o.nome}`) }));
  }, [opcoes, limparRotulo]);
  const selecionada = indiceBusca.find((x) => x.o.id === value)?.o;

  const resultados = useMemo(() => {
    const termos = normalizar(busca).split(/\s+/).filter(Boolean);
    const achados = termos.length === 0 ? indiceBusca : indiceBusca.filter((x) => termos.every((t) => x.alvo.includes(t)));
    return achados.map((x) => x.o);
  }, [busca, indiceBusca]);
  const visiveis = resultados.slice(0, 60);

  useEffect(() => {
    if (!aberto) return;
    function fora(e: MouseEvent) {
      if (raiz.current && !raiz.current.contains(e.target as Node)) setAberto(false);
    }
    document.addEventListener('mousedown', fora);
    return () => document.removeEventListener('mousedown', fora);
  }, [aberto]);

  function escolher(o: OpcaoItem) {
    onChange(o.id);
    setAberto(false);
    setBusca('');
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIndice((i) => Math.min(i + 1, visiveis.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIndice((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && visiveis[indice]) {
      e.preventDefault();
      escolher(visiveis[indice]);
    } else if (e.key === 'Escape') {
      setAberto(false);
    }
  }

  return (
    <div ref={raiz} className="relative">
      {aberto ? (
        <Input
          autoFocus
          role="combobox"
          aria-expanded="true"
          placeholder="Digite parte do código ou da descrição"
          value={busca}
          onChange={(e) => {
            setBusca(e.target.value);
            setIndice(0);
          }}
          onKeyDown={handleKeyDown}
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            setAberto(true);
            setIndice(0);
          }}
          className="flex h-10 w-full items-center justify-between rounded-md border border-input bg-card px-3 py-2 text-left text-sm focus:outline-none focus:border-ring focus:ring-1 focus:ring-ring"
        >
          <span className={selecionada ? 'truncate text-foreground' : 'truncate text-fg-muted'}>
            {selecionada ? (selecionada.codigo ? `${selecionada.codigo} — ${selecionada.nome}` : selecionada.nome) : placeholder}
          </span>
          <Search className="size-4 shrink-0 text-fg-muted" />
        </button>
      )}
      {aberto && (
        <div className="absolute z-30 mt-1 max-h-72 w-full overflow-y-auto rounded-md border border-border bg-card shadow-popover" role="listbox">
          {visiveis.length === 0 && <p className="p-3 text-xs text-fg-helper">{vazio}</p>}
          {visiveis.map((o, i) => (
            <button
              key={o.id}
              type="button"
              role="option"
              aria-selected={o.id === value}
              className={`block w-full px-3 py-2 text-left text-sm ${i === indice ? 'bg-muted' : 'hover:bg-muted/60'}`}
              onMouseEnter={() => setIndice(i)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => escolher(o)}
            >
              {o.codigo ? (
                <>
                  <span className="font-medium">{o.codigo}</span> — {o.nome}
                </>
              ) : (
                o.nome
              )}
            </button>
          ))}
          {resultados.length > visiveis.length && (
            <p className="border-t border-border p-2 text-xs text-fg-helper">
              Mostrando {visiveis.length} de {resultados.length}. Digite mais para refinar.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

// CNPJ (14) / CPF (11) com máscara — só exibição; o backend guarda dígitos.
function formatarDocumento(valor: string) {
  const d = valor.replace(/\D/g, '').slice(0, 14);
  if (d.length <= 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{1,2})$/, '$1.$2.$3-$4');
  return d.replace(/^(\d{2})(\d{3})(\d{3})(\d{4})(\d{1,2})$/, '$1.$2.$3/$4-$5');
}

interface FornecedorOpcao {
  id: string;
  cgc: string;
  razsoc: string;
  fantas: string;
  uf: string;
}

// CampoFornecedor (20/10) — texto livre (continua um VARCHAR(255) livre em
// solicitacoes, igual ao formulário legado — fornecedor novo, ainda não
// sincronizado do Oracle, pode ser digitado direto) + popup de consulta
// contra o cadastro de fornecedores (GET /api/formulario/fornecedores?
// busca=cgc/razsoc/fantas) — mesmo "zoom"/lookup clássico de ERP: digita
// CGC ou nome, o resultado mais próximo do topo da lista fica destacado
// (seta ↑/↓ troca, Enter ou clique confirma), igual ao pedido do negócio.
// Debounce de 300ms: a rota é ILIKE sem índice de trigram, não dá pra
// disparar uma query por tecla.
function CampoFornecedor({
  valor,
  onChange,
  cnpj,
  onCnpjChange,
  naoCadastrado,
  onNaoCadastradoChange,
}: {
  valor: string;
  onChange: (v: string) => void;
  cnpj: string;
  onCnpjChange: (v: string) => void;
  naoCadastrado: boolean;
  onNaoCadastradoChange: (v: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [busca, setBusca] = useState('');
  const [buscaDebounced, setBuscaDebounced] = useState('');
  const [indice, setIndice] = useState(0);

  useEffect(() => {
    const t = setTimeout(() => {
      setBuscaDebounced(busca.trim());
      setIndice(0);
    }, 300);
    return () => clearTimeout(t);
  }, [busca]);

  const { data, isFetching } = useQuery({
    queryKey: ['fornecedores', buscaDebounced],
    queryFn: () =>
      fetchJSON<{ items: FornecedorOpcao[] }>(`/api/formulario/fornecedores?busca=${encodeURIComponent(buscaDebounced)}`),
    enabled: open && buscaDebounced.length >= 2,
  });
  const resultados = data?.items ?? [];

  function abrirPopup() {
    setBusca('');
    setIndice(0);
    setOpen(true);
  }

  // A consulta traz o CNPJ junto (11/11, item 4) e desmarca "não cadastrado".
  function selecionar(f: FornecedorOpcao) {
    onChange(f.razsoc);
    onCnpjChange(f.cgc);
    onNaoCadastradoChange(false);
    setOpen(false);
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIndice((i) => Math.min(i + 1, resultados.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIndice((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && resultados[indice]) {
      e.preventDefault();
      selecionar(resultados[indice]);
    }
  }

  return (
    <div className="grid gap-1.5">
      <Label>Fornecedor da despesa</Label>
      <div className="grid gap-2 sm:grid-cols-[1fr_200px]">
        <div className="flex gap-2">
          <Input value={valor} onChange={(e) => onChange(e.target.value)} placeholder="Opcional" />
          <Button type="button" variant="outline" size="icon" onClick={abrirPopup} title="Consultar fornecedor cadastrado">
            <Search className="h-4 w-4" />
          </Button>
        </div>
        <Input
          value={formatarDocumento(cnpj)}
          onChange={(e) => onCnpjChange(e.target.value.replace(/\D/g, '').slice(0, 14))}
          placeholder="CNPJ"
          inputMode="numeric"
          aria-label="CNPJ do fornecedor"
        />
      </div>
      <label className="flex w-fit cursor-pointer items-center gap-2 text-sm text-fg-secondary">
        <input
          type="checkbox"
          className="size-4 cursor-pointer accent-[var(--blue-d50)]"
          checked={naoCadastrado}
          onChange={(e) => onNaoCadastradoChange(e.target.checked)}
        />
        Fornecedor não cadastrado
      </label>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Consultar fornecedor</DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            placeholder="Digite o CNPJ ou a razão social..."
            value={busca}
            onChange={(e) => setBusca(e.target.value)}
            onKeyDown={handleKeyDown}
          />
          <div className="max-h-72 overflow-y-auto rounded-md border border-border bg-card">
            {buscaDebounced.length < 2 && (
              <p className="p-3 text-xs text-muted-foreground">Digite ao menos 2 caracteres.</p>
            )}
            {buscaDebounced.length >= 2 && isFetching && (
              <p className="p-3 text-xs text-muted-foreground">Buscando...</p>
            )}
            {buscaDebounced.length >= 2 && !isFetching && resultados.length === 0 && (
              <p className="p-3 text-xs text-muted-foreground">Nenhum resultado</p>
            )}
            {resultados.map((f, i) => (
              <button
                key={f.id}
                type="button"
                className={`block w-full px-3 py-2 text-left text-sm ${i === indice ? 'bg-muted' : 'hover:bg-muted/60'}`}
                onMouseEnter={() => setIndice(i)}
                onClick={() => selecionar(f)}
              >
                <span className="font-medium">{f.razsoc}</span>
                {f.fantas ? ` (${f.fantas})` : ''} — {formatarDocumento(f.cgc)}
                {f.uf ? ` — ${f.uf}` : ''}
              </button>
            ))}
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              Cancelar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
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
  // Fornecedor/Descrição (20/10) — paridade com o formulário legado (Jira):
  // texto livre, opcionais para todos os tipos, inclusive Obras.
  const [fornecedor, setFornecedor] = useState('');
  const [descricao, setDescricao] = useState('');
  const [fornecedorCnpj, setFornecedorCnpj] = useState('');
  const [fornecedorNaoCadastrado, setFornecedorNaoCadastrado] = useState(false);
  // Transferência (11/11, item 2): CC do destino ('' = mesmo CC da origem).
  const [ccDestinoId, setCcDestinoId] = useState('');
  // Imobilizado (11/11, item 8).
  const [quantidadeMaterial, setQuantidadeMaterial] = useState('');
  const [classificacao, setClassificacao] = useState('inclusao');
  const [obrasLinhas, setObrasLinhas] = useState<LinhaObra[]>([novaLinhaObra()]);
  const [obraLocalId, setObraLocalId] = useState('');
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
    setFornecedor('');
    setDescricao('');
    setFornecedorCnpj('');
    setFornecedorNaoCadastrado(false);
    setCcDestinoId('');
    setQuantidadeMaterial('');
    if (novoTipo === 'transferencia') {
      setLinhas([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
    } else if (novoTipo === 'obras') {
      setObrasLinhas([novaLinhaObra()]);
      setObraLocalId('');
    } else {
      setLinhas([novaLinhaLancamento('destino')]);
    }
  }

  const opcoesOrigemTodas = (tipo === 'transferencia' ? opcoes?.centros_custo_origem : undefined) ?? opcoes?.centros_custo ?? [];
  const centroCustoSelecionado = opcoesOrigemTodas.find((cc) => cc.id === centroCustoId);

  const linhasOrigem = linhas.filter((l) => l.lado === 'origem');
  const somaOrigens = Math.round(linhasOrigem.reduce((acc, l) => acc + (parseFloat(l.valor) || 0), 0) * 100) / 100;
  // saldo do SAP só da ORIGEM da transferência (decisão com a Wanessa, 23/11/2026): é de onde sai o valor
  const { linhas: saldos, carregando: carregandoSaldo, ativo: saldoAtivo } = useSaldoOrcamento(
    linhas.map((l) => ({
      lado: l.lado,
      centro_custo_id: l.lado === 'origem' ? centroCustoId : '',
      conta_id: l.contaId,
      mes: l.mes ? `${l.mes}-01` : '',
      valor: parseFloat(l.valor) || 0,
    })),
    tipo === 'transferencia',
  );
  const saldoDe = (i: number) => saldos.find((x) => x.indice === i);
  const ccDestino = ccDestinoId
    ? opcoes?.centros_custo_destino?.find((cc) => cc.id === ccDestinoId)
    : centroCustoSelecionado;

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
    setFornecedor('');
    setDescricao('');
    setFornecedorCnpj('');
    setFornecedorNaoCadastrado(false);
    setCcDestinoId('');
    setQuantidadeMaterial('');
    if (tipo === 'transferencia') {
      setLinhas([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
    } else if (tipo === 'obras') {
      setClassificacao('inclusao');
      setObrasLinhas([novaLinhaObra()]);
      setObraLocalId('');
    } else {
      setLinhas([novaLinhaLancamento('destino')]);
    }
  }

  async function handleSubmit() {
    setEnviando(true);
    try {
      let payload: Record<string, unknown>;

      // Fornecedor/Descrição/Anexo (20/10) — mesmos 3 campos opcionais para
      // todos os tipos, inclusive Obras (paridade com o formulário legado).
      const camposComuns = {
        ...(fornecedor.trim() ? { fornecedor: fornecedor.trim() } : {}),
        ...(descricao.trim() ? { descricao: descricao.trim() } : {}),
        ...(fornecedorCnpj ? { fornecedor_cnpj: fornecedorCnpj } : {}),
        ...(fornecedorNaoCadastrado ? { fornecedor_nao_cadastrado: true } : {}),
        ...(anexo
          ? { anexos: [{ nome_arquivo: anexo.nome, content_type: anexo.contentType, conteudo_base64: anexo.base64 }] }
          : {}),
      };

      if (tipo === 'obras') {
        payload = {
          tipo_solicitacao: 'obras',
          classificacao,
          autorizador_id: autorizadorId,
          ...camposComuns,
          obras_linhas: obrasLinhas.map((l) => ({
            local_obra_id: obraLocalId,
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
          ...(tipo === 'imobilizado' ? { quantidade_material: parseInt(quantidadeMaterial, 10) || 0 } : {}),
          ...camposComuns,
          lancamentos: linhas.map((l) => ({
            lado: l.lado,
            // Destino de Transferência pode ser de outro CC (divisão do próprio CC).
            divisao_id: tipo === 'transferencia' && l.lado === 'destino' ? ccDestino?.divisao_id ?? divisaoId : divisaoId,
            centro_custo_id: tipo === 'transferencia' && l.lado === 'destino' ? ccDestino?.id ?? centroCustoId : centroCustoId,
            // O destino é sempre a soma das origens (arredondada a centavos).
            valor: tipo === 'transferencia' && l.lado === 'destino' ? somaOrigens : parseFloat(l.valor) || 0,
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

  const contasDisponiveis = opcoes?.contas ?? [];
  // Anexo obrigatório em todos os tipos desde 11/11 (item 5).
  const podeEnviar =
    Boolean(anexo) &&
    (tipo === 'obras'
      ? Boolean(autorizadorId) && Boolean(obraLocalId) && obrasLinhas.every((l) => l.subgrupoDespesaId && l.ordemInvestimento && l.valor)
      : tipo === 'transferencia'
        ? Boolean(centroCustoId) &&
          linhas.every((l) => l.contaId && l.mes && (l.lado === 'destino' || parseFloat(l.valor) > 0)) &&
          somaOrigens > 0
        : Boolean(centroCustoId) &&
          linhas.every((l) => l.valor && (tipo === 'imobilizado' ? l.classeImobilizadoId : l.contaId && l.mes)) &&
          (tipo !== 'inclusao' && tipo !== 'imobilizado' ? true : Boolean(autorizadorId)) &&
          (tipo !== 'imobilizado' || parseInt(quantidadeMaterial, 10) > 0));

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
          <Label>{tipo === 'transferencia' ? 'Centro de custo de origem' : 'Centro de custo'}</Label>
          <ContaCombobox
            value={centroCustoId}
            onChange={setCentroCustoId}
            opcoes={opcoesOrigemTodas}
            placeholder="Escolha o centro de custo"
            vazio="Nenhum centro de custo encontrado."
          />
          {opcoes && opcoesOrigemTodas.length === 0 && (
            <p className="text-xs text-destructive">
              Seu usuário não tem nenhum centro de custo autorizado ainda — fale com o administrador.
            </p>
          )}
        </div>
      )}

      {tipo === 'transferencia' && (
        <>
          <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
            <h2 className="text-sm font-semibold text-foreground">Origem</h2>
            <p className="-mt-2 text-xs text-fg-helper">
              Uma linha por mês de onde o valor sai, todas no centro de custo de origem.
            </p>
            {linhas.map((linha, i) =>
              linha.lado !== 'origem' ? null : (
                <div key={i} className="grid grid-cols-1 gap-3 border-t border-border pt-3 first-of-type:border-0 sm:grid-cols-[17rem_9rem_auto] sm:items-end">
                  <div className="grid gap-1.5 sm:col-span-3">
                    <Label>Conta</Label>
                    <ContaCombobox value={linha.contaId} onChange={(v) => atualizarLinha(i, 'contaId', v)} opcoes={contasDisponiveis} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Mês</Label>
                    <MesAno value={linha.mes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Valor (R$)</Label>
                    <Input type="number" step="0.01" min="0" value={linha.valor} onChange={(e) => atualizarLinha(i, 'valor', e.target.value)} />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={linhasOrigem.length <= 1}
                    onClick={() => setLinhas((atual) => atual.filter((_, j) => j !== i))}
                  >
                    Remover
                  </Button>
                  <div className="sm:col-span-3">
                    {saldoAtivo && <SaldoDaLinha saldo={saldoDe(i)} carregando={carregandoSaldo} />}
                  </div>
                </div>
              ),
            )}
            <Button type="button" variant="outline" size="sm" className="self-start" onClick={() =>
                setLinhas((a) => {
                  // a nova linha repete a conta da última origem: normalmente só o mês muda (chamado 1 para 1)
                  const ultima = [...a].reverse().find((l) => l.lado === 'origem');
                  return [...a, { ...novaLinhaLancamento('origem'), contaId: ultima?.contaId ?? '' }];
                })
              }>
              + linha de origem
            </Button>
          </section>

          <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
            <h2 className="text-sm font-semibold text-foreground">Destino</h2>
            <div className="grid gap-1.5">
              <Label>Centro de custo de destino</Label>
              <ContaCombobox
                value={ccDestinoId}
                onChange={setCcDestinoId}
                opcoes={opcoes?.centros_custo_destino ?? []}
                placeholder="Mesmo centro de custo da origem"
                vazio="Nenhum centro de custo encontrado."
                limparRotulo="Mesmo centro de custo da origem"
              />
            </div>
            {linhas.map((linha, i) =>
              linha.lado !== 'destino' ? null : (
                <div key={i} className="grid grid-cols-1 gap-3 sm:grid-cols-[17rem_9rem] sm:items-end">
                  <div className="grid gap-1.5 sm:col-span-2">
                    <Label>Conta</Label>
                    <ContaCombobox value={linha.contaId} onChange={(v) => atualizarLinha(i, 'contaId', v)} opcoes={contasDisponiveis} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Mês</Label>
                    <MesAno value={linha.mes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Valor (soma)</Label>
                    <div className="flex h-10 items-center rounded-md border border-input bg-[var(--bg-input-disabled)] px-3 text-sm tabular-nums text-foreground" aria-live="polite">
                      {reais.format(somaOrigens)}
                    </div>
                  </div>
                </div>
              ),
            )}
          </section>
        </>
      )}

      {tipo !== 'obras' &&
        tipo !== 'transferencia' &&
        linhas.map((linha, i) => (
          <div key={i} className="grid grid-cols-2 gap-3 rounded-lg border border-border bg-card p-4">
            {tipo === 'imobilizado' ? (
              <>
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
                <div className="grid gap-1.5">
                  <Label>Quantidade do material</Label>
                  <Input
                    type="number"
                    min="1"
                    step="1"
                    inputMode="numeric"
                    value={quantidadeMaterial}
                    onChange={(e) => setQuantidadeMaterial(e.target.value.replace(/\D/g, ''))}
                  />
                </div>
              </>
            ) : (
              <>
                <div className="grid gap-1.5">
                  <Label>Conta</Label>
                  <ContaCombobox value={linha.contaId} onChange={(v) => atualizarLinha(i, 'contaId', v)} opcoes={contasDisponiveis} />
                </div>
                <div className="grid gap-1.5">
                  <Label>Mês de competência</Label>
                  <MesAno value={linha.mes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
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

          <div className="grid gap-1.5">
            <Label>Local de obra</Label>
            <Select
              value={obraLocalId}
              onValueChange={(v) => {
                setObraLocalId(v);
                // A ordem de investimento depende do local — as já escolhidas
                // podem não existir no local novo, então são limpas.
                setObrasLinhas((atual) => atual.map((l) => ({ ...l, ordemInvestimento: '' })));
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="Escolha o local de obra" />
              </SelectTrigger>
              <SelectContent>
                {(opcoes?.locais_obra ?? []).map((o) => (
                  <SelectItem key={o.id} value={o.id}>
                    {o.codigo} — {o.nome}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-fg-helper">Vale para todas as linhas desta solicitação.</p>
          </div>

          {obrasLinhas.map((linha, i) => (
            <div key={i} className="grid grid-cols-2 gap-3 rounded-lg border border-border bg-card p-4">
              <div className="col-span-2 flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">Linha {i + 1}</span>
                {obrasLinhas.length > 1 && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setObrasLinhas((atual) => atual.filter((_, j) => j !== i))}
                  >
                    Remover linha
                  </Button>
                )}
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
                  localObraId={obraLocalId}
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
                <div className="grid gap-1.5">
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

          <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => setObrasLinhas((a) => [...a, novaLinhaObra()])}>
            + linha
          </Button>
        </>
      )}

      <CampoFornecedor
        valor={fornecedor}
        onChange={setFornecedor}
        cnpj={fornecedorCnpj}
        onCnpjChange={setFornecedorCnpj}
        naoCadastrado={fornecedorNaoCadastrado}
        onNaoCadastradoChange={setFornecedorNaoCadastrado}
      />

      <div className="grid gap-1.5">
        <Label>Descrição detalhada</Label>
        <Input value={descricao} onChange={(e) => setDescricao(e.target.value)} placeholder="Opcional — item e onde será aplicado" />
      </div>

      <div className="grid gap-1.5">
        <Label>
          Anexo <span className="text-destructive" aria-hidden>*</span>
        </Label>
        <Input type="file" onChange={handleAnexoChange} />
        {anexo && <p className="text-xs text-muted-foreground">Selecionado: {anexo.nome}</p>}
      </div>

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
