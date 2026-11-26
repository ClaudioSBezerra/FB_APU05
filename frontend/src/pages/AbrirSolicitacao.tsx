import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
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
import { ValorInput } from '@/components/ValorInput';
import { BuscaCombobox, type OpcaoBusca } from '@/components/BuscaCombobox';
import { DivisaoCentroCusto, type CentroCustoOpcao } from '@/components/DivisaoCentroCusto';
import { TabelaSaldoMeses, rotuloMes } from '@/components/TabelaSaldoMeses';
import { useSaldoMeses } from '@/hooks/useSaldoMeses';
import { BuscaPessoa, type Pessoa, type GrupoOpcao } from '@/components/CompartilharChamado';

// Tela de abertura de solicitação (Epic 3 — Stories 3.1 a 3.5). Cobre os 5
// tipos aceitos por POST /api/solicitacoes (handlers/solicitacoes.go):
// transferencia, inclusao, inclusao_sfc, imobilizado, obras. O servidor é
// sempre a fonte da verdade: esta tela só monta o payload e antecipa as
// mesmas regras para o usuário (o servidor confere tudo de novo).
//
// Ajustes de 26/11/2026 (sinalizações da equipe + resposta da Controladoria):
//   A1 tipo de compra no Imobilizado · A2 Transferência com uma conta por lado, só os meses variam ·
//   A3 descrição obrigatória com limite · A4 Imobilizado com valor unitário e total calculado ·
//   B1 rótulo "Nome Fornecedor da despesa" (Inclusão e Transferência) · B2 vários anexos, com remover e trocar ·
//   B3 divisão e CC separados · B4 CNPJ preenche o nome · B5 busca limpa ao fechar ·
//   C tabela de saldo da origem (jan até o mês) que BLOQUEIA o envio · Inclusão mostra o orçamento do mês (só informa) ·
//   Obras: "CRIAR" só onde a ordem nasce.

type TipoSolicitacao = 'transferencia' | 'inclusao' | 'inclusao_sfc' | 'imobilizado' | 'obras';

interface OpcoesFormulario {
  centros_custo: CentroCustoOpcao[];
  // Todos os CCs (destino de Transferência, 11/11) — centros_custo traz só os autorizados.
  centros_custo_destino?: CentroCustoOpcao[];
  // Origem da Transferência: todos os CCs na fase de testes; senão igual a centros_custo.
  centros_custo_origem?: CentroCustoOpcao[];
  divisoes: OpcaoBusca[];
  contas: OpcaoBusca[]; // plano de contas do SAP
  classes_imobilizado: OpcaoBusca[];
  locais_obra: OpcaoBusca[];
  subgrupos_despesa: OpcaoBusca[];
  // Regra do ano (data do SERVIDOR, fuso de Recife): só o corrente; em janeiro também o anterior.
  ano_corrente?: number;
  anos_permitidos?: number[];
  // Obras (retirada e transferência de saldo): ano anterior e atual.
  anos_obras?: number[];
}

const MAX_DESCRICAO = 2000; // igual a MaxDescricao no servidor
const MAX_VALOR = 100_000_000; // igual a MaxValorLinha no servidor
const MAX_MESES_LADO = 12;

const moeda = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });
const num = (v: string) => parseFloat(v) || 0;
const centavos = (v: number) => Math.round(v * 100) / 100;

// Linha de mês/valor. Na Transferência a conta é UMA por lado (contaOrigem/contaDestino); nos demais tipos há uma
// linha só e a conta fica nela.
interface LinhaLancamento {
  lado: 'origem' | 'destino';
  contaId: string;
  classeImobilizadoId: string;
  mes: string;
  valor: string;
}

// O local de obra NÃO fica na linha: é escolhido uma vez para a solicitação
// inteira (obraLocalId) e repetido em cada linha só no payload.
interface LinhaObra {
  subgrupoDespesaId: string;
  ordemInvestimento: string;
  valor: string;
  lado: 'retirada' | 'inclusao';
}

interface Anexo {
  chave: number;
  nome: string;
  contentType: string;
  base64: string;
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

// Obras (item 4 de 24/11): "CRIAR" só onde a ordem nasce — Inclusão, FL e o lado de inclusão da transferência de saldo.
function permiteCriar(classificacao: string, lado: LinhaObra['lado']) {
  if (classificacao === 'retirada_saldo') return false;
  if (classificacao === 'transferencia_saldo') return lado === 'inclusao';
  return true;
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

const EXTENSOES_ANEXO = /\.(pdf|jpe?g|png|docx?|xlsx?|msg|eml)$/i;
const ACCEPT_ANEXO = '.pdf,.jpg,.jpeg,.png,.doc,.docx,.xls,.xlsx,.msg,.eml';
const MAX_ANEXO_BYTES = 10 * 1024 * 1024;

async function lerAnexo(file: File, chave: number): Promise<Anexo | null> {
  // Avisa já na escolha (o servidor confere de novo pelo conteúdo do arquivo).
  if (!EXTENSOES_ANEXO.test(file.name)) {
    toast.error(`"${file.name}" não é um tipo aceito. Aceitos: PDF, imagem (JPG/PNG), Word, Excel e e-mail (.msg ou .eml).`);
    return null;
  }
  if (file.size > MAX_ANEXO_BYTES) {
    toast.error(`"${file.name}" passa de 10 MB.`);
    return null;
  }
  return { chave, nome: file.name, contentType: file.type || 'application/octet-stream', base64: await fileParaBase64(file) };
}

// OrdemInvestimentoSelect — combo de ordem de investimento para uma linha de Obras: as ordens já existentes para o
// par local/subgrupo escolhido (GET /api/formulario/obra-ordens) e, onde a ordem pode nascer, a opção "CRIAR".
function OrdemInvestimentoSelect({
  localObraId,
  subgrupoDespesaId,
  value,
  onChange,
  criar,
}: {
  localObraId: string;
  subgrupoDespesaId: string;
  value: string;
  onChange: (v: string) => void;
  criar: boolean;
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
  const ordens = data?.items ?? [];

  return (
    <>
      <Select value={value} onValueChange={onChange} disabled={!habilitado}>
        <SelectTrigger className="h-9">
          <SelectValue placeholder={habilitado ? 'Ordem de investimento' : !localObraId ? 'Escolha o local de obra' : 'Escolha o subgrupo'} />
        </SelectTrigger>
        <SelectContent>
          {criar && <SelectItem value="CRIAR">CRIAR (nova ordem, só na finalização)</SelectItem>}
          {ordens.map((numero) => (
            <SelectItem key={numero} value={numero}>
              {numero}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {habilitado && !criar && data && ordens.length === 0 && (
        <p className="text-xs text-destructive">Não há ordem cadastrada para este local e subgrupo. Na retirada a ordem precisa existir.</p>
      )}
    </>
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

// CampoFornecedor (20/10) — texto livre + popup de consulta ao cadastro de fornecedores (sincronizado do Oracle).
// B4 (26/11): digitar o CNPJ completo busca no MESMO cadastro e preenche o nome; se não achar, o nome continua livre
// e editável. B1: o rótulo muda por formulário.
function CampoFornecedor({
  rotulo,
  valor,
  onChange,
  cnpj,
  onCnpjChange,
  naoCadastrado,
  onNaoCadastradoChange,
}: {
  rotulo: string;
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
  // CNPJ procurado e não achado no cadastro (o aviso só aparece enquanto o campo tem esse mesmo CNPJ)
  const [cnpjNaoAchado, setCnpjNaoAchado] = useState('');
  const ultimoCnpjBuscado = useRef('');

  useEffect(() => {
    const t = setTimeout(() => {
      setBuscaDebounced(busca.trim());
      setIndice(0);
    }, 300);
    return () => clearTimeout(t);
  }, [busca]);

  // B4: CNPJ (14) ou CPF (11) completo → procura no cadastro e preenche o nome.
  useEffect(() => {
    const d = cnpj.replace(/\D/g, '');
    if ((d.length !== 14 && d.length !== 11) || d === ultimoCnpjBuscado.current) return;
    ultimoCnpjBuscado.current = d;
    let cancelado = false;
    fetchJSON<{ items: FornecedorOpcao[] }>(`/api/formulario/fornecedores?busca=${d}`)
      .then((r) => {
        if (cancelado) return;
        const achado = r.items.find((f) => f.cgc.replace(/\D/g, '') === d);
        if (achado) {
          onChange(achado.razsoc);
          onNaoCadastradoChange(false);
        } else {
          setCnpjNaoAchado(d);
        }
      })
      .catch(() => undefined);
    return () => {
      cancelado = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cnpj]);

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
    ultimoCnpjBuscado.current = f.cgc.replace(/\D/g, '');
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
      <Label>{rotulo}</Label>
      <div className="grid gap-2 sm:grid-cols-[1fr_200px]">
        <div className="flex gap-2">
          <Input value={valor} onChange={(e) => onChange(e.target.value)} placeholder="Opcional" maxLength={255} />
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
      {cnpjNaoAchado && cnpjNaoAchado === cnpj.replace(/\D/g, '') && (
        <p className="text-xs text-fg-helper">CNPJ não encontrado no cadastro de fornecedores. Digite o nome.</p>
      )}
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
            {buscaDebounced.length < 2 && <p className="p-3 text-xs text-muted-foreground">Digite ao menos 2 caracteres.</p>}
            {buscaDebounced.length >= 2 && isFetching && <p className="p-3 text-xs text-muted-foreground">Buscando...</p>}
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

// B2 (26/11): vários anexos; cada um pode ser removido ou trocado antes do envio. O envio leva só o que ficou.
function CampoAnexos({ anexos, onChange }: { anexos: Anexo[]; onChange: (a: Anexo[]) => void }) {
  const [chaveInput, setChaveInput] = useState(0);
  const proxima = useRef(1);
  const trocarInput = useRef<HTMLInputElement>(null);
  const [trocando, setTrocando] = useState<number | null>(null);

  async function adicionar(e: React.ChangeEvent<HTMLInputElement>) {
    const arquivos = Array.from(e.target.files ?? []);
    setChaveInput((k) => k + 1); // limpa o campo de arquivo: o nome fica só na lista abaixo
    const lidos = (await Promise.all(arquivos.map((f) => lerAnexo(f, proxima.current++)))).filter(Boolean) as Anexo[];
    if (lidos.length) onChange([...anexos, ...lidos]);
  }

  async function trocar(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f || trocando === null) return;
    const novo = await lerAnexo(f, proxima.current++);
    if (novo) onChange(anexos.map((a) => (a.chave === trocando ? novo : a)));
    setTrocando(null);
  }

  return (
    <div className="grid gap-1.5">
      <Label>
        Anexos <span className="text-destructive" aria-hidden>*</span>
      </Label>
      <Input key={chaveInput} type="file" multiple accept={ACCEPT_ANEXO} onChange={adicionar} aria-label="Adicionar anexo" />
      <input ref={trocarInput} type="file" accept={ACCEPT_ANEXO} className="hidden" onChange={trocar} aria-hidden tabIndex={-1} />
      <p className="text-xs text-fg-helper">Aceitos: PDF, imagem (JPG/PNG), Word, Excel e e-mail (.msg ou .eml), até 10 MB cada.</p>
      {anexos.length > 0 && (
        <ul className="divide-y divide-border rounded-md border border-border">
          {anexos.map((a) => (
            <li key={a.chave} className="flex items-center gap-2 px-3 py-1.5 text-sm">
              <span className="min-w-0 flex-1 truncate" title={a.nome}>
                {a.nome}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setTrocando(a.chave);
                  trocarInput.current?.click();
                }}
              >
                Trocar
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={() => onChange(anexos.filter((x) => x.chave !== a.chave))}>
                Remover
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// §9 (26/11): pessoas e grupos em cópia já na abertura. Acompanham o chamado, recebem os avisos e podem comentar.
function CampoVisualizadores({
  pessoas,
  onPessoas,
  grupos,
  onGrupos,
}: {
  pessoas: Pessoa[];
  onPessoas: (p: Pessoa[]) => void;
  grupos: string[];
  onGrupos: (g: string[]) => void;
}) {
  const { data } = useQuery({ queryKey: ['grupos-opcoes'], queryFn: () => fetchJSON<{ items: GrupoOpcao[] }>('/api/grupos/opcoes') });
  const todos = data?.items ?? [];
  const livres = todos.filter((g) => !grupos.includes(g.id));
  return (
    <div className="grid gap-1.5">
      <Label>Em cópia (opcional)</Label>
      <div className="grid gap-2 sm:grid-cols-2">
        <BuscaPessoa ignorar={pessoas.map((p) => p.id)} onEscolher={(p) => onPessoas([...pessoas, p])} placeholder="Pessoa: nome ou e-mail (3 letras)" />
        <Select value="" onValueChange={(g) => onGrupos([...grupos, g])}>
          <SelectTrigger aria-label="Grupo em cópia">
            <SelectValue placeholder={todos.length ? 'Grupo (ex.: Grupo Expansão)' : 'Nenhum grupo cadastrado'} />
          </SelectTrigger>
          <SelectContent>
            {livres.map((g) => (
              <SelectItem key={g.id} value={g.id}>
                {g.nome}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {(pessoas.length > 0 || grupos.length > 0) && (
        <div className="flex flex-wrap gap-1.5">
          {grupos.map((gid) => (
            <span key={gid} className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-xs">
              Grupo {todos.find((g) => g.id === gid)?.nome ?? ''}
              <button type="button" aria-label="Retirar grupo" className="text-fg-helper hover:text-foreground" onClick={() => onGrupos(grupos.filter((x) => x !== gid))}>
                ×
              </button>
            </span>
          ))}
          {pessoas.map((p) => (
            <span key={p.id} className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-xs" title={p.email}>
              {p.nome}
              <button type="button" aria-label={`Retirar ${p.nome}`} className="text-fg-helper hover:text-foreground" onClick={() => onPessoas(pessoas.filter((x) => x.id !== p.id))}>
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <p className="text-xs text-fg-helper">Quem está em cópia vê o chamado, recebe os avisos por e-mail e pode comentar.</p>
    </div>
  );
}

// Inclusão / Inclusão SFC (resposta de 26/11, §5): mostra o orçamento do destino no mês, só como informação.
function OrcamentoInformativo({ ccId, contaId, mes }: { ccId: string; contaId: string; mes: string }) {
  const { data, isFetching } = useSaldoMeses(ccId, contaId, mes.slice(0, 4));
  if (!ccId || !contaId || !/^\d{4}-\d{2}$/.test(mes)) return null;
  if (isFetching && !data) return <p className="text-xs text-fg-helper">Consultando o orçamento no SAP…</p>;
  if (!data || !data.ativo) return null;
  if (data.status === 'nao_identificado') {
    return (
      <p className="text-xs text-fg-helper" title={data.motivo}>
        Orçamento Não Identificado
      </p>
    );
  }
  const m = data.meses.find((x) => x.mes === mes);
  if (!m) return null;
  return (
    <p className="text-xs text-fg-secondary" role="status">
      Orçamento no mês: <span className="font-medium tabular-nums">{moeda.format(m.disponivel)}</span> · Acumulado até o mês:{' '}
      <span className="font-medium tabular-nums">{moeda.format(m.disponivel_acumulado)}</span>
      <span className="text-fg-helper"> (só informação; não bloqueia o envio)</span>
    </p>
  );
}

const AbrirSolicitacao = () => {
  const [tipo, setTipo] = useState<TipoSolicitacao>('transferencia');
  const [centroCustoId, setCentroCustoId] = useState('');
  const [linhas, setLinhas] = useState<LinhaLancamento[]>([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
  // Transferência (A2): uma conta por lado
  const [contaOrigem, setContaOrigem] = useState('');
  const [contaDestino, setContaDestino] = useState('');
  const [origemAtiva, setOrigemAtiva] = useState(0);
  const [autorizadorId, setAutorizadorId] = useState('');
  const [anexos, setAnexos] = useState<Anexo[]>([]);
  const [fornecedor, setFornecedor] = useState('');
  const [descricao, setDescricao] = useState('');
  const [fornecedorCnpj, setFornecedorCnpj] = useState('');
  const [fornecedorNaoCadastrado, setFornecedorNaoCadastrado] = useState(false);
  // Transferência (11/11, item 2): CC do destino ('' = mesmo CC da origem).
  const [ccDestinoId, setCcDestinoId] = useState('');
  // Imobilizado (11/11 item 8; A1/A4 de 26/11).
  const [quantidadeMaterial, setQuantidadeMaterial] = useState('');
  const [tipoCompra, setTipoCompra] = useState('');
  const [valorUnitario, setValorUnitario] = useState('');
  const [classificacao, setClassificacao] = useState('inclusao');
  const [anoObras, setAnoObras] = useState(''); // '' = ano atual (padrão do servidor)
  const [obrasLinhas, setObrasLinhas] = useState<LinhaObra[]>([novaLinhaObra()]);
  const [obraLocalId, setObraLocalId] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [emCopia, setEmCopia] = useState<Pessoa[]>([]);
  const [gruposCopia, setGruposCopia] = useState<string[]>([]);

  const { data: opcoes, error: opcoesError } = useQuery({
    queryKey: ['formulario-opcoes'],
    queryFn: () => fetchJSON<OpcoesFormulario>('/api/formulario/opcoes'),
  });

  function limparCampos(novoTipo: TipoSolicitacao) {
    setEmCopia([]);
    setGruposCopia([]);
    setAutorizadorId('');
    setAnexos([]);
    setFornecedor('');
    setDescricao('');
    setFornecedorCnpj('');
    setFornecedorNaoCadastrado(false);
    setCcDestinoId('');
    setQuantidadeMaterial('');
    setTipoCompra('');
    setValorUnitario('');
    setContaOrigem('');
    setContaDestino('');
    setOrigemAtiva(0);
    if (novoTipo === 'transferencia') {
      setLinhas([novaLinhaLancamento('origem'), novaLinhaLancamento('destino')]);
    } else if (novoTipo === 'obras') {
      setObrasLinhas([novaLinhaObra()]);
      setObraLocalId('');
    } else {
      setLinhas([novaLinhaLancamento('destino')]);
    }
  }

  // Ao trocar de tipo, reseta os campos específicos do tipo anterior (evita enviar lixo de um tipo para outro).
  function handleTipoChange(novoTipo: TipoSolicitacao) {
    setTipo(novoTipo);
    limparCampos(novoTipo);
  }

  function resetarFormulario() {
    setCentroCustoId('');
    if (tipo === 'obras') {
      setClassificacao('inclusao');
      setAnoObras('');
    }
    limparCampos(tipo);
  }

  const opcoesOrigemTodas = (tipo === 'transferencia' ? opcoes?.centros_custo_origem : undefined) ?? opcoes?.centros_custo ?? [];
  const centroCustoSelecionado = opcoesOrigemTodas.find((cc) => cc.id === centroCustoId);
  const ccDestino = ccDestinoId ? opcoes?.centros_custo_destino?.find((cc) => cc.id === ccDestinoId) : centroCustoSelecionado;

  // Ano único da solicitação (Transferência, Inclusão, Inclusão SFC e, desde 26/11, Imobilizado).
  const usaAnoUnico = tipo !== 'obras';
  const anosPermitidos = (opcoes?.anos_permitidos ?? []).map(String);
  const [anoEscolhido, setAnoEscolhido] = useState('');
  const anoOrcamento = anosPermitidos.includes(anoEscolhido) ? anoEscolhido : anosPermitidos[0] ?? '';
  function escolherAno(novo: string) {
    setAnoEscolhido(novo);
    // todas as linhas passam para o novo ano (a solicitação inteira é de um único ano)
    setLinhas((atual) => atual.map((l) => (l.mes ? { ...l, mes: `${novo}${l.mes.slice(4)}` } : l)));
  }
  const anoParaMes = usaAnoUnico && anoOrcamento ? anoOrcamento : undefined;
  const contasDisponiveis = opcoes?.contas ?? [];

  // ---------------------------------------------------------------- Transferência
  const indicesOrigem = linhas.map((l, i) => (l.lado === 'origem' ? i : -1)).filter((i) => i >= 0);
  const indicesDestino = linhas.map((l, i) => (l.lado === 'destino' ? i : -1)).filter((i) => i >= 0);
  const somaOrigens = centavos(indicesOrigem.reduce((acc, i) => acc + num(linhas[i].valor), 0));
  const destinoUnico = indicesDestino.length === 1;
  const somaDestinos = destinoUnico ? somaOrigens : centavos(indicesDestino.reduce((acc, i) => acc + num(linhas[i].valor), 0));
  const ativaIdx = indicesOrigem.includes(origemAtiva) ? origemAtiva : indicesOrigem[0] ?? 0;

  const saldoOrigem = useSaldoMeses(tipo === 'transferencia' ? centroCustoId : '', contaOrigem, anoOrcamento);
  const saldoValido = saldoOrigem.data?.ativo && (saldoOrigem.data.status === 'ok' || saldoOrigem.data.status === 'sem_lancamentos');
  const retiradas = useMemo(() => {
    const r: Record<string, number> = {};
    for (const i of indicesOrigem) {
      const l = linhas[i];
      if (l.mes) r[l.mes] = (r[l.mes] ?? 0) + num(l.valor);
    }
    return r;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linhas]);

  // Item 6/C: a retirada de cada mês não passa do "pode remanejar", e a acumulada não passa do acumulado (bloqueia).
  const errosSaldo: Record<string, string> = {};
  if (tipo === 'transferencia' && saldoValido) {
    let acum = 0;
    for (const m of saldoOrigem.data!.meses) {
      const r = retiradas[m.mes] ?? 0;
      acum += r;
      if (r <= 0) continue;
      if (r > m.pode_remanejar + 0.005) {
        errosSaldo[m.mes] = `${rotuloMes(m.mes)}: ${moeda.format(r)} passa do que pode sair do mês (${moeda.format(m.pode_remanejar)}) em ${moeda.format(r - m.pode_remanejar)}.`;
      } else if (acum > m.disponivel_acumulado + 0.005) {
        errosSaldo[m.mes] = `${rotuloMes(m.mes)}: a retirada acumulada de ${moeda.format(acum)} passa do acumulado até o mês (${moeda.format(Math.max(0, m.disponivel_acumulado))}).`;
      }
    }
  }
  const mesesRepetidos = (idx: number[]) => {
    const vistos = new Set<string>();
    for (const i of idx) {
      const m = linhas[i].mes;
      if (!m) continue;
      if (vistos.has(m)) return true;
      vistos.add(m);
    }
    return false;
  };

  function adicionarLinha(lado: 'origem' | 'destino') {
    if (lado === 'origem') setOrigemAtiva(linhas.length);
    setLinhas((a) => [...a, novaLinhaLancamento(lado)]);
  }

  // ---------------------------------------------------------------- autorizador
  const valorImobilizado = centavos((parseInt(quantidadeMaterial, 10) || 0) * num(valorUnitario));
  const valorParaAutorizador = useMemo(() => {
    if (tipo === 'obras') {
      if (classificacao === 'transferencia_saldo') {
        const somaRetirada = obrasLinhas.filter((l) => l.lado === 'retirada').reduce((acc, l) => acc + num(l.valor), 0);
        const somaInclusao = obrasLinhas.filter((l) => l.lado === 'inclusao').reduce((acc, l) => acc + num(l.valor), 0);
        return Math.max(somaRetirada, somaInclusao);
      }
      return obrasLinhas.reduce((acc, l) => acc + num(l.valor), 0);
    }
    if (tipo === 'imobilizado') return valorImobilizado;
    return num(linhas[0]?.valor ?? '');
  }, [tipo, classificacao, obrasLinhas, linhas, valorImobilizado]);

  const precisaAutorizador = tipo === 'inclusao' || tipo === 'imobilizado' || tipo === 'obras';
  const { data: autorizadores } = useQuery({
    queryKey: ['autorizadores', tipo, centroCustoSelecionado?.codigo, valorParaAutorizador],
    queryFn: () => {
      const params = new URLSearchParams({ tipo, valor: String(valorParaAutorizador) });
      if (tipo !== 'obras' && centroCustoSelecionado) params.set('centro_custo_codigo', centroCustoSelecionado.codigo);
      return fetchJSON<{ items: { colaborador_id: string; nome: string }[] }>(`/api/formulario/autorizadores?${params.toString()}`);
    },
    enabled: precisaAutorizador && valorParaAutorizador > 0 && (tipo === 'obras' || Boolean(centroCustoSelecionado)),
  });

  function atualizarLinha(indice: number, campo: keyof LinhaLancamento, valor: string) {
    setLinhas((atual) => atual.map((linha, i) => (i === indice ? { ...linha, [campo]: valor } : linha)));
  }

  function atualizarLinhaObra(indice: number, campo: keyof LinhaObra, valor: string) {
    setObrasLinhas((atual) =>
      atual.map((linha, i) => {
        if (i !== indice) return linha;
        const nova = { ...linha, [campo]: valor } as LinhaObra;
        // trocou para retirada: "CRIAR" deixa de valer nesta linha
        if (nova.ordemInvestimento === 'CRIAR' && !permiteCriar(classificacao, nova.lado)) nova.ordemInvestimento = '';
        return nova;
      }),
    );
  }

  function trocarClassificacao(c: string) {
    setClassificacao(c);
    setObrasLinhas((atual) => atual.map((l) => (l.ordemInvestimento === 'CRIAR' && !permiteCriar(c, l.lado) ? { ...l, ordemInvestimento: '' } : l)));
  }

  async function handleSubmit() {
    setEnviando(true);
    try {
      let payload: Record<string, unknown>;
      const camposComuns = {
        ...(tipo !== 'imobilizado' && fornecedor.trim() ? { fornecedor: fornecedor.trim() } : {}),
        descricao: descricao.trim(),
        ...(tipo !== 'imobilizado' && fornecedorCnpj ? { fornecedor_cnpj: fornecedorCnpj } : {}),
        ...(tipo !== 'imobilizado' && fornecedorNaoCadastrado ? { fornecedor_nao_cadastrado: true } : {}),
        anexos: anexos.map((a) => ({ nome_arquivo: a.nome, content_type: a.contentType, conteudo_base64: a.base64 })),
        ...(emCopia.length ? { visualizadores: emCopia.map((p) => p.id) } : {}),
        ...(gruposCopia.length ? { grupos: gruposCopia } : {}),
      };

      if (tipo === 'obras') {
        payload = {
          tipo_solicitacao: 'obras',
          classificacao,
          ...(classificacao === 'retirada_saldo' || classificacao === 'transferencia_saldo'
            ? { ano_orcamento: Number(anoObras || opcoes?.ano_corrente || 0) || undefined }
            : {}),
          autorizador_id: autorizadorId,
          ...camposComuns,
          obras_linhas: obrasLinhas.map((l) => ({
            local_obra_id: obraLocalId,
            subgrupo_despesa_id: l.subgrupoDespesaId,
            ordem_investimento: l.ordemInvestimento,
            valor: num(l.valor),
            ...(classificacao === 'transferencia_saldo' ? { lado: l.lado } : {}),
          })),
        };
      } else {
        const divisaoId = centroCustoSelecionado?.divisao_id ?? '';
        payload = {
          tipo_solicitacao: tipo,
          ...(tipo === 'inclusao' || tipo === 'imobilizado' ? { autorizador_id: autorizadorId } : {}),
          ...(tipo === 'imobilizado'
            ? { quantidade_material: parseInt(quantidadeMaterial, 10) || 0, tipo_compra: tipoCompra, valor_unitario: num(valorUnitario) }
            : {}),
          ...camposComuns,
          lancamentos: linhas.map((l) => {
            const destinoTransf = tipo === 'transferencia' && l.lado === 'destino';
            const valor =
              tipo === 'imobilizado' ? valorImobilizado : destinoTransf && destinoUnico ? somaOrigens : num(l.valor);
            const conta = tipo === 'transferencia' ? (l.lado === 'origem' ? contaOrigem : contaDestino) : l.contaId;
            return {
              lado: l.lado,
              // Destino de Transferência pode ser de outro CC (divisão do próprio CC).
              divisao_id: destinoTransf ? ccDestino?.divisao_id ?? divisaoId : divisaoId,
              centro_custo_id: destinoTransf ? ccDestino?.id ?? centroCustoId : centroCustoId,
              valor,
              mes: l.mes ? `${l.mes}-01` : '',
              ...(tipo === 'imobilizado' ? { classe_imobilizado_id: l.classeImobilizadoId } : { conta_id: conta }),
            };
          }),
        };
      }

      const resp = await fetchJSON<{ id: string; aprovador_snapshot: { nome: string; motivo: string } }>('/api/solicitacoes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      toast.success(`Solicitação aberta com sucesso. Aprovador: ${resp.aprovador_snapshot.nome} (${resp.aprovador_snapshot.motivo})`);
      resetarFormulario();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao abrir solicitação');
    } finally {
      setEnviando(false);
    }
  }

  // ---------------------------------------------------------------- o que falta (o botão nunca fica cinza sem dizer)
  const faltando: string[] = [];
  const valoresDigitados: number[] = [];
  if (tipo === 'obras') {
    if (!autorizadorId) faltando.push('Autorizador');
    if (!obraLocalId) faltando.push('Local de obra');
    if (obrasLinhas.some((l) => !l.subgrupoDespesaId || !l.ordemInvestimento)) faltando.push('Subgrupo e ordem de investimento de todas as linhas');
    if (obrasLinhas.some((l) => !(num(l.valor) > 0))) faltando.push('Valor maior que zero em todas as linhas');
    if (classificacao === 'transferencia_saldo') {
      const r = obrasLinhas.filter((l) => l.lado === 'retirada').reduce((a, l) => a + num(l.valor), 0);
      const inc = obrasLinhas.filter((l) => l.lado === 'inclusao').reduce((a, l) => a + num(l.valor), 0);
      if (Math.abs(r - inc) > 0.01) faltando.push(`Retirada (${moeda.format(r)}) e inclusão (${moeda.format(inc)}) com o mesmo total`);
    }
    obrasLinhas.forEach((l) => valoresDigitados.push(num(l.valor)));
  } else if (tipo === 'transferencia') {
    if (!centroCustoId) faltando.push('Centro de custo de origem');
    if (!contaOrigem) faltando.push('Conta de origem');
    if (!contaDestino) faltando.push('Conta de destino');
    if (indicesOrigem.some((i) => !linhas[i].mes)) faltando.push('Mês de todas as linhas de origem');
    if (indicesDestino.some((i) => !linhas[i].mes)) faltando.push('Mês de todas as linhas de destino');
    if (indicesOrigem.some((i) => !(num(linhas[i].valor) > 0))) faltando.push('Valor maior que zero em todas as origens');
    if (!destinoUnico && indicesDestino.some((i) => !(num(linhas[i].valor) > 0))) faltando.push('Valor maior que zero em todos os destinos');
    if (!destinoUnico && Math.abs(somaDestinos - somaOrigens) > 0.01)
      faltando.push(`Destino (${moeda.format(somaDestinos)}) igual à origem (${moeda.format(somaOrigens)})`);
    if (mesesRepetidos(indicesOrigem) || mesesRepetidos(indicesDestino)) faltando.push('Um mês só uma vez em cada lado');
    if (Object.keys(errosSaldo).length) faltando.push('Retirada dentro do saldo da origem (veja a tabela)');
    indicesOrigem.forEach((i) => valoresDigitados.push(num(linhas[i].valor)));
    indicesDestino.forEach((i) => valoresDigitados.push(num(linhas[i].valor)));
  } else {
    if (!centroCustoId) faltando.push('Centro de custo');
    if (tipo === 'imobilizado') {
      if (!tipoCompra) faltando.push('Tipo de compra (FL ou Externa)');
      if (linhas.some((l) => !l.classeImobilizadoId)) faltando.push('Classe de imobilizado');
      if (linhas.some((l) => !l.mes)) faltando.push('Mês de competência');
      if (!(parseInt(quantidadeMaterial, 10) > 0) || /[.,]/.test(quantidadeMaterial)) faltando.push('Quantidade do material (número inteiro)');
      if (!(num(valorUnitario) > 0)) faltando.push('Valor unitário maior que zero');
      valoresDigitados.push(num(valorUnitario), valorImobilizado);
    } else {
      if (linhas.some((l) => !l.contaId)) faltando.push('Conta');
      if (linhas.some((l) => !l.mes)) faltando.push('Mês de competência');
      if (linhas.some((l) => !(num(l.valor) > 0))) faltando.push('Valor maior que zero');
      linhas.forEach((l) => valoresDigitados.push(num(l.valor)));
    }
    if ((tipo === 'inclusao' || tipo === 'imobilizado') && !autorizadorId) faltando.push('Autorizador');
  }
  if (valoresDigitados.some((v) => v > MAX_VALOR)) faltando.push(`Valor até ${moeda.format(MAX_VALOR)}`);
  if (!descricao.trim()) faltando.push(tipo === 'obras' ? 'Detalhe' : 'Descrição detalhada');
  if (descricao.trim().length > MAX_DESCRICAO) faltando.push(`Descrição com até ${MAX_DESCRICAO} caracteres`);
  if (anexos.length === 0) faltando.push('Anexo');
  const podeEnviar = faltando.length === 0;

  const rotuloFornecedor = tipo === 'inclusao' || tipo === 'transferencia' ? 'Nome Fornecedor da despesa' : 'Fornecedor da despesa';
  const rotuloDescricao = tipo === 'obras' ? 'Detalhe' : tipo === 'imobilizado' ? 'Descrição do imobilizado' : 'Descrição detalhada';
  const divisoes = opcoes?.divisoes ?? [];
  const linhaAtiva = linhas[ativaIdx];

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 p-6">
      <h1 className="text-lg font-semibold text-foreground">Nova solicitação</h1>

      {opcoesError && <p className="text-sm text-destructive">{(opcoesError as Error).message}</p>}

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

      {usaAnoUnico && anosPermitidos.length > 1 && (
        <div className="grid gap-1.5">
          <Label>Ano do orçamento</Label>
          <Select value={anoOrcamento} onValueChange={escolherAno}>
            <SelectTrigger className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {anosPermitidos.map((a) => (
                <SelectItem key={a} value={a}>
                  {a}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-fg-helper">O fechamento de janeiro ainda permite o ano anterior. A solicitação inteira é de um único ano.</p>
        </div>
      )}
      {usaAnoUnico && anosPermitidos.length === 1 && (
        <p className="text-sm text-fg-secondary">
          Ano do orçamento: <span className="font-medium text-foreground">{anosPermitidos[0]}</span>
        </p>
      )}

      {tipo !== 'obras' && tipo !== 'transferencia' && (
        <div className="grid min-w-0 gap-1.5">
          <DivisaoCentroCusto value={centroCustoId} onChange={setCentroCustoId} centros={opcoesOrigemTodas} divisoes={divisoes} />
          {opcoes && opcoesOrigemTodas.length === 0 && (
            <p className="text-xs text-destructive">Seu usuário não tem nenhum centro de custo autorizado ainda — fale com o administrador.</p>
          )}
        </div>
      )}

      {tipo === 'transferencia' && (
        <>
          <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
            <h2 className="text-sm font-semibold text-foreground">Origem</h2>
            <DivisaoCentroCusto
              value={centroCustoId}
              onChange={setCentroCustoId}
              centros={opcoesOrigemTodas}
              divisoes={divisoes}
              rotuloCC="Centro de custo de origem"
            />
            {opcoes && opcoesOrigemTodas.length === 0 && (
              <p className="text-xs text-destructive">Seu usuário não tem nenhum centro de custo autorizado ainda — fale com o administrador.</p>
            )}
            <div className="grid min-w-0 gap-1.5">
              <Label>Conta de origem</Label>
              <BuscaCombobox value={contaOrigem} onChange={setContaOrigem} opcoes={contasDisponiveis} ariaLabel="Conta de origem" />
            </div>
            <p className="text-xs text-fg-helper">Uma conta só; uma linha por mês de onde o valor sai (de 1 a 12 meses).</p>
            {indicesOrigem.map((i) => {
              const linha = linhas[i];
              const erro = linha.mes ? errosSaldo[linha.mes] : '';
              return (
                <div
                  key={i}
                  className={`grid grid-cols-1 gap-3 rounded-md p-2 sm:grid-cols-[10rem_11rem_auto] sm:items-end ${i === ativaIdx ? 'bg-muted/50 ring-1 ring-border' : ''}`}
                  onFocusCapture={() => setOrigemAtiva(i)}
                  onClick={() => setOrigemAtiva(i)}
                >
                  <div className="grid gap-1.5">
                    <Label>Mês</Label>
                    <MesAno value={linha.mes} ano={anoParaMes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                  </div>
                  <div className="grid gap-1.5">
                    <Label>Valor (R$)</Label>
                    <ValorInput value={linha.valor} onChange={(v) => atualizarLinha(i, 'valor', v)} />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={indicesOrigem.length <= 1}
                    onClick={(e) => {
                      e.stopPropagation();
                      setLinhas((atual) => atual.filter((_, j) => j !== i));
                      setOrigemAtiva(0);
                    }}
                  >
                    Remover
                  </Button>
                  {erro && (
                    <p className="text-xs font-medium text-danger-text sm:col-span-3" role="alert">
                      {erro}
                    </p>
                  )}
                </div>
              );
            })}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="self-start"
              disabled={indicesOrigem.length >= MAX_MESES_LADO}
              onClick={() => adicionarLinha('origem')}
            >
              + mês de origem
            </Button>
            {centroCustoId && contaOrigem && (
              <TabelaSaldoMeses
                saldo={saldoOrigem.data}
                carregando={saldoOrigem.isFetching && !saldoOrigem.data}
                retiradas={retiradas}
                mesAtivo={linhaAtiva?.mes ?? ''}
                valorAtivo={num(linhaAtiva?.valor ?? '')}
                mesesOcupados={new Set(indicesOrigem.filter((i) => i !== ativaIdx).map((i) => linhas[i].mes).filter(Boolean))}
                onEscolherMes={(m) => atualizarLinha(ativaIdx, 'mes', m)}
              />
            )}
          </section>

          <section className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
            <h2 className="text-sm font-semibold text-foreground">Destino</h2>
            <DivisaoCentroCusto
              value={ccDestinoId}
              onChange={setCcDestinoId}
              centros={opcoes?.centros_custo_destino ?? []}
              divisoes={divisoes}
              rotuloCC="Centro de custo de destino"
              limparRotulo="Mesmo centro de custo da origem"
            />
            <div className="grid min-w-0 gap-1.5">
              <Label>Conta de destino</Label>
              <BuscaCombobox value={contaDestino} onChange={setContaDestino} opcoes={contasDisponiveis} ariaLabel="Conta de destino" />
            </div>
            {indicesDestino.map((i) => (
              <div key={i} className="grid grid-cols-1 gap-3 p-2 sm:grid-cols-[10rem_11rem_auto] sm:items-end">
                <div className="grid gap-1.5">
                  <Label>Mês</Label>
                  <MesAno value={linhas[i].mes} ano={anoParaMes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                </div>
                <div className="grid gap-1.5">
                  <Label>{destinoUnico ? 'Valor (soma da origem)' : 'Valor (R$)'}</Label>
                  {destinoUnico ? (
                    <div
                      className="flex h-10 items-center rounded-md border border-input bg-[var(--bg-input-disabled)] px-3 text-sm tabular-nums text-foreground"
                      aria-live="polite"
                    >
                      {moeda.format(somaOrigens)}
                    </div>
                  ) : (
                    <ValorInput value={linhas[i].valor} onChange={(v) => atualizarLinha(i, 'valor', v)} />
                  )}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={destinoUnico}
                  onClick={() => setLinhas((atual) => atual.filter((_, j) => j !== i))}
                >
                  Remover
                </Button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="self-start"
              disabled={indicesDestino.length >= MAX_MESES_LADO}
              onClick={() => adicionarLinha('destino')}
            >
              + mês de destino
            </Button>
            {!destinoUnico && (
              <p className={`text-xs ${Math.abs(somaDestinos - somaOrigens) > 0.01 ? 'text-danger-text' : 'text-fg-helper'}`}>
                Destino: {moeda.format(somaDestinos)} · Origem: {moeda.format(somaOrigens)}. Os dois lados precisam somar o mesmo valor.
              </p>
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
                <div className="grid gap-1.5">
                  <Label>Tipo de compra</Label>
                  <Select value={tipoCompra} onValueChange={setTipoCompra}>
                    <SelectTrigger aria-label="Tipo de compra">
                      <SelectValue placeholder="FL ou Externa" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="FL">FL</SelectItem>
                      <SelectItem value="Externa">Externa</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1.5">
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
                  <Label>Mês de competência</Label>
                  <MesAno value={linha.mes} ano={anoParaMes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                </div>
                <div className="grid gap-1.5">
                  <Label>Quantidade do material</Label>
                  <Input
                    inputMode="numeric"
                    value={quantidadeMaterial}
                    aria-invalid={/[.,]/.test(quantidadeMaterial)}
                    onChange={(e) => setQuantidadeMaterial(e.target.value.replace(/[^\d.,]/g, ''))}
                  />
                  {/[.,]/.test(quantidadeMaterial) && (
                    <p className="text-xs text-destructive" role="alert">
                      A quantidade deve ser um número inteiro (sem vírgula nem ponto).
                    </p>
                  )}
                </div>
                <div className="grid gap-1.5">
                  <Label>Valor unitário (R$)</Label>
                  <ValorInput value={valorUnitario} onChange={setValorUnitario} />
                </div>
                <div className="grid gap-1.5">
                  <Label>Valor total</Label>
                  <div
                    className="flex h-10 items-center rounded-md border border-input bg-[var(--bg-input-disabled)] px-3 text-sm tabular-nums text-foreground"
                    aria-live="polite"
                    title="Quantidade × valor unitário"
                  >
                    {moeda.format(valorImobilizado)}
                  </div>
                </div>
              </>
            ) : (
              <>
                <div className="col-span-2 grid min-w-0 gap-1.5">
                  <Label>Conta</Label>
                  <BuscaCombobox value={linha.contaId} onChange={(v) => atualizarLinha(i, 'contaId', v)} opcoes={contasDisponiveis} ariaLabel="Conta" />
                </div>
                <div className="grid gap-1.5">
                  <Label>Mês de competência</Label>
                  <MesAno value={linha.mes} ano={anoParaMes} onChange={(v) => atualizarLinha(i, 'mes', v)} />
                </div>
                <div className="grid gap-1.5">
                  <Label>Valor (R$)</Label>
                  <ValorInput value={linha.valor} onChange={(v) => atualizarLinha(i, 'valor', v)} />
                </div>
                <div className="col-span-2">
                  <OrcamentoInformativo ccId={centroCustoId} contaId={linha.contaId} mes={linha.mes} />
                </div>
              </>
            )}
          </div>
        ))}

      {tipo === 'obras' && (
        <>
          <div className="grid gap-1.5">
            <Label>Classificação</Label>
            <Select value={classificacao} onValueChange={trocarClassificacao}>
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

          {(classificacao === 'retirada_saldo' || classificacao === 'transferencia_saldo') && (
            <div className="grid max-w-[10rem] gap-1.5">
              <Label>Ano do orçamento</Label>
              <Select value={anoObras || String(opcoes?.ano_corrente ?? '')} onValueChange={setAnoObras}>
                <SelectTrigger>
                  <SelectValue placeholder="Ano" />
                </SelectTrigger>
                <SelectContent>
                  {(opcoes?.anos_obras ?? []).map((a) => (
                    <SelectItem key={a} value={String(a)}>
                      {a}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="grid gap-1.5">
            <Label>Local de obra</Label>
            <Select
              value={obraLocalId}
              onValueChange={(v) => {
                setObraLocalId(v);
                // A ordem de investimento depende do local — as já escolhidas podem não existir no local novo.
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
                  <Button type="button" variant="ghost" size="sm" onClick={() => setObrasLinhas((atual) => atual.filter((_, j) => j !== i))}>
                    Remover linha
                  </Button>
                )}
              </div>
              {classificacao === 'transferencia_saldo' && (
                <div className="col-span-2 grid gap-1.5 sm:max-w-[14rem]">
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
                  criar={permiteCriar(classificacao, linha.lado)}
                />
              </div>
              <div className="grid gap-1.5">
                <Label>Valor (R$)</Label>
                <ValorInput value={linha.valor} onChange={(v) => atualizarLinhaObra(i, 'valor', v)} />
              </div>
            </div>
          ))}

          <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => setObrasLinhas((a) => [...a, novaLinhaObra()])}>
            + linha
          </Button>
        </>
      )}

      {tipo !== 'imobilizado' && (
        <CampoFornecedor
          rotulo={rotuloFornecedor}
          valor={fornecedor}
          onChange={setFornecedor}
          cnpj={fornecedorCnpj}
          onCnpjChange={setFornecedorCnpj}
          naoCadastrado={fornecedorNaoCadastrado}
          onNaoCadastradoChange={setFornecedorNaoCadastrado}
        />
      )}

      <div className="grid gap-1.5">
        <Label>
          {rotuloDescricao} <span className="text-destructive" aria-hidden>*</span>
        </Label>
        <Textarea
          value={descricao}
          onChange={(e) => setDescricao(e.target.value.slice(0, MAX_DESCRICAO))}
          placeholder={tipo === 'obras' ? 'O que será feito e onde' : 'Item e onde será aplicado'}
          rows={3}
          aria-required
        />
        <p className="text-right text-xs text-fg-helper tabular-nums">
          {descricao.length}/{MAX_DESCRICAO}
        </p>
      </div>

      <CampoAnexos anexos={anexos} onChange={setAnexos} />

      <CampoVisualizadores pessoas={emCopia} onPessoas={setEmCopia} grupos={gruposCopia} onGrupos={setGruposCopia} />

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
            <p className="text-xs text-destructive">Nenhum autorizador cadastrado para esse CC/faixa de valor.</p>
          )}
        </div>
      )}

      <Button onClick={handleSubmit} disabled={!podeEnviar || enviando}>
        {enviando ? 'Enviando...' : 'Abrir solicitação'}
      </Button>
      {!podeEnviar && (
        <p className="-mt-3 text-xs text-fg-helper" role="status">
          Para abrir a solicitação falta: {faltando.join(' · ')}.
        </p>
      )}
    </div>
  );
};

export default AbrirSolicitacao;
