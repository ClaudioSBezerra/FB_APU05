import { useEffect, useState } from 'react'

type HealthResponse = {
  status: string
  error?: string
}

function App() {
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
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-background text-foreground">
      <h1 className="text-2xl font-bold">FB_APU05</h1>
      <p className="text-sm text-muted-foreground" data-testid="health-status">
        {error && `Erro ao consultar /api/health: ${error}`}
        {!error && !health && 'Consultando /api/health...'}
        {!error && health && `/api/health respondeu: ${JSON.stringify(health)}`}
      </p>
    </div>
  )
}

export default App
