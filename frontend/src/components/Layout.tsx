import { useLocation } from 'react-router-dom';
import { AppRail } from '@/components/AppRail';

// Header + rail — réplica do AppHeader/AppLayout do FB_APU02 (ver App.tsx de
// lá), simplificado: sem módulos com abas (FB_APU05 não tem esse conceito
// ainda, só duas páginas), sem FilialSelector/CompanySwitcher (AD-7).
const titles: Record<string, string> = {
  '/painel': 'Painel Consolidado',
  '/admin/usuarios': 'Gestão de Usuários',
};

function AppHeader() {
  const location = useLocation();
  const title = titles[location.pathname];
  if (!title) return null;

  return (
    <header className="flex items-center h-12 border-b bg-white px-4 shrink-0">
      <span className="text-sm font-semibold text-foreground">{title}</span>
    </header>
  );
}

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen">
      <AppRail />
      <div className="flex-1 flex flex-col overflow-hidden">
        <AppHeader />
        <main className="flex-1 overflow-auto">{children}</main>
      </div>
    </div>
  );
}
