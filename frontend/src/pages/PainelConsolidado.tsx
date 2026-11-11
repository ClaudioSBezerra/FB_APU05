import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { toast } from 'sonner'
import { RefreshCw } from 'lucide-react'

// PainelConsolidado — Story 5.1 (Epic 5, FR-17). Página autenticada que
// busca o painel agregado pré-calculado em GET /api/paineis/consolidado
// (o servidor lê só painel_snapshots, nunca as tabelas transacionais) e
// renderiza os 3 agrupamentos: por status como blocos de indicador (+ total),
// por tipo e por centro de custo como barras horizontais ranqueadas em CSS.
// Total e percentuais são só a soma/razão dos volumes já recebidos — nenhum
// indicador novo — e não há biblioteca de gráficos (Boundaries "Never" da
// spec 5.1).
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

const numero = new Intl.NumberFormat('pt-BR')
const percentual = new Intl.NumberFormat('pt-BR', { style: 'percent', maximumFractionDigits: 1 })

// Bloco de indicador (dataviz: stat tile) — rótulo em sentence case, número
// em semibold. Sem cor de série: os status são texto + número, nunca só cor.
function Indicador({ rotulo, valor, destaque = false }: { rotulo: string; valor: number; destaque?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border border-border bg-card px-4 py-3">
      <span className="truncate text-xs text-fg-helper">{rotulo}</span>
      <span className={destaque ? 'text-2xl font-semibold text-foreground' : 'text-xl font-semibold text-foreground'}>
        {numero.format(valor)}
      </span>
    </div>
  )
}

const LIMITE_INICIAL = 8

// Barras horizontais ranqueadas (dataviz: part-to-whole com > 6 partes /
// comparação de magnitude) — uma série só, um tom (--green-250, contraste
// >= 3:1 contra o card), barra fina de 8px com ponta arredondada de 4px e
// base reta, nome + valor sempre visíveis (a lista é a própria tabela). O
// título nomeia a série, então não há legenda.
function BarrasRanqueadas({ titulo, itens, unidade }: { titulo: string; itens: PainelItem[]; unidade: string }) {
  const [expandido, setExpandido] = useState(false)
  const ordenados = [...itens].sort((a, b) => b.quantidade - a.quantidade)
  const total = ordenados.reduce((acc, i) => acc + i.quantidade, 0)
  const maximo = ordenados[0]?.quantidade ?? 0
  const visiveis = expandido ? ordenados : ordenados.slice(0, LIMITE_INICIAL)

  return (
    <section className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-4">
      <h2 className="text-base font-semibold text-foreground">{titulo}</h2>
      {ordenados.length === 0 ? (
        <p className="text-sm text-fg-helper">Nenhuma solicitação neste agrupamento ainda.</p>
      ) : (
        <ul className="flex flex-col gap-2.5">
          {visiveis.map((item) => {
            const share = total > 0 ? item.quantidade / total : 0
            const descricao = `${item.rotulo}: ${numero.format(item.quantidade)} ${unidade} (${percentual.format(share)})`
            return (
              <li key={item.chave} title={descricao} aria-label={descricao} className="group flex flex-col gap-1">
                <div className="flex items-baseline justify-between gap-3 text-[13px]">
                  <span className="truncate text-foreground">{item.rotulo}</span>
                  <span className="shrink-0 tabular-nums text-fg-secondary">
                    <span className="font-medium text-foreground">{numero.format(item.quantidade)}</span>
                    <span className="ml-1.5 text-xs text-fg-muted">{percentual.format(share)}</span>
                  </span>
                </div>
                <div className="h-2 w-full" aria-hidden>
                  <div
                    className="h-2 rounded-r bg-[var(--green-250)] transition-[filter] duration-150 group-hover:brightness-110"
                    style={{ width: `${maximo > 0 ? Math.max((item.quantidade / maximo) * 100, 1) : 0}%` }}
                  />
                </div>
              </li>
            )
          })}
        </ul>
      )}
      {ordenados.length > LIMITE_INICIAL && (
        <Button variant="ghost" size="sm" className="self-start" onClick={() => setExpandido((v) => !v)}>
          {expandido ? 'Mostrar menos' : `Mostrar todos (${numero.format(ordenados.length)})`}
        </Button>
      )}
    </section>
  )
}

function Carregando() {
  return (
    <div className="flex w-full flex-col gap-4" data-testid="painel-loading" aria-busy="true" aria-label="Carregando painel">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="h-[68px] animate-pulse rounded-lg bg-border/60" />
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="h-56 animate-pulse rounded-lg bg-border/60" />
        <div className="h-56 animate-pulse rounded-lg bg-border/60" />
      </div>
    </div>
  )
}

function PainelConsolidado() {
  const { isAuthenticated, user } = useAuth()
  const navigate = useNavigate()
  const [painel, setPainel] = useState<PainelConsolidadoResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [recalculando, setRecalculando] = useState(false)

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/login', { replace: true })
    }
  }, [isAuthenticated, navigate])

  const buscarPainel = useCallback(() => {
    return fetch('/api/paineis/consolidado')
      .then(async (res) => {
        const data = (await res.json()) as PainelConsolidadoResponse
        if (!res.ok) {
          throw new Error(data.error || `HTTP ${res.status}`)
        }
        return data
      })
      .then((data) => setPainel(data))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(() => {
    if (!isAuthenticated) return
    buscarPainel()
  }, [isAuthenticated, buscarPainel])

  // Recalcular agora — dispara o mesmo job que roda sozinho a cada 5
  // minutos (handlers/painel_calculo.go), só que na hora; útil para não
  // esperar o próximo ciclo depois de testar uma solicitação nova.
  async function recalcularAgora() {
    setRecalculando(true)
    try {
      const res = await fetch('/api/admin/paineis/consolidado/recalcular', { method: 'POST' })
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data?.error || `HTTP ${res.status}`)
      }
      await buscarPainel()
      toast.success('Painel recalculado')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao recalcular painel')
    } finally {
      setRecalculando(false)
    }
  }

  if (!isAuthenticated) {
    return null
  }

  const total = painel ? painel.por_status.reduce((acc, i) => acc + i.quantidade, 0) : 0

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-lg font-semibold text-foreground">Painel consolidado de solicitações</h1>
          {painel && (
            <p className="text-xs text-fg-helper">
              {painel.gerado_em
                ? `Atualizado em ${new Date(painel.gerado_em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })}. Recalculado automaticamente a cada 5 minutos.`
                : 'O painel ainda não foi calculado.'}
            </p>
          )}
        </div>
        {user?.perfil === 'administrador' && (
          <Button size="sm" variant="outline" onClick={recalcularAgora} disabled={recalculando}>
            <RefreshCw className={recalculando ? 'animate-spin' : undefined} />
            {recalculando ? 'Recalculando...' : 'Recalcular agora'}
          </Button>
        )}
      </div>

      {error && (
        <div
          role="alert"
          data-testid="painel-error"
          className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger-border bg-danger-bg px-4 py-3 text-sm text-danger-text"
        >
          <span>Não foi possível carregar o painel. {error}</span>
          <Button size="sm" variant="outline" onClick={() => { setError(null); buscarPainel() }}>
            Tentar de novo
          </Button>
        </div>
      )}

      {!error && !painel && <Carregando />}

      {!error && painel && (
        <div className="flex flex-col gap-4" data-testid="painel-consolidado">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-[repeat(auto-fit,minmax(150px,1fr))]">
            <Indicador rotulo="Total de solicitações" valor={total} destaque />
            {painel.por_status.map((item) => (
              <Indicador key={item.chave} rotulo={item.rotulo} valor={item.quantidade} />
            ))}
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <BarrasRanqueadas titulo="Por tipo de solicitação" itens={painel.por_tipo} unidade="solicitações" />
            <BarrasRanqueadas titulo="Por centro de custo" itens={painel.por_cc} unidade="solicitações" />
          </div>
        </div>
      )}
    </div>
  )
}

export default PainelConsolidado
