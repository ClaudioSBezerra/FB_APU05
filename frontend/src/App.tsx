import { useEffect, useState } from 'react'
import { Routes, Route, Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'
import Login from '@/pages/Login'
import AuthCallback from '@/pages/AuthCallback'
import AuthError from '@/pages/AuthError'
import PainelConsolidado from '@/pages/PainelConsolidado'

type HealthResponse = {
  status: string
  error?: string
}

// Home — tela protegida: exige sessão válida (Story 1.2: login via SSO
// corporativo). Mostra o health-check (herdado de Story 1.1) e o usuário
// autenticado, pra tornar o fluxo de ponta a ponta (login → sessão →
// logout) visível/verificável manualmente.
function Home() {
  const { user, isAuthenticated, logout } = useAuth()
  const navigate = useNavigate()
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/login', { replace: true })
    }
  }, [isAuthenticated, navigate])

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

  if (!isAuthenticated) {
    return null
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-background text-foreground">
      <h1 className="text-2xl font-bold">FB_APU05</h1>
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
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route path="/auth/error" element={<AuthError />} />
      <Route path="/" element={<Home />} />
      <Route path="/painel" element={<PainelConsolidado />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default App
