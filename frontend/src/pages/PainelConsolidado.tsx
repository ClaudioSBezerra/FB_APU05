import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'

// PainelConsolidado — Story 5.1 (Epic 5, FR-17). Página autenticada que
// busca o painel agregado pré-calculado em GET /api/paineis/consolidado
// (nunca agrega nada no cliente) e renderiza os 3 agrupamentos (por tipo,
// por status, por centro de custo) como listas simples — sem biblioteca de
// gráficos (Boundaries "Never" da spec: nenhuma existe hoje no
// package.json, e nenhuma AC pede visualização gráfica).
//
// Mesmo auth-gate manual de Home (App.tsx): useAuth() + useEffect
// redireciona para /login se !isAuthenticated, ANTES de qualquer chamada à
// API (Tasks & Acceptance da spec). fetch puro em /api/paineis/consolidado,
// sem montar o header Authorization manualmente — o interceptor global de
// AuthContext já injeta Bearer <token> em toda chamada a /api/* e redireciona
// para /login em 401 (mesmo padrão do fetch('/api/health') em Home).

type PainelItem = {
  chave: string
  rotulo: string
  quantidade: number
}

type PainelConsolidadoResponse = {
  por_tipo: PainelItem[]
  por_status: PainelItem[]
  por_cc: PainelItem[]
  gerado_em: string | null
  error?: string
}

function ListaAgrupamento({ titulo, itens }: { titulo: string; itens: PainelItem[] }) {
  return (
    <div className="flex w-full flex-col gap-2 rounded-md border border-border p-4">
      <h2 className="text-lg font-semibold">{titulo}</h2>
      {itens.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nenhum dado disponível ainda.</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {itens.map((item) => (
            <li key={item.chave} className="flex items-center justify-between text-sm">
              <span>{item.rotulo}</span>
              <span className="font-medium">{item.quantidade}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function PainelConsolidado() {
  const { isAuthenticated } = useAuth()
  const navigate = useNavigate()
  const [painel, setPainel] = useState<PainelConsolidadoResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/login', { replace: true })
    }
  }, [isAuthenticated, navigate])

  useEffect(() => {
    if (!isAuthenticated) return

    fetch('/api/paineis/consolidado')
      .then(async (res) => {
        const data = (await res.json()) as PainelConsolidadoResponse
        if (!res.ok) {
          throw new Error(data.error || `HTTP ${res.status}`)
        }
        return data
      })
      .then((data) => setPainel(data))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [isAuthenticated])

  if (!isAuthenticated) {
    return null
  }

  return (
    <div className="flex min-h-screen flex-col items-center gap-6 bg-background p-6 text-foreground">
      <h1 className="text-2xl font-bold">Painel consolidado de solicitações</h1>

      {error && (
        <p className="text-sm text-destructive" data-testid="painel-error">
          Erro ao consultar /api/paineis/consolidado: {error}
        </p>
      )}

      {!error && !painel && (
        <p className="text-sm text-muted-foreground" data-testid="painel-loading">
          Consultando /api/paineis/consolidado...
        </p>
      )}

      {!error && painel && (
        <div className="flex w-full max-w-3xl flex-col gap-4" data-testid="painel-consolidado">
          <p className="text-xs text-muted-foreground">
            {painel.gerado_em
              ? `Dados calculados em ${new Date(painel.gerado_em).toLocaleString('pt-BR')}`
              : 'Ainda não há snapshot calculado para este painel.'}
          </p>
          <ListaAgrupamento titulo="Por tipo de solicitação" itens={painel.por_tipo} />
          <ListaAgrupamento titulo="Por status" itens={painel.por_status} />
          <ListaAgrupamento titulo="Por centro de custo" itens={painel.por_cc} />
        </div>
      )}
    </div>
  )
}

export default PainelConsolidado
