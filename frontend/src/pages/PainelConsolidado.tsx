import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { formatarCC } from '@/lib/centroCusto'

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
  escopo?: 'meus'
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
                    <span className="ml-3 inline-block min-w-[3.75rem] border-l border-border pl-3 text-right text-xs text-fg-muted">{percentual.format(share)}</span>
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

type PainelFilial = { divisao: string; rotulo: string; transferido: number; incluido: number; chamados: number }
type Pessoa = { id: string; nome: string }
const moedaBR = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL', maximumFractionDigits: 0 })

// Período (F5, 26/11): mês atual, mês passado, ano, tudo ou datas livres — vira de/ate (data de abertura).
type Periodo = 'mes' | 'mes_passado' | 'ano' | 'tudo' | 'livre'
const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
function intervalo(p: Periodo, de: string, ate: string): { de?: string; ate?: string } {
  const h = new Date()
  if (p === 'mes') return { de: iso(new Date(h.getFullYear(), h.getMonth(), 1)), ate: iso(h) }
  if (p === 'mes_passado') return { de: iso(new Date(h.getFullYear(), h.getMonth() - 1, 1)), ate: iso(new Date(h.getFullYear(), h.getMonth(), 0)) }
  if (p === 'ano') return { de: iso(new Date(h.getFullYear(), 0, 1)), ate: iso(h) }
  if (p === 'livre') return { de: de || undefined, ate: ate || undefined }
  return {}
}

// Resultado (F5): os cartões contam pelo status, na ordem em que a pessoa lê.
const RESULTADOS: [string, string][] = [
  ['finalizada_sucesso', 'Finalizadas com sucesso'],
  ['finalizada_erro', 'Finalizadas com erro'],
  ['pendente', 'Com pendência'],
  ['aberta', 'Abertas'],
  ['em_atendimento', 'Em atendimento'],
  ['aguardando_aprovacao', 'Aguardando aprovação'],
  ['cancelada', 'Canceladas'],
]

function TabelaFiliais({ itens }: { itens: PainelFilial[] }) {
  const ordenados = [...itens].sort((a, b) => b.transferido + b.incluido - (a.transferido + a.incluido) || b.chamados - a.chamados)
  return (
    <section className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-base font-semibold text-foreground">Por filial</h2>
        <p className="text-xs text-fg-helper">Valor já transferido (saída da filial de origem) e já incluído, de chamados finalizados com sucesso no período.</p>
      </div>
      {ordenados.length === 0 ? (
        <p className="text-sm text-fg-helper">Nenhuma solicitação no período.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm tabular-nums">
            <thead className="text-xs text-fg-helper">
              <tr className="border-b border-border">
                <th className="py-1.5 text-left font-medium">Filial</th>
                <th className="py-1.5 text-right font-medium">Transferido</th>
                <th className="py-1.5 text-right font-medium">Incluído</th>
                <th className="py-1.5 text-right font-medium">Chamados</th>
              </tr>
            </thead>
            <tbody>
              {ordenados.map((f) => (
                <tr key={f.divisao} className="border-b border-border last:border-0">
                  <td className="py-1.5 pr-3"><span className="font-medium">{f.divisao}</span> <span className="text-fg-secondary">{f.rotulo !== f.divisao ? f.rotulo : ''}</span></td>
                  <td className="py-1.5 text-right">{f.transferido ? moedaBR.format(f.transferido) : '—'}</td>
                  <td className="py-1.5 text-right">{f.incluido ? moedaBR.format(f.incluido) : '—'}</td>
                  <td className="py-1.5 text-right">{numero.format(f.chamados)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function PainelConsolidado() {
  const { isAuthenticated } = useAuth()
  const navigate = useNavigate()
  const [painel, setPainel] = useState<(PainelConsolidadoResponse & { por_filial?: PainelFilial[]; solicitantes?: Pessoa[] }) | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [periodo, setPeriodo] = useState<Periodo>('ano')
  const [de, setDe] = useState('')
  const [ate, setAte] = useState('')
  const [quem, setQuem] = useState('')
  const [pessoas, setPessoas] = useState<Pessoa[]>([])

  useEffect(() => {
    if (!isAuthenticated) navigate('/login', { replace: true })
  }, [isAuthenticated, navigate])

  const buscarPainel = useCallback(() => {
    const p = new URLSearchParams()
    const r = intervalo(periodo, de, ate)
    if (r.de) p.set('de', r.de)
    if (r.ate) p.set('ate', r.ate)
    if (quem) p.set('solicitante', quem)
    return fetch(`/api/paineis/consolidado?${p}`)
      .then(async (res) => {
        const data = (await res.json()) as PainelConsolidadoResponse & { por_filial?: PainelFilial[]; solicitantes?: Pessoa[] }
        if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
        return data
      })
      .then((data) => {
        setPainel(data)
        // a lista de "Quem abriu" não depende do filtro escolhido: guarda a primeira que vier
        if ((data.solicitantes ?? []).length > 0) setPessoas(data.solicitantes ?? [])
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }, [periodo, de, ate, quem])

  useEffect(() => {
    if (!isAuthenticated) return
    buscarPainel()
  }, [isAuthenticated, buscarPainel])

  if (!isAuthenticated) return null

  const total = painel ? painel.por_status.reduce((acc, i) => acc + i.quantidade, 0) : 0
  const qtdStatus = (k: string) => painel?.por_status.find((i) => i.chave === k)?.quantidade ?? 0

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold text-foreground">
          {painel?.escopo === 'meus' ? 'Painel das minhas solicitações' : 'Painel de solicitações'}
        </h1>
        <p className="text-xs text-fg-helper">
          {painel?.escopo === 'meus'
            ? 'Os chamados que você abriu, os que acompanha em cópia e os dos centros de custo dos seus grupos.'
            : 'Todos os chamados.'}{' '}
          Atualizado agora.
        </p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1">
          <label className="text-xs text-fg-helper" htmlFor="painel-periodo">Período (abertura)</label>
          <select id="painel-periodo" className="h-9 rounded-md border border-input bg-card px-2 text-sm" value={periodo} onChange={(e) => setPeriodo(e.target.value as Periodo)}>
            <option value="mes">Mês atual</option>
            <option value="mes_passado">Mês passado</option>
            <option value="ano">Ano atual</option>
            <option value="tudo">Tudo</option>
            <option value="livre">Datas livres</option>
          </select>
        </div>
        {periodo === 'livre' && (
          <>
            <div className="grid gap-1">
              <label className="text-xs text-fg-helper" htmlFor="painel-de">De</label>
              <input id="painel-de" type="date" className="h-9 rounded-md border border-input bg-card px-2 text-sm" value={de} onChange={(e) => setDe(e.target.value)} />
            </div>
            <div className="grid gap-1">
              <label className="text-xs text-fg-helper" htmlFor="painel-ate">Até</label>
              <input id="painel-ate" type="date" className="h-9 rounded-md border border-input bg-card px-2 text-sm" value={ate} onChange={(e) => setAte(e.target.value)} />
            </div>
          </>
        )}
        {pessoas.length > 0 && (
          <div className="grid gap-1">
            <label className="text-xs text-fg-helper" htmlFor="painel-quem">Quem abriu</label>
            <select id="painel-quem" className="h-9 rounded-md border border-input bg-card px-2 text-sm" value={quem} onChange={(e) => setQuem(e.target.value)}>
              <option value="">Todos</option>
              {pessoas.map((p) => <option key={p.id} value={p.id}>{p.nome}</option>)}
            </select>
          </div>
        )}
      </div>

      {error && (
        <div role="alert" data-testid="painel-error" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger-border bg-danger-bg px-4 py-3 text-sm text-danger-text">
          <span>Não foi possível carregar o painel. {error}</span>
          <Button size="sm" variant="outline" onClick={() => { setError(null); buscarPainel() }}>Tentar de novo</Button>
        </div>
      )}

      {!error && !painel && <Carregando />}

      {!error && painel && (
        <div className="flex flex-col gap-4" data-testid="painel-consolidado">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-8">
            <Indicador rotulo="Total" valor={total} destaque />
            {RESULTADOS.map(([k, r]) => <Indicador key={k} rotulo={r} valor={qtdStatus(k)} />)}
          </div>
          <TabelaFiliais itens={painel.por_filial ?? []} />
          <div className="grid gap-4 lg:grid-cols-2">
            <BarrasRanqueadas titulo="Por tipo de solicitação" itens={painel.por_tipo} unidade="solicitações" />
            <BarrasRanqueadas titulo="Por centro de custo" itens={painel.por_cc.map((i) => ({ ...i, rotulo: `${formatarCC(i.chave)} — ${i.rotulo}` }))} unidade="solicitações" />
          </div>
        </div>
      )}
    </div>
  )
}

export default PainelConsolidado
