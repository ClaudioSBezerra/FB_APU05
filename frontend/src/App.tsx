import { Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from '@/components/ui/sonner'
import { useAuth } from '@/contexts/AuthContext'
import { Layout } from '@/components/Layout'
import Login from '@/pages/Login'
import AuthCallback from '@/pages/AuthCallback'
import AuthError from '@/pages/AuthError'
import PainelConsolidado from '@/pages/PainelConsolidado'
import ForgotPassword from '@/pages/ForgotPassword'
import ResetPassword from '@/pages/ResetPassword'
import ChangePassword from '@/pages/ChangePassword'
import GestaoUsuarios from '@/pages/GestaoUsuarios'
import AbrirSolicitacao from '@/pages/AbrirSolicitacao'
import FilaAdministrador from '@/pages/FilaAdministrador'
import ERPBridgeCredenciais from '@/pages/ERPBridgeCredenciais'
import CadastrosMestres from '@/pages/CadastrosMestres'

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
    return <Navigate to="/painel" replace />
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
        <Route
          path="/*"
          element={
            <ProtectedRoute>
              <Layout>
                <Routes>
                  <Route path="/" element={<Navigate to="/painel" replace />} />
                  <Route path="/painel" element={<PainelConsolidado />} />
                  <Route path="/solicitacoes/nova" element={<AbrirSolicitacao />} />
                  <Route
                    path="/fila"
                    element={
                      <AdminRoute>
                        <FilaAdministrador />
                      </AdminRoute>
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
                    path="/admin/cadastros"
                    element={
                      <AdminRoute>
                        <CadastrosMestres />
                      </AdminRoute>
                    }
                  />
                  <Route path="*" element={<Navigate to="/painel" replace />} />
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
