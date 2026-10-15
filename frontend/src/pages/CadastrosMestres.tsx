import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
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
  DialogTrigger,
} from '@/components/ui/dialog';
import { Upload, Pencil, History, ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react';

// Tela genérica para os cadastros mestres do Epic 2/3 (handlers/cadastros.go)
// — um único cadastroRegistry no backend serve 15 {tipo} com os mesmos 5
// handlers (listar/importar/atualizar/histórico/restaurar); esta tela espelha
// essa generalidade no frontend com UM formulário dirigido por metadado
// (TIPOS_CADASTRO abaixo) em vez de 15 páginas quase-idênticas.
//
// Limitação herdada do backend, não desta tela: ImportarCadastroHandler só
// aceita CSV quando a tabela do tipo está INTEIRAMENTE vazia (409 caso
// contrário) — não existe "criar 1 registro novo" depois da carga inicial,
// só editar (PUT) o que já existe. Então esta tela nunca tem botão "Novo
// registro": só "Carregar CSV inicial" (antes de ter dados) e "Editar" (depois).

type CampoKind = 'texto' | 'texto_opcional' | 'numero' | 'numero_opcional' | 'booleano' | 'data' | 'uuid' | 'uuid_opcional';

interface CampoDef {
  nome: string;
  label: string;
  kind: CampoKind;
  ajuda?: string;
}

interface TipoCadastroDef {
  tipo: string;
  label: string;
  csvCabecalho: string;
  campos: CampoDef[];
}

// Espelha 1:1 o `cadastroRegistry` de backend/handlers/cadastros.go — mesma
// ordem de colunas, mesmos nomes. Campos uuid/uuid_opcional que são FK
// resolvida por e-mail/código SÓ no CSV (ex. colaborador_email) viram o
// próprio UUID já resolvido no corpo de PUT (ex. colaborador_id) — por isso o
// "nome" aqui é sempre o nome de COLUNA, igual ao backend espera no PUT.
const TIPOS_CADASTRO: TipoCadastroDef[] = [
  {
    tipo: 'divisoes',
    label: 'Divisões',
    csvCabecalho: 'codigo;nome',
    campos: [
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
    ],
  },
  {
    tipo: 'centros-custo',
    label: 'Centros de Custo',
    csvCabecalho: 'codigo;nome;divisao_codigo;filial',
    campos: [
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
      { nome: 'divisao_id', label: 'Divisão (id)', kind: 'uuid', ajuda: 'UUID de uma divisão — veja em "Divisões"' },
      { nome: 'filial', label: 'Filial', kind: 'texto_opcional' },
    ],
  },
  {
    tipo: 'contas',
    label: 'Contas',
    csvCabecalho: 'plano;codigo;nome',
    campos: [
      { nome: 'plano', label: 'Plano (BIFC/SFC)', kind: 'texto' },
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
    ],
  },
  {
    tipo: 'classes-imobilizado',
    label: 'Classes de Imobilizado',
    csvCabecalho: 'codigo;nome',
    campos: [
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
    ],
  },
  {
    tipo: 'alcadas',
    label: 'Alçadas',
    csvCabecalho: 'filial;centro_custo_codigo;valor_minimo;valor_maximo;papel_aprovador;ativo',
    campos: [
      { nome: 'filial', label: 'Filial', kind: 'texto' },
      { nome: 'centro_custo_codigo', label: 'Centro de custo (código)', kind: 'texto' },
      { nome: 'valor_minimo', label: 'Valor mínimo', kind: 'numero' },
      { nome: 'valor_maximo', label: 'Valor máximo', kind: 'numero_opcional', ajuda: 'vazio = sem teto' },
      { nome: 'papel_aprovador', label: 'Papel aprovador', kind: 'texto' },
      { nome: 'ativo', label: 'Ativo', kind: 'booleano' },
    ],
  },
  {
    tipo: 'papel-pessoa',
    label: 'Papel × Pessoa',
    csvCabecalho: 'papel;pessoa_nome',
    campos: [
      { nome: 'papel', label: 'Papel', kind: 'texto' },
      { nome: 'pessoa_nome', label: 'Pessoa (nome)', kind: 'texto' },
    ],
  },
  {
    tipo: 'feriados',
    label: 'Feriados (SLA)',
    csvCabecalho: 'data;descricao',
    campos: [
      { nome: 'data', label: 'Data', kind: 'data' },
      { nome: 'descricao', label: 'Descrição', kind: 'texto' },
    ],
  },
  {
    tipo: 'cc-excecao',
    label: 'Exceção de Centro de Custo',
    csvCabecalho: 'colaborador_email;centro_custo_codigo',
    campos: [
      { nome: 'colaborador_id', label: 'Colaborador (id)', kind: 'uuid' },
      { nome: 'centro_custo_id', label: 'Centro de custo (id)', kind: 'uuid' },
    ],
  },
  {
    tipo: 'regras-aprovacao',
    label: 'Regras de Aprovação',
    csvCabecalho: 'precedencia;filial;centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;papel_aprovador;ativo',
    campos: [
      { nome: 'precedencia', label: 'Precedência', kind: 'numero' },
      { nome: 'filial', label: 'Filial', kind: 'texto' },
      { nome: 'centro_custo_codigo', label: 'Centro de custo (código)', kind: 'texto' },
      { nome: 'valor_minimo', label: 'Valor mínimo', kind: 'numero' },
      { nome: 'valor_maximo', label: 'Valor máximo', kind: 'numero_opcional', ajuda: 'vazio = sem teto' },
      { nome: 'colaborador_id', label: 'Colaborador (id)', kind: 'uuid_opcional', ajuda: 'informe isto OU papel aprovador, não os dois' },
      { nome: 'papel_aprovador', label: 'Papel aprovador', kind: 'texto_opcional' },
      { nome: 'ativo', label: 'Ativo', kind: 'booleano' },
    ],
  },
  {
    tipo: 'gerentes-aprovacao',
    label: 'Gerentes de Aprovação',
    csvCabecalho: 'centro_custo_codigo;divisao_codigo;colaborador_email;papel_aprovador;teto;ativo',
    campos: [
      { nome: 'centro_custo_id', label: 'Centro de custo (id)', kind: 'uuid_opcional', ajuda: 'informe isto OU divisão, não os dois (ambos vazios = global)' },
      { nome: 'divisao_id', label: 'Divisão (id)', kind: 'uuid_opcional' },
      { nome: 'colaborador_id', label: 'Colaborador (id)', kind: 'uuid_opcional', ajuda: 'informe isto OU papel aprovador, não os dois' },
      { nome: 'papel_aprovador', label: 'Papel aprovador', kind: 'texto_opcional' },
      { nome: 'teto', label: 'Teto', kind: 'numero' },
      { nome: 'ativo', label: 'Ativo', kind: 'booleano' },
    ],
  },
  {
    tipo: 'autorizadores-formulario',
    label: 'Autorizadores do Formulário',
    csvCabecalho: 'centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;ativo',
    campos: [
      { nome: 'centro_custo_codigo', label: 'Centro de custo (código)', kind: 'texto' },
      { nome: 'valor_minimo', label: 'Valor mínimo', kind: 'numero' },
      { nome: 'valor_maximo', label: 'Valor máximo', kind: 'numero_opcional', ajuda: 'vazio = sem teto' },
      { nome: 'colaborador_id', label: 'Colaborador (id)', kind: 'uuid' },
      { nome: 'ativo', label: 'Ativo', kind: 'booleano' },
    ],
  },
  {
    tipo: 'locais-obra',
    label: 'Locais de Obra',
    csvCabecalho: 'codigo;nome',
    campos: [
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
    ],
  },
  {
    tipo: 'subgrupos-despesa',
    label: 'Subgrupos de Despesa',
    csvCabecalho: 'codigo;nome',
    campos: [
      { nome: 'codigo', label: 'Código', kind: 'texto' },
      { nome: 'nome', label: 'Nome', kind: 'texto' },
    ],
  },
  {
    tipo: 'obra-ordens',
    label: 'Ordens de Obra',
    csvCabecalho: 'numero_ordem;local_obra_codigo;subgrupo_despesa_codigo;tipo_despesa;tipo_faturamento',
    campos: [
      { nome: 'numero_ordem', label: 'Número da ordem', kind: 'texto' },
      { nome: 'local_obra_id', label: 'Local de obra (id)', kind: 'uuid' },
      { nome: 'subgrupo_despesa_id', label: 'Subgrupo de despesa (id)', kind: 'uuid' },
      { nome: 'tipo_despesa', label: 'Tipo de despesa', kind: 'texto_opcional' },
      { nome: 'tipo_faturamento', label: 'Tipo de faturamento', kind: 'texto_opcional' },
    ],
  },
  {
    tipo: 'aprovadores-obra',
    label: 'Aprovadores de Obra',
    csvCabecalho: 'colaborador_email;teto;ativo',
    campos: [
      { nome: 'colaborador_id', label: 'Colaborador (id)', kind: 'uuid' },
      { nome: 'teto', label: 'Teto', kind: 'numero' },
      { nome: 'ativo', label: 'Ativo', kind: 'booleano' },
    ],
  },
];

type Registro = Record<string, unknown> & { id: string; created_at: string; updated_at: string };

interface ListaResponse {
  items: Registro[];
  pagina: number;
  tamanho: number;
}

interface HistoricoItem {
  versao: number;
  acao: string;
  dados: Record<string, unknown>;
  ator_id: string | null;
  created_at: string;
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

function valorExibicao(campo: CampoDef, valor: unknown): string {
  if (valor === null || valor === undefined) return '—';
  if (campo.kind === 'booleano') return valor ? 'Sim' : 'Não';
  return String(valor);
}

// valorParaFormulario/valorParaEnvio — conversão linha<->formulário. Campo
// opcional vazio vira `null` no envio (igual extractStringOpcional/
// extractFloatOpcional do backend esperam: a CHAVE precisa estar presente,
// null é "sem valor", omitir é erro).
function valorParaFormulario(campo: CampoDef, valor: unknown): string | boolean {
  if (campo.kind === 'booleano') return Boolean(valor);
  if (valor === null || valor === undefined) return '';
  return String(valor);
}

function valorParaEnvio(campo: CampoDef, valor: string | boolean): unknown {
  if (campo.kind === 'booleano') return Boolean(valor);
  const texto = String(valor).trim();
  switch (campo.kind) {
    case 'numero':
      return Number(texto);
    case 'numero_opcional':
      return texto === '' ? null : Number(texto);
    case 'texto_opcional':
    case 'uuid_opcional':
      return texto === '' ? null : texto;
    default:
      return texto;
  }
}

const CadastrosMestres = () => {
  const queryClient = useQueryClient();
  const [tipoAtual, setTipoAtual] = useState(TIPOS_CADASTRO[0].tipo);
  const [pagina, setPagina] = useState(1);
  const [cargaOpen, setCargaOpen] = useState(false);
  const [arquivoCarga, setArquivoCarga] = useState<File | null>(null);
  const [editAlvo, setEditAlvo] = useState<Registro | null>(null);
  const [editValores, setEditValores] = useState<Record<string, string | boolean>>({});
  const [historicoAlvo, setHistoricoAlvo] = useState<Registro | null>(null);

  const def = TIPOS_CADASTRO.find((t) => t.tipo === tipoAtual)!;

  function trocarTipo(novoTipo: string) {
    setTipoAtual(novoTipo);
    setPagina(1);
  }

  const { data, isLoading, error } = useQuery({
    queryKey: ['cadastros', tipoAtual, pagina],
    queryFn: () => fetchJSON<ListaResponse>(`/api/admin/cadastros/${tipoAtual}?pagina=${pagina}&tamanho=20`),
  });

  const cargaMutation = useMutation({
    mutationFn: async (arquivo: File) => {
      const texto = await arquivo.text();
      return fetchJSON<{ importados: number }>(`/api/admin/cadastros/${tipoAtual}`, {
        method: 'POST',
        headers: { 'Content-Type': 'text/csv; charset=utf-8' },
        body: texto,
      });
    },
    onSuccess: (resp) => {
      toast.success(`${resp.importados} registro(s) carregado(s)`);
      setCargaOpen(false);
      setArquivoCarga(null);
      queryClient.invalidateQueries({ queryKey: ['cadastros', tipoAtual] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) =>
      fetchJSON(`/api/admin/cadastros/${tipoAtual}/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      toast.success('Registro atualizado');
      setEditAlvo(null);
      queryClient.invalidateQueries({ queryKey: ['cadastros', tipoAtual] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const { data: historico, isLoading: historicoCarregando } = useQuery({
    queryKey: ['cadastro-historico', tipoAtual, historicoAlvo?.id],
    queryFn: () =>
      fetchJSON<{ items: HistoricoItem[] }>(`/api/admin/cadastros/${tipoAtual}/${historicoAlvo!.id}/historico?tamanho=50`),
    enabled: historicoAlvo !== null,
  });

  const restaurarMutation = useMutation({
    mutationFn: (versao: number) =>
      fetchJSON(`/api/admin/cadastros/${tipoAtual}/${historicoAlvo!.id}/restaurar`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ versao }),
      }),
    onSuccess: () => {
      toast.success('Versão restaurada');
      setHistoricoAlvo(null);
      queryClient.invalidateQueries({ queryKey: ['cadastros', tipoAtual] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  function abrirEdicao(registro: Registro) {
    const valores: Record<string, string | boolean> = {};
    for (const campo of def.campos) {
      valores[campo.nome] = valorParaFormulario(campo, registro[campo.nome]);
    }
    setEditAlvo(registro);
    setEditValores(valores);
  }

  function salvarEdicao() {
    if (!editAlvo) return;
    const body: Record<string, unknown> = {};
    for (const campo of def.campos) {
      body[campo.nome] = valorParaEnvio(campo, editValores[campo.nome]);
    }
    updateMutation.mutate({ id: editAlvo.id, body });
  }

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold text-foreground">Cadastros Mestres</h1>
        <Dialog open={cargaOpen} onOpenChange={(o) => { setCargaOpen(o); if (!o) setArquivoCarga(null); }}>
          <DialogTrigger asChild>
            <Button size="sm" variant="outline">
              <Upload className="mr-2 h-4 w-4" />
              Carregar CSV inicial
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>Carregar CSV inicial — {def.label}</DialogTitle>
            </DialogHeader>
            <p className="text-xs text-muted-foreground">
              Arquivo CSV (separador &quot;;&quot;, UTF-8). Cabeçalho exigido (nesta ordem exata):{' '}
              <code>{def.csvCabecalho}</code>. Só funciona enquanto este cadastro estiver vazio — se já houver
              dados, a carga é recusada (edite registro a registro com &quot;Editar&quot;).
            </p>
            <Input type="file" accept=".csv,text/csv" onChange={(e) => setArquivoCarga(e.target.files?.[0] ?? null)} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setCargaOpen(false)}>
                Cancelar
              </Button>
              <Button
                disabled={!arquivoCarga || cargaMutation.isPending}
                onClick={() => arquivoCarga && cargaMutation.mutate(arquivoCarga)}
              >
                {cargaMutation.isPending ? 'Enviando...' : 'Carregar'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="max-w-xs">
        <Select value={tipoAtual} onValueChange={trocarTipo}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TIPOS_CADASTRO.map((t) => (
              <SelectItem key={t.tipo} value={t.tipo}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}

      {data && (
        <>
          {data.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhum registro ainda — use &quot;Carregar CSV inicial&quot; para popular este cadastro.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  {def.campos.map((campo) => (
                    <TableHead key={campo.nome}>{campo.label}</TableHead>
                  ))}
                  <TableHead>Ações</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.map((registro) => (
                  <TableRow key={registro.id}>
                    {def.campos.map((campo) =>
                      campo.kind === 'booleano' ? (
                        <TableCell key={campo.nome}>
                          <Badge variant={registro[campo.nome] ? 'default' : 'secondary'}>
                            {valorExibicao(campo, registro[campo.nome])}
                          </Badge>
                        </TableCell>
                      ) : (
                        <TableCell key={campo.nome} className="text-muted-foreground">
                          {valorExibicao(campo, registro[campo.nome])}
                        </TableCell>
                      ),
                    )}
                    <TableCell>
                      <div className="flex gap-2">
                        <Button size="sm" variant="outline" onClick={() => abrirEdicao(registro)}>
                          <Pencil className="mr-1 h-3.5 w-3.5" />
                          Editar
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => setHistoricoAlvo(registro)}>
                          <History className="mr-1 h-3.5 w-3.5" />
                          Histórico
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" disabled={pagina <= 1} onClick={() => setPagina((p) => p - 1)}>
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <span className="text-xs text-muted-foreground">Página {pagina}</span>
            <Button
              size="sm"
              variant="outline"
              disabled={data.items.length < data.tamanho}
              onClick={() => setPagina((p) => p + 1)}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </>
      )}

      <Dialog open={editAlvo !== null} onOpenChange={(o) => !o && setEditAlvo(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Editar — {def.label}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-2 max-h-[60vh] overflow-y-auto">
            {def.campos.map((campo) => (
              <div key={campo.nome} className="grid gap-1.5">
                {campo.kind === 'booleano' ? (
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={Boolean(editValores[campo.nome])}
                      onCheckedChange={(checked) => setEditValores((v) => ({ ...v, [campo.nome]: checked }))}
                    />
                    <Label>{campo.label}</Label>
                  </div>
                ) : (
                  <>
                    <Label htmlFor={`edit-${campo.nome}`}>{campo.label}</Label>
                    <Input
                      id={`edit-${campo.nome}`}
                      type={campo.kind === 'numero' || campo.kind === 'numero_opcional' ? 'number' : campo.kind === 'data' ? 'date' : 'text'}
                      value={String(editValores[campo.nome] ?? '')}
                      onChange={(e) => setEditValores((v) => ({ ...v, [campo.nome]: e.target.value }))}
                    />
                    {campo.ajuda && <p className="text-[11px] text-muted-foreground">{campo.ajuda}</p>}
                  </>
                )}
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditAlvo(null)}>
              Cancelar
            </Button>
            <Button onClick={salvarEdicao} disabled={updateMutation.isPending}>
              {updateMutation.isPending ? 'Salvando...' : 'Salvar'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={historicoAlvo !== null} onOpenChange={(o) => !o && setHistoricoAlvo(null)}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Histórico — {def.label}</DialogTitle>
          </DialogHeader>
          <div className="max-h-[60vh] overflow-y-auto">
            {historicoCarregando && <p className="text-sm text-muted-foreground">Carregando...</p>}
            {historico && historico.items.length === 0 && (
              <p className="text-sm text-muted-foreground">Sem histórico ainda.</p>
            )}
            {historico && historico.items.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Versão</TableHead>
                    <TableHead>Ação</TableHead>
                    <TableHead>Quando</TableHead>
                    <TableHead>Ações</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {historico.items.map((item) => (
                    <TableRow key={item.versao}>
                      <TableCell>{item.versao}</TableCell>
                      <TableCell className="capitalize">{item.acao}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {new Date(item.created_at).toLocaleString('pt-BR')}
                      </TableCell>
                      <TableCell>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={restaurarMutation.isPending}
                          onClick={() => restaurarMutation.mutate(item.versao)}
                        >
                          <RotateCcw className="mr-1 h-3.5 w-3.5" />
                          Restaurar
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
          <DialogFooter>
            <Button onClick={() => setHistoricoAlvo(null)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default CadastrosMestres;
