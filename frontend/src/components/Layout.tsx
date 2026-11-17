import { useLocation } from 'react-router-dom';
import { AppRail } from '@/components/AppRail';
import { WORKSPACE } from '@/lib/workspace';

// Breadcrumb do Catálogo 2.0: navegação real, sempre enraizada no workspace
// ("Business › Página"), separador › (U+203A). Prefixo mais longo vence.
const titles: [prefix: string, title: string][] = [
  ['/solicitacoes/nova', 'Nova solicitação'],
  ['/admin/usuarios', 'Gestão de usuários'],
  ['/admin/cadastros', 'Cadastros mestres'],
  ['/admin/configuracoes', 'Configurações'],
  ['/admin/erp-bridge', 'ERP Bridge — credenciais'],
  ['/controle-ti', 'Controle TI'],
  ['/painel', 'Painel'],
  ['/fila', 'Fila de atendimento'],
];

function AppHeader() {
  const { pathname } = useLocation();
  const title = titles.find(([prefix]) => pathname.startsWith(prefix))?.[1];

  return (
    <header className="flex h-12 shrink-0 items-center border-b bg-card px-4">
      <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-sm">
        <span className="text-fg-helper">{WORKSPACE}</span>
        {title && (
          <>
            <span aria-hidden className="text-fg-muted">›</span>
            <span className="font-medium text-foreground" aria-current="page">
              {title}
            </span>
          </>
        )}
      </nav>
    </header>
  );
}

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen">
      <AppRail />
      <div className="flex flex-1 flex-col overflow-hidden">
        <AppHeader />
        <main className="flex-1 overflow-auto">{children}</main>
      </div>
    </div>
  );
}
