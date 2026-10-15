import { useEffect, useState } from 'react'
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

const queryClient = new QueryClient()

type HealthResponse = {
  status: string
  error?: string
}

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
    return <Navigate to="/" replace />
  }
  return <>{children}</>
}

// Home — tela inicial do módulo. Mostra o health-check (herdado de Story
// 1.1) e o usuário autenticado, pra tornar o fluxo de ponta a ponta (login →
// sessão → logout) visível/verificável manualmente.
function Home() {
  const { user, logout } = useAuth()
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then(async (res) => {
        const data = (await res.json()) as HealthResponse
        if (!res.ok) {
          throw new Error(data.error || `HTTP ${res.status}`)
        }
        return data
      })
      .then((data) => setHealth(data))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  return (
    <div className="flex flex-col items-center justify-center gap-4 p-6 text-foreground">
      <h1 className="text-2xl font-bold">Módulo de gestão de solicitações de orçamentos</h1>
      <p className="text-sm text-muted-foreground" data-testid="health-status">
        {error && `Erro ao consultar /api/health: ${error}`}
        {!error && !health && 'Consultando /api/health...'}
        {!error && health && `/api/health respondeu: ${JSON.stringify(health)}`}
      </p>
      {user && (
        <div className="flex flex-col items-center gap-2 text-sm">
          <p>
            Logado como <strong>{user.email}</strong> ({user.perfil})
          </p>
          <button
            type="button"
            onClick={logout}
            className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-muted"
          >
            Sair
          </button>
        </div>
      )}
    </div>
  )
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
                  <Route path="/" element={<Home />} />
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
                  <Route path="*" element={<Navigate to="/" replace />} />
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
