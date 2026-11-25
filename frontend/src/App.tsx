import { Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from '@/components/ui/sonner'
import { useAuth } from '@/contexts/AuthContext'
import { Layout } from '@/components/Layout'
import Login from '@/pages/Login'
import AuthCallback from '@/pages/AuthCallback'
import AuthError from '@/pages/AuthError'
import AprovarPorLink from '@/pages/AprovarPorLink'
import SolicitacaoDetalhe from '@/pages/SolicitacaoDetalhe'
import Solicitacoes from '@/pages/Solicitacoes'
import PainelConsolidado from '@/pages/PainelConsolidado'
import ForgotPassword from '@/pages/ForgotPassword'
import ResetPassword from '@/pages/ResetPassword'
import ChangePassword from '@/pages/ChangePassword'
import GestaoUsuarios from '@/pages/GestaoUsuarios'
import AbrirSolicitacao from '@/pages/AbrirSolicitacao'
import FilaAtendimento from '@/pages/FilaAtendimento'
import Configuracoes from '@/pages/Configuracoes'
import { paginaInicial, podeAtender } from '@/lib/perfis'
import ERPBridgeCredenciais from '@/pages/ERPBridgeCredenciais'
import CadastrosMestres from '@/pages/CadastrosMestres'
import ControleTI from '@/pages/ControleTI'
import { usePermissoes } from '@/hooks/usePermissoes'

const queryClient = new QueryClient()

// ProtectedRoute/AdminRoute — mesmo padrão do FB_APU02 (App.tsx de lá):
// AuthContext do FB_APU05 restaura a sessão de sessionStorage de forma
// síncrona (sem fetch, ver readStoredUser), então isAuthenticated já está
// correto na primeira renderização — sem necessidade de um estado "loading"
// como o de lá.
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

function AdminRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, user } = useAuth()
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  if (user?.perfil !== 'administrador') {
    return <Navigate to={paginaInicial(user?.perfil)} replace />
  }
  return <>{children}</>
}

// AtendimentoRoute — Fila de atendimento: administrador e atendente.
function AtendimentoRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, user } = useAuth()
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  if (!podeAtender(user?.perfil)) {
    return <Navigate to={paginaInicial(user?.perfil)} replace />
  }
  return <>{children}</>
}

// PaginaInicial — cada perfil cai na sua página (atendente: fila; demais: painel).
function PaginaInicial() {
  const { user } = useAuth()
  return <Navigate to={paginaInicial(user?.perfil)} replace />
}

// ControleTIRoute — só entra quem tem o interruptor "Controle TI" ligado
// (permissão lida do servidor). Enquanto a permissão carrega, não renderiza
// nada (evita piscar o redirecionamento).
function ControleTIRoute({ children }: { children: React.ReactNode }) {
  const { data, isLoading } = usePermissoes()
  if (isLoading) return null
  if (!data?.controle_ti) {
    return <PaginaInicial />
  }
  return <>{children}</>
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/esqueci-senha" element={<ForgotPassword />} />
        <Route path="/reset-senha" element={<ResetPassword />} />
        <Route path="/alterar-senha" element={<ChangePassword />} />
        <Route path="/auth/callback" element={<AuthCallback />} />
        <Route path="/auth/error" element={<AuthError />} />
        <Route path="/aprovacao/:token" element={<AprovarPorLink />} />
        <Route
          path="/*"
          element={
            <ProtectedRoute>
              <Layout>
                <Routes>
                  <Route path="/" element={<PaginaInicial />} />
                  <Route path="/painel" element={<PainelConsolidado />} />
                  <Route path="/solicitacoes/nova" element={<AbrirSolicitacao />} />
                  <Route path="/solicitacoes/:id" element={<SolicitacaoDetalhe />} />
                  <Route path="/solicitacoes" element={<Solicitacoes />} />
                  <Route
                    path="/fila/:id"
                    element={
                      <AtendimentoRoute>
                        <SolicitacaoDetalhe />
                      </AtendimentoRoute>
                    }
                  />
                  <Route
                    path="/fila"
                    element={
                      <AtendimentoRoute>
                        <FilaAtendimento />
                      </AtendimentoRoute>
                    }
                  />
                  <Route
                    path="/admin/usuarios"
                    element={
                      <AdminRoute>
                        <GestaoUsuarios />
                      </AdminRoute>
                    }
                  />
                  <Route
                    path="/admin/erp-bridge"
                    element={
                      <AdminRoute>
                        <ERPBridgeCredenciais />
                      </AdminRoute>
                    }
                  />
                  <Route
                    path="/admin/configuracoes"
                    element={
                      <AdminRoute>
                        <Configuracoes />
                      </AdminRoute>
                    }
                  />
                  <Route
                    path="/admin/cadastros"
                    element={
                      <AdminRoute>
                        <CadastrosMestres />
                      </AdminRoute>
                    }
                  />
                  <Route
                    path="/controle-ti"
                    element={
                      <ControleTIRoute>
                        <ControleTI />
                      </ControleTIRoute>
                    }
                  />
                  <Route path="*" element={<PaginaInicial />} />
                </Routes>
              </Layout>
            </ProtectedRoute>
          }
        />
      </Routes>
      <Toaster />
    </QueryClientProvider>
  )
}

export default App
