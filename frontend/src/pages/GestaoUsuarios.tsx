import { useMemo, useState } from 'react';
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
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
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
import { UserPlus, Upload, Mail, Pencil, Search } from 'lucide-react';

// Tela de gestão de usuários — consome os 3 endpoints administrativos do
// backend (handlers/admin.go): GET /api/admin/usuarios (lista paginada,
// AD-14), POST /api/admin/usuarios (CreateUsuarioLocalHandler) e
// PATCH /api/admin/usuarios/{id} (AdminUpdateUsuarioHandler — perfil/ativo/
// cod_empresa/numcad). O Authorization é injetado automaticamente pelo
// interceptor global de fetch (AuthContext.tsx), não precisa setar aqui.
//
// cod_empresa/numcad (migration 019) identificam o usuário em
// SFC.SFC_VW_COLABORADORES (Oracle/FCCORP) — a view não tem e-mail, então a
// sincronização diária de centro de custo via ERP Bridge só alcança um
// usuário que tenha os dois preenchidos aqui.
interface Usuario {
  id: string;
  email: string;
  nome: string;
  perfil: 'solicitante' | 'administrador';
  ativo: boolean;
  created_at: string;
  cod_empresa: number | null;
  numcad: number | null;
  centro_custo_id: string | null;
  centro_custo_nome: string | null;
  // aprovador (migration 020): libera o usuário como autorizador em
  // qualquer CC/faixa/teto, sem precisar de cadastro em
  // autorizadores_formulario/aprovadores_obra (Cadastros Mestres) — ver
  // internal/aprovacao, resolveAprovadorGlobal.
  aprovador: boolean;
}

interface CentroCusto {
  id: string;
  codigo: string;
  nome: string;
}

// A API não devolve total/cursor (AD-14) e paginacaoDaQuery trava "tamanho"
// em 100 no servidor — então pra ter a lista inteira (hoje ~500 usuários
// carregados via planilha) é preciso paginar no cliente até vir uma página
// mais curta que o pedido, igual já se faz em qualquer outra tela que
// precisa do dado completo sem filtro no servidor.
async function fetchTudoPaginado<T>(urlBase: string): Promise<T[]> {
  const tamanho = 100;
  let pagina = 1;
  const todos: T[] = [];
  for (;;) {
    const resp = await fetchJSON<{ items: T[] }>(
      `${urlBase}${urlBase.includes('?') ? '&' : '?'}pagina=${pagina}&tamanho=${tamanho}`,
    );
    todos.push(...resp.items);
    if (resp.items.length < tamanho) break;
    pagina++;
  }
  return todos;
}

interface CargaResultado {
  criados: number;
  atualizados: number;
  ignorados: number;
  total: number;
  erros: string[];
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

// IdentidadeOracleCell — edição inline de cod_empresa/numcad para um usuário
// já existente (ex.: provisionado via SSO, que nunca passa pelo formulário
// de criação local onde esses campos são obrigatórios). Os dois sempre
// salvam juntos (o backend recusa um par parcial) — onBlur em vez de
// onChange para não disparar um PATCH a cada dígito.
function IdentidadeOracleCell({
  usuario,
  onSalvar,
}: {
  usuario: Usuario;
  onSalvar: (codEmpresa: number, numcad: number) => void;
}) {
  const [codEmpresa, setCodEmpresa] = useState(usuario.cod_empresa?.toString() ?? '');
  const [numcad, setNumcad] = useState(usuario.numcad?.toString() ?? '');

  function salvarSeMudou() {
    const codEmpresaNum = Number(codEmpresa);
    const numcadNum = Number(numcad);
    if (!codEmpresaNum || !numcadNum) return;
    if (codEmpresaNum === usuario.cod_empresa && numcadNum === usuario.numcad) return;
    onSalvar(codEmpresaNum, numcadNum);
  }

  return (
    <div className="flex gap-1.5">
      <Input
        type="number"
        max={2147483647}
        value={codEmpresa}
        onChange={(e) => setCodEmpresa(e.target.value)}
        onBlur={salvarSeMudou}
        placeholder="Empresa"
        className="h-8 w-20"
      />
      <Input
        type="number"
        max={2147483647}
        value={numcad}
        onChange={(e) => setNumcad(e.target.value)}
        onBlur={salvarSeMudou}
        placeholder="Matrícula"
        className="h-8 w-24"
      />
    </div>
  );
}

const GestaoUsuarios = () => {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState('');
  const [nome, setNome] = useState('');
  const [senha, setSenha] = useState('');
  const [perfil, setPerfil] = useState<'solicitante' | 'administrador'>('solicitante');
  const [codEmpresa, setCodEmpresa] = useState('');
  const [numcad, setNumcad] = useState('');
  const [cargaOpen, setCargaOpen] = useState(false);
  const [arquivoCarga, setArquivoCarga] = useState<File | null>(null);
  const [resultadoCarga, setResultadoCarga] = useState<CargaResultado | null>(null);
  const [busca, setBusca] = useState('');
  const [editAlvo, setEditAlvo] = useState<Usuario | null>(null);
  const [editNome, setEditNome] = useState('');
  const [editEmail, setEditEmail] = useState('');
  const [editCentroCustoId, setEditCentroCustoId] = useState('');

  const { data: usuarios, isLoading, error } = useQuery({
    queryKey: ['usuarios'],
    queryFn: () => fetchTudoPaginado<Usuario>('/api/admin/usuarios'),
  });

  const { data: centrosCusto } = useQuery({
    queryKey: ['centros-custo'],
    queryFn: () => fetchTudoPaginado<CentroCusto>('/api/admin/cadastros/centros-custo'),
  });

  const usuariosFiltrados = useMemo(() => {
    const todos = usuarios ?? [];
    const termo = busca.trim().toLowerCase();
    if (!termo) return todos;
    return todos.filter((u) =>
      [u.nome, u.email, u.centro_custo_nome ?? '', String(u.cod_empresa ?? ''), String(u.numcad ?? '')]
        .join(' ')
        .toLowerCase()
        .includes(termo),
    );
  }, [usuarios, busca]);

  const createMutation = useMutation({
    mutationFn: () =>
      fetchJSON('/api/admin/usuarios', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          email,
          nome,
          senha,
          perfil,
          cod_empresa: Number(codEmpresa),
          numcad: Number(numcad),
        }),
      }),
    onSuccess: () => {
      toast.success('Usuário criado com sucesso');
      setCreateOpen(false);
      setEmail('');
      setNome('');
      setSenha('');
      setPerfil('solicitante');
      setCodEmpresa('');
      setNumcad('');
      queryClient.invalidateQueries({ queryKey: ['usuarios'] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: string;
      body: Partial<Pick<Usuario, 'perfil' | 'ativo' | 'cod_empresa' | 'numcad' | 'aprovador'>> & {
        nome?: string;
        email?: string;
        centro_custo_id?: string;
      };
    }) =>
      fetchJSON(`/api/admin/usuarios/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['usuarios'] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  // Carga em lote via planilha (CSV ';', UTF-8) — consome
  // POST /api/admin/usuarios/carga (handlers/usuarios_carga.go). Upsert por
  // e-mail: cria quem não existe (senha descartável, só via "Esqueci minha
  // senha" dá pra entrar) e atualiza identidade/perfil/CC de quem já existe.
  const cargaMutation = useMutation({
    mutationFn: async (arquivo: File) => {
      const texto = await arquivo.text();
      return fetchJSON<CargaResultado>('/api/admin/usuarios/carga', {
        method: 'POST',
        headers: { 'Content-Type': 'text/csv; charset=utf-8' },
        body: texto,
      });
    },
    onSuccess: (resultado) => {
      setResultadoCarga(resultado);
      queryClient.invalidateQueries({ queryKey: ['usuarios'] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  // Liberar acesso — dispara o mesmo e-mail de "Esqueci minha senha", mas a
  // pedido do administrador (AdminLiberarAcessoHandler, admin.go). Principal
  // uso: ativar uma conta recém-criada pela carga acima, cuja senha é um
  // valor aleatório que ninguém conhece.
  const liberarAcessoMutation = useMutation({
    mutationFn: (id: string) => fetchJSON<{ message: string }>(`/api/admin/usuarios/${id}/liberar-acesso`, { method: 'POST' }),
    onSuccess: (resp) => toast.success(resp.message),
    onError: (err: Error) => toast.error(err.message),
  });

  function fecharDialogCarga(aberto: boolean) {
    setCargaOpen(aberto);
    if (!aberto) {
      setArquivoCarga(null);
      setResultadoCarga(null);
    }
  }

  function abrirEdicao(usuario: Usuario) {
    setEditAlvo(usuario);
    setEditNome(usuario.nome);
    setEditEmail(usuario.email);
    setEditCentroCustoId(usuario.centro_custo_id ?? '');
  }

  function salvarEdicao() {
    if (!editAlvo) return;
    updateMutation.mutate(
      {
        id: editAlvo.id,
        body: { nome: editNome, email: editEmail, centro_custo_id: editCentroCustoId },
      },
      { onSuccess: () => setEditAlvo(null) },
    );
  }

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold text-foreground">Usuários</h1>
        <div className="flex gap-2">
        <Dialog open={cargaOpen} onOpenChange={fecharDialogCarga}>
          <DialogTrigger asChild>
            <Button size="sm" variant="outline">
              <Upload className="mr-2 h-4 w-4" />
              Carregar planilha
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>Carregar planilha de usuários</DialogTitle>
            </DialogHeader>
            {!resultadoCarga ? (
              <>
                <p className="text-xs text-muted-foreground">
                  Arquivo CSV (separador &quot;;&quot;, UTF-8). Colunas obrigatórias:{' '}
                  <code>email;nome;cod_empresa;numcad</code>. Opcionais: <code>centro_custo_codigo</code> e{' '}
                  <code>centro_custo_nome</code> (se o CC não existir ainda, é criado automaticamente quando o nome
                  vier preenchido). A ordem das colunas não importa, maiúsculas/minúsculas tanto faz, e colunas
                  extras (ex. &quot;perfil&quot;) são ignoradas — todo usuário criado entra como solicitante,
                  promova a administrador depois na tabela abaixo.
                </p>
                <Input
                  type="file"
                  accept=".csv,text/csv"
                  onChange={(e) => setArquivoCarga(e.target.files?.[0] ?? null)}
                />
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
              </>
            ) : (
              <>
                <div className="flex flex-col gap-2 text-sm">
                  <p>
                    <strong>{resultadoCarga.criados}</strong> criados, <strong>{resultadoCarga.atualizados}</strong>{' '}
                    atualizados, <strong>{resultadoCarga.ignorados}</strong> ignorados (de {resultadoCarga.total}).
                  </p>
                  {resultadoCarga.erros.length > 0 && (
                    <div className="max-h-48 overflow-y-auto rounded-md border border-border p-2 text-xs text-destructive">
                      {resultadoCarga.erros.map((erro, i) => (
                        <p key={i}>{erro}</p>
                      ))}
                    </div>
                  )}
                </div>
                <DialogFooter>
                  <Button onClick={() => fecharDialogCarga(false)}>Fechar</Button>
                </DialogFooter>
              </>
            )}
          </DialogContent>
        </Dialog>
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <UserPlus className="mr-2 h-4 w-4" />
              Novo usuário
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Novo usuário local</DialogTitle>
            </DialogHeader>
            <div className="grid gap-4 py-2">
              <div className="grid gap-1.5">
                <Label htmlFor="novo-email">E-mail</Label>
                <Input id="novo-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="novo-nome">Nome</Label>
                <Input id="novo-nome" value={nome} onChange={(e) => setNome(e.target.value)} />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="novo-senha">Senha (mínimo 8 caracteres)</Label>
                <Input id="novo-senha" type="password" value={senha} onChange={(e) => setSenha(e.target.value)} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-1.5">
                  <Label htmlFor="novo-cod-empresa">Código da empresa</Label>
                  <Input
                    id="novo-cod-empresa"
                    type="number"
                    max={2147483647}
                    value={codEmpresa}
                    onChange={(e) => setCodEmpresa(e.target.value)}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="novo-numcad">Matrícula (NUMCAD)</Label>
                  <Input
                    id="novo-numcad"
                    type="number"
                    max={2147483647}
                    value={numcad}
                    onChange={(e) => setNumcad(e.target.value)}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label>Perfil</Label>
                <Select value={perfil} onValueChange={(v) => setPerfil(v as 'solicitante' | 'administrador')}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="solicitante">Solicitante</SelectItem>
                    <SelectItem value="administrador">Administrador</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>
                Cancelar
              </Button>
              <Button
                onClick={() => createMutation.mutate()}
                disabled={
                  createMutation.isPending || !email || !nome || senha.length < 8 || !codEmpresa || !numcad
                }
              >
                {createMutation.isPending ? 'Criando...' : 'Criar'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        </div>
      </div>

      <div className="relative max-w-sm">
        <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          value={busca}
          onChange={(e) => setBusca(e.target.value)}
          placeholder="Buscar por nome, e-mail, empresa/matrícula ou centro de custo..."
          className="pl-8"
        />
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}

      {usuarios && (
        <>
          <p className="text-xs text-muted-foreground">
            {usuariosFiltrados.length} de {usuarios.length} usuário(s)
          </p>
          <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nome</TableHead>
              <TableHead>E-mail</TableHead>
              <TableHead>Perfil</TableHead>
              <TableHead>Ativo</TableHead>
              <TableHead>Aprovador</TableHead>
              <TableHead>Empresa / Matrícula (Oracle)</TableHead>
              <TableHead>Centro de custo</TableHead>
              <TableHead>Criado em</TableHead>
              <TableHead>Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {usuariosFiltrados.map((u) => (
              <TableRow key={u.id}>
                <TableCell className="font-medium">{u.nome}</TableCell>
                <TableCell className="text-muted-foreground">{u.email}</TableCell>
                <TableCell>
                  <Select
                    value={u.perfil}
                    onValueChange={(v) => updateMutation.mutate({ id: u.id, body: { perfil: v } })}
                  >
                    <SelectTrigger className="h-8 w-40">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="solicitante">Solicitante</SelectItem>
                      <SelectItem value="administrador">Administrador</SelectItem>
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={u.ativo}
                      onCheckedChange={(checked) => updateMutation.mutate({ id: u.id, body: { ativo: checked } })}
                    />
                    <Badge variant={u.ativo ? 'default' : 'secondary'}>{u.ativo ? 'Ativo' : 'Inativo'}</Badge>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2" title="Libera o usuário como autorizador em qualquer centro de custo/faixa/teto, sem precisar cadastrá-lo em Cadastros Mestres">
                    <Switch
                      checked={u.aprovador}
                      onCheckedChange={(checked) => updateMutation.mutate({ id: u.id, body: { aprovador: checked } })}
                    />
                    {u.aprovador && <Badge variant="default">Aprovador</Badge>}
                  </div>
                </TableCell>
                <TableCell>
                  <IdentidadeOracleCell
                    usuario={u}
                    onSalvar={(codEmpresa, numcadValor) =>
                      updateMutation.mutate({ id: u.id, body: { cod_empresa: codEmpresa, numcad: numcadValor } })
                    }
                  />
                </TableCell>
                <TableCell className="text-muted-foreground">{u.centro_custo_nome ?? '—'}</TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {new Date(u.created_at).toLocaleDateString('pt-BR')}
                </TableCell>
                <TableCell>
                  <div className="flex gap-2">
                    <Button size="sm" variant="outline" onClick={() => abrirEdicao(u)}>
                      <Pencil className="mr-1 h-3.5 w-3.5" />
                      Editar
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => liberarAcessoMutation.mutate(u.id)}
                      disabled={liberarAcessoMutation.isPending}
                    >
                      <Mail className="mr-1 h-3.5 w-3.5" />
                      Liberar acesso
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
          </Table>
        </>
      )}

      <Dialog open={editAlvo !== null} onOpenChange={(o) => !o && setEditAlvo(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Editar usuário</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="grid gap-1.5">
              <Label htmlFor="edit-nome">Nome</Label>
              <Input id="edit-nome" value={editNome} onChange={(e) => setEditNome(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="edit-email">E-mail</Label>
              <Input id="edit-email" type="email" value={editEmail} onChange={(e) => setEditEmail(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label>Centro de custo</Label>
              <Select value={editCentroCustoId || '__nenhum__'} onValueChange={(v) => setEditCentroCustoId(v === '__nenhum__' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__nenhum__">Nenhum</SelectItem>
                  {(centrosCusto ?? []).map((cc) => (
                    <SelectItem key={cc.id} value={cc.id}>
                      {cc.codigo} — {cc.nome}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditAlvo(null)}>
              Cancelar
            </Button>
            <Button onClick={salvarEdicao} disabled={updateMutation.isPending || !editNome || !editEmail}>
              {updateMutation.isPending ? 'Salvando...' : 'Salvar'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default GestaoUsuarios;
