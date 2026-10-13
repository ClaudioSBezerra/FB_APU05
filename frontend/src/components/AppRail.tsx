import { useState } from 'react';
import { LayoutDashboard, Users, LogOut, KeyRound } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router-dom';
import { cn } from '@/lib/utils';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useAuth } from '@/contexts/AuthContext';
import { toast } from 'sonner';

// Barra de navegação — réplica do AppRail do FB_APU02 (o componente que
// realmente roda lá; o ui/sidebar.tsx + AppSidebar.tsx de lá nunca chegam a
// ser renderizados, são código morto, não copiados aqui). Sem
// FilialSelector/CompanySwitcher (AD-7, single-company) nem trial_ends_at
// (sem conceito de trial no FB_APU05).
const mainItems = [
  { id: 'painel', icon: LayoutDashboard, label: 'Painel', path: '/painel' },
];

function getActiveId(pathname: string): string {
  if (pathname.startsWith('/admin/usuarios')) return 'usuarios';
  if (pathname.startsWith('/painel')) return 'painel';
  return '';
}

export function AppRail() {
  const location = useLocation();
  const navigate = useNavigate();
  const { user, logout, token } = useAuth();
  const active = getActiveId(location.pathname);

  const [pwDialog, setPwDialog] = useState(false);
  const [pwCurrent, setPwCurrent] = useState('');
  const [pwNew, setPwNew] = useState('');
  const [pwConfirm, setPwConfirm] = useState('');
  const [pwLoading, setPwLoading] = useState(false);

  async function handleChangePassword() {
    if (pwNew !== pwConfirm) {
      toast.error('As senhas não coincidem');
      return;
    }
    if (pwNew.length < 8) {
      toast.error('Mínimo 8 caracteres');
      return;
    }
    setPwLoading(true);
    try {
      const res = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ current_password: pwCurrent, new_password: pwNew }),
      });
      const data = await res.json();
      if (!res.ok) {
        toast.error(data.error || 'Erro ao alterar senha');
        return;
      }
      toast.success('Senha alterada com sucesso');
      setPwDialog(false);
      setPwCurrent('');
      setPwNew('');
      setPwConfirm('');
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setPwLoading(false);
    }
  }

  const initials =
    user?.nome
      ?.split(' ')
      .filter(Boolean)
      .slice(0, 2)
      .map((n) => n[0])
      .join('')
      .toUpperCase() ?? '?';

  const items = [
    ...mainItems,
    ...(user?.perfil === 'administrador'
      ? [{ id: 'usuarios', icon: Users, label: 'Gestão de Usuários', path: '/admin/usuarios' }]
      : []),
  ];

  return (
    <TooltipProvider delayDuration={200}>
      <div className="flex flex-col w-14 shrink-0 border-r bg-white h-screen z-20">
        <div className="flex items-center justify-center h-14 border-b shrink-0">
          <img src="/favicon-fc.png" alt="FC" className="size-8 rounded-lg object-cover" />
        </div>

        <nav className="flex flex-col items-center gap-1 p-2 flex-1 pt-3">
          {items.map((item) => (
            <Tooltip key={item.id}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => navigate(item.path)}
                  className={cn(
                    'flex items-center justify-center w-10 h-10 rounded-lg transition-colors',
                    active === item.id
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:bg-gray-100 hover:text-foreground',
                  )}
                >
                  <item.icon className="h-5 w-5" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right" className="text-xs">
                {item.label}
              </TooltipContent>
            </Tooltip>
          ))}
        </nav>

        <div className="flex flex-col items-center gap-1 p-2 border-t shrink-0">
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <button className="flex items-center justify-center w-10 h-10 rounded-lg bg-primary/10 text-primary text-xs font-bold hover:bg-primary/20 transition-colors">
                    {initials}
                  </button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent side="right" className="text-xs">
                {user?.nome}
              </TooltipContent>
            </Tooltip>

            <DropdownMenuContent side="right" align="end" className="w-52">
              <DropdownMenuLabel className="pb-1">
                <p className="text-xs font-semibold leading-tight truncate">{user?.nome}</p>
                <p className="text-[10px] text-muted-foreground font-normal truncate mt-0.5">{user?.email}</p>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem className="text-xs cursor-pointer" onClick={() => setPwDialog(true)}>
                <KeyRound className="mr-2 h-3.5 w-3.5" />
                Trocar senha
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem className="text-xs text-red-600 focus:text-red-600 cursor-pointer" onClick={logout}>
                <LogOut className="mr-2 h-3.5 w-3.5" />
                Sair
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <Dialog
        open={pwDialog}
        onOpenChange={(o) => {
          setPwDialog(o);
          if (!o) {
            setPwCurrent('');
            setPwNew('');
            setPwConfirm('');
          }
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Trocar Senha</DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="grid gap-1.5">
              <Label htmlFor="pw-current">Senha atual</Label>
              <Input id="pw-current" type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="pw-new">Nova senha</Label>
              <Input id="pw-new" type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="pw-confirm">Confirmar nova senha</Label>
              <Input
                id="pw-confirm"
                type="password"
                value={pwConfirm}
                onChange={(e) => setPwConfirm(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleChangePassword()}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPwDialog(false)}>
              Cancelar
            </Button>
            <Button onClick={handleChangePassword} disabled={pwLoading || !pwCurrent || !pwNew || !pwConfirm}>
              {pwLoading ? 'Salvando...' : 'Salvar'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}
