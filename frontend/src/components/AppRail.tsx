import { podeAtender } from '@/lib/perfis';
import { useState } from 'react';
import { LayoutDashboard, Users, LogOut, KeyRound, Mail, FilePlus, ListChecks, Plug, Database, PanelLeftClose, PanelLeftOpen, Cpu, Settings, ArrowUpCircle } from 'lucide-react';
import { useQueryClient } from '@tanstack/react-query';
import { usePromocao } from '@/hooks/usePromocao';
import { Textarea } from '@/components/ui/textarea';
import { WORKSPACE } from '@/lib/workspace';
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
import { usePermissoes } from '@/hooks/usePermissoes';
import { toast } from 'sonner';

// Barra de navegação — réplica do AppRail do FB_APU02 (o componente que
// realmente roda lá; o ui/sidebar.tsx + AppSidebar.tsx de lá nunca chegam a
// ser renderizados, são código morto, não copiados aqui). Sem
// FilialSelector/CompanySwitcher (AD-7, single-company) nem trial_ends_at
// (sem conceito de trial no FB_APU05).
const RAIL_KEY = 'fb_apu05.rail.expanded';

const mainItems = [
  { id: 'nova-solicitacao', icon: FilePlus, label: 'Nova solicitação', path: '/solicitacoes/nova' },
  { id: 'painel', icon: LayoutDashboard, label: 'Painel', path: '/painel' },
];

function getActiveId(pathname: string): string {
  if (pathname.startsWith('/admin/usuarios')) return 'usuarios';
  if (pathname.startsWith('/admin/configuracoes')) return 'configuracoes';
  if (pathname.startsWith('/admin/cadastros')) return 'cadastros';
  if (pathname.startsWith('/admin/erp-bridge')) return 'erp-bridge';
  if (pathname.startsWith('/controle-ti')) return 'controle-ti';
  if (pathname.startsWith('/fila')) return 'fila';
  if (pathname.startsWith('/painel')) return 'painel';
  if (pathname.startsWith('/solicitacoes/nova')) return 'nova-solicitacao';
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
  const [forgotLoading, setForgotLoading] = useState(false);

  // "Solicitar promoção": todo usuário novo é solicitante; daqui ele pede para virar atendente e os
  // administradores recebem e-mail. Uma solicitação a cada 24 h (o servidor trava).
  const queryClient = useQueryClient();
  const { data: promocao } = usePromocao();
  const [promoDialog, setPromoDialog] = useState(false);
  const [promoMsg, setPromoMsg] = useState('');
  const [promoLoading, setPromoLoading] = useState(false);

  async function handleSolicitarPromocao() {
    if (promoLoading) return;
    setPromoLoading(true);
    try {
      const res = await fetch('/api/me/solicitar-promocao', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mensagem: promoMsg.trim() }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        toast.error(data.error || 'Não foi possível enviar o pedido');
        return;
      }
      toast.success('Pedido enviado aos administradores');
      setPromoDialog(false);
      setPromoMsg('');
      queryClient.invalidateQueries({ queryKey: ['promocao'] });
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setPromoLoading(false);
    }
  }

  // Preferência por navegador; storage pode falhar (modo privado) — cai em fechado.
  const [expanded, setExpanded] = useState(() => {
    try {
      return localStorage.getItem(RAIL_KEY) === '1';
    } catch {
      return false;
    }
  });
  function toggleExpanded() {
    setExpanded((v) => {
      try {
        localStorage.setItem(RAIL_KEY, v ? '0' : '1');
      } catch {
        /* sem persistência */
      }
      return !v;
    });
  }

  // Dispara o mesmo fluxo de "esqueci minha senha" da tela de login, mas já
  // com o e-mail do usuário logado — serve para testar se o envio de e-mail
  // está funcionando sem precisar deslogar.
  async function handleForgotPassword() {
    if (!user?.email || forgotLoading) return;
    setForgotLoading(true);
    try {
      const res = await fetch('/api/auth/forgot-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: user.email }),
      });
      const data = await res.json();
      if (!res.ok) {
        toast.error(data.error || 'Erro ao solicitar redefinição de senha');
        return;
      }
      toast.success(data.message || 'Link de redefinição enviado para seu e-mail');
    } catch {
      toast.error('Erro de conexão');
    } finally {
      setForgotLoading(false);
    }
  }

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

  // Módulo Controle TI: o menu só aparece para quem tem o interruptor ligado
  // em Gestão de Usuários (lido do servidor, ver usePermissoes).
  const { data: permissoes } = usePermissoes();
  // Atendente: só Nova solicitação e Fila de atendimento (sem Painel). Administrador: tudo.
  const filaItem = { id: 'fila', icon: ListChecks, label: 'Fila de atendimento', path: '/fila' };
  const items = [
    ...(user?.perfil === 'atendente' ? mainItems.filter((i) => i.id === 'nova-solicitacao') : mainItems),
    ...(permissoes?.controle_ti ? [{ id: 'controle-ti', icon: Cpu, label: 'Controle TI', path: '/controle-ti' }] : []),
    ...(podeAtender(user?.perfil) ? [filaItem] : []),
    ...(user?.perfil === 'administrador'
      ? [
          { id: 'usuarios', icon: Users, label: 'Gestão de usuários', path: '/admin/usuarios' },
          { id: 'cadastros', icon: Database, label: 'Cadastros mestres', path: '/admin/cadastros' },
          { id: 'configuracoes', icon: Settings, label: 'Configurações', path: '/admin/configuracoes' },
          { id: 'erp-bridge', icon: Plug, label: 'ERP Bridge — credenciais', path: '/admin/erp-bridge' },
        ]
      : []),
  ];

  const navItemCls = (isActive: boolean) =>
    cn(
      'mx-[7px] flex h-10 items-center gap-3 rounded-md px-[10px] text-sm whitespace-nowrap transition-colors duration-150',
      isActive
        ? 'bg-cta-soft font-medium text-cta-pressed'
        : 'text-fg-secondary hover:bg-border hover:text-foreground',
    );

  return (
    <TooltipProvider delayDuration={200}>
      {/* Catálogo 2.0: sidebar 54px fechada / 240px aberta (contrato do DS),
          transição de largura .25s. Chip do workspace no topo-esquerdo. */}
      <div
        className={cn(
          'z-20 flex h-screen shrink-0 flex-col overflow-hidden border-r bg-card transition-[width] [transition-duration:250ms] ease-in-out',
          expanded ? 'w-[240px]' : 'w-[54px]',
        )}
      >
        <div className="flex h-14 shrink-0 items-center gap-3 border-b px-[11px]">
          <img src="/favicon-fc.png" alt="Ferreira Costa" className="size-8 shrink-0 rounded-md object-cover" />
          {expanded && (
            <div className="min-w-0 leading-tight">
              <p className="truncate text-sm font-semibold text-foreground">{WORKSPACE}</p>
              <p className="truncate text-xs text-fg-helper">Controladoria</p>
            </div>
          )}
        </div>

        <nav className="flex flex-1 flex-col gap-1 py-2 pt-3">
          {items.map((item) => (
            <Tooltip key={item.id}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => navigate(item.path)}
                  aria-label={item.label}
                  aria-current={active === item.id ? 'page' : undefined}
                  className={navItemCls(active === item.id)}
                >
                  <item.icon className="size-5 shrink-0" />
                  {expanded && <span className="truncate">{item.label}</span>}
                </button>
              </TooltipTrigger>
              {!expanded && (
                <TooltipContent side="right">{item.label}</TooltipContent>
              )}
            </Tooltip>
          ))}
        </nav>

        <div className="flex shrink-0 flex-col gap-1 border-t py-2">
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={toggleExpanded}
                aria-label={expanded ? 'Recolher menu' : 'Expandir menu'}
                className={navItemCls(false)}
              >
                {expanded ? <PanelLeftClose className="size-5 shrink-0" /> : <PanelLeftOpen className="size-5 shrink-0" />}
                {expanded && <span className="truncate">Recolher menu</span>}
              </button>
            </TooltipTrigger>
            {!expanded && <TooltipContent side="right">Expandir menu</TooltipContent>}
          </Tooltip>

          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <button aria-label="Conta" className={cn(navItemCls(false), 'px-[6px]')}>
                    <span className="flex size-7 shrink-0 items-center justify-center rounded-avatar bg-cta-soft text-xs font-semibold text-cta-pressed">
                      {initials}
                    </span>
                    {expanded && <span className="truncate">{user?.nome}</span>}
                  </button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              {!expanded && <TooltipContent side="right">{user?.nome}</TooltipContent>}
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
              <DropdownMenuItem className="text-xs cursor-pointer" disabled={forgotLoading} onClick={handleForgotPassword}>
                <Mail className="mr-2 h-3.5 w-3.5" />
                {forgotLoading ? 'Enviando...' : 'Esqueci minha senha'}
              </DropdownMenuItem>
              {user?.perfil === 'solicitante' && (
                <DropdownMenuItem className="text-xs cursor-pointer" onClick={() => setPromoDialog(true)}>
                  <ArrowUpCircle className="mr-2 h-3.5 w-3.5" />
                  Solicitar promoção
                </DropdownMenuItem>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem className="text-xs text-destructive focus:text-destructive cursor-pointer" onClick={logout}>
                <LogOut className="mr-2 h-3.5 w-3.5" />
                Sair
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <Dialog
        open={promoDialog}
        onOpenChange={(o) => {
          setPromoDialog(o);
          if (!o) setPromoMsg('');
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Solicitar promoção</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2 text-sm">
            <p className="text-muted-foreground">
              Seu perfil hoje é <strong className="text-foreground">Solicitante</strong>. Para atender a fila, peça a
              promoção para <strong className="text-foreground">Atendente</strong>: os administradores recebem um
              e-mail com o seu pedido e quem decide é um administrador.
            </p>
            {promocao && !promocao.pode_solicitar && promocao.ultimo_pedido && (
              <p role="status" className="rounded-md border bg-muted px-3 py-2 text-xs">
                Você já pediu em{' '}
                {new Date(promocao.ultimo_pedido).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })}.
                {promocao.proximo_pedido &&
                  ` Poderá pedir de novo depois de ${new Date(promocao.proximo_pedido).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })}.`}
              </p>
            )}
            <div className="grid gap-1.5">
              <Label htmlFor="promo-msg">Mensagem (opcional)</Label>
              <Textarea
                id="promo-msg"
                rows={4}
                maxLength={500}
                placeholder="Ex.: vou atender as solicitações de TI da minha área"
                value={promoMsg}
                onChange={(e) => setPromoMsg(e.target.value)}
              />
              <span className="text-right text-xs text-muted-foreground">{promoMsg.length}/500</span>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPromoDialog(false)}>
              Cancelar
            </Button>
            <Button onClick={handleSolicitarPromocao} disabled={promoLoading || promocao?.pode_solicitar === false}>
              {promoLoading ? 'Enviando...' : 'Enviar pedido'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
            <DialogTitle>Trocar senha</DialogTitle>
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
