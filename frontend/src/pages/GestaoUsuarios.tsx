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
import { UserPlus } from 'lucide-react';

// Tela de gestão de usuários — consome os 3 endpoints administrativos do
// backend (handlers/admin.go): GET /api/admin/usuarios (lista paginada,
// AD-14), POST /api/admin/usuarios (CreateUsuarioLocalHandler) e
// PATCH /api/admin/usuarios/{id} (AdminUpdateUsuarioHandler — perfil/ativo).
// O Authorization é injetado automaticamente pelo interceptor global de
// fetch (AuthContext.tsx), não precisa setar aqui.
interface Usuario {
  id: string;
  email: string;
  nome: string;
  perfil: 'solicitante' | 'administrador';
  ativo: boolean;
  created_at: string;
}

interface ListaResponse {
  items: Usuario[];
  pagina: number;
  tamanho: number;
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

const GestaoUsuarios = () => {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState('');
  const [nome, setNome] = useState('');
  const [senha, setSenha] = useState('');
  const [perfil, setPerfil] = useState<'solicitante' | 'administrador'>('solicitante');

  const { data, isLoading, error } = useQuery({
    queryKey: ['usuarios'],
    queryFn: () => fetchJSON<ListaResponse>('/api/admin/usuarios?tamanho=100'),
  });

  const createMutation = useMutation({
    mutationFn: () =>
      fetchJSON('/api/admin/usuarios', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, nome, senha, perfil }),
      }),
    onSuccess: () => {
      toast.success('Usuário criado com sucesso');
      setCreateOpen(false);
      setEmail('');
      setNome('');
      setSenha('');
      setPerfil('solicitante');
      queryClient.invalidateQueries({ queryKey: ['usuarios'] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Partial<Pick<Usuario, 'perfil' | 'ativo'>> }) =>
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

  return (
    <div className="p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold text-foreground">Usuários</h1>
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
                disabled={createMutation.isPending || !email || !nome || senha.length < 8}
              >
                {createMutation.isPending ? 'Criando...' : 'Criar'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}

      {data && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nome</TableHead>
              <TableHead>E-mail</TableHead>
              <TableHead>Perfil</TableHead>
              <TableHead>Ativo</TableHead>
              <TableHead>Criado em</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.map((u) => (
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
                <TableCell className="text-xs text-muted-foreground">
                  {new Date(u.created_at).toLocaleDateString('pt-BR')}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
};

export default GestaoUsuarios;
