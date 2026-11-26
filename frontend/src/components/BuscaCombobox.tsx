import { useEffect, useMemo, useRef, useState } from 'react';
import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';

export interface OpcaoBusca {
  id: string;
  codigo: string;
  nome: string;
  // texto extra só para a busca (ex.: o código do CC em outros formatos, F13)
  busca?: string;
  // opção visível mas não selecionável (ex.: ordem bloqueada), com o motivo
  desabilitada?: boolean;
  motivo?: string;
}

// Busca sem acento/caixa: "manutencao" acha "Manutenção".
export const normalizar = (t: string) => t.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();

// BuscaCombobox (11/11, item 1) — escolha com busca por qualquer parte do código OU da descrição (todos os termos
// digitados precisam aparecer, em qualquer ordem). Teclado: ↑/↓ navega, Enter escolhe, Esc fecha.
// B5 (26/11): ao fechar (Esc, clique fora ou perda de foco) o termo digitado e a lista filtrada são apagados.
export function BuscaCombobox({
  value,
  onChange,
  opcoes,
  placeholder = 'Escolha a conta',
  vazio = 'Nenhuma conta encontrada.',
  limparRotulo,
  disabled,
  ariaLabel,
}: {
  value: string;
  onChange: (id: string) => void;
  opcoes: OpcaoBusca[];
  placeholder?: string;
  vazio?: string;
  // quando informado, a primeira opção da lista limpa a escolha (value '') com este texto
  limparRotulo?: string;
  disabled?: boolean;
  ariaLabel?: string;
}) {
  const [aberto, setAberto] = useState(false);
  const [busca, setBusca] = useState('');
  const [indice, setIndice] = useState(0);
  const raiz = useRef<HTMLDivElement>(null);
  // índice normalizado uma vez por lista (listas de milhares de itens: não refazer a cada tecla)
  const indiceBusca = useMemo(() => {
    const base: OpcaoBusca[] = limparRotulo ? [{ id: '', codigo: '', nome: limparRotulo }, ...opcoes] : opcoes;
    return base.map((o) => ({ o, alvo: normalizar(`${o.codigo} ${o.nome} ${o.busca ?? ''}`) }));
  }, [opcoes, limparRotulo]);
  const selecionada = indiceBusca.find((x) => x.o.id === value)?.o;

  const resultados = useMemo(() => {
    const termos = normalizar(busca).split(/\s+/).filter(Boolean);
    const achados = termos.length === 0 ? indiceBusca : indiceBusca.filter((x) => termos.every((t) => x.alvo.includes(t)));
    return achados.map((x) => x.o);
  }, [busca, indiceBusca]);
  const visiveis = resultados.slice(0, 60);

  function fechar() {
    setAberto(false);
    setBusca('');
    setIndice(0);
  }

  useEffect(() => {
    if (!aberto) return;
    function fora(e: MouseEvent) {
      if (raiz.current && !raiz.current.contains(e.target as Node)) fechar();
    }
    document.addEventListener('mousedown', fora);
    return () => document.removeEventListener('mousedown', fora);
  }, [aberto]);

  function escolher(o: OpcaoBusca) {
    if (o.desabilitada) return;
    onChange(o.id);
    fechar();
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIndice((i) => Math.min(i + 1, visiveis.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIndice((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && visiveis[indice]) {
      e.preventDefault();
      escolher(visiveis[indice]);
    } else if (e.key === 'Escape') {
      fechar();
    }
  }

  return (
    <div
      ref={raiz}
      className="relative min-w-0"
      onBlur={(e) => {
        // perdeu o foco para fora do combo (Tab, por exemplo): fecha e limpa a busca
        if (aberto && !raiz.current?.contains(e.relatedTarget as Node)) fechar();
      }}
    >
      {aberto ? (
        <Input
          autoFocus
          role="combobox"
          aria-expanded="true"
          aria-label={ariaLabel}
          placeholder="Digite parte do código ou da descrição"
          value={busca}
          onChange={(e) => {
            setBusca(e.target.value);
            setIndice(0);
          }}
          onKeyDown={handleKeyDown}
        />
      ) : (
        <button
          type="button"
          disabled={disabled}
          aria-label={ariaLabel}
          onClick={() => {
            setBusca('');
            setAberto(true);
            setIndice(0);
          }}
          className="flex h-10 w-full items-center justify-between rounded-md border border-input bg-card px-3 py-2 text-left text-sm focus:border-ring focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:bg-[var(--bg-input-disabled)]"
        >
          <span className={selecionada ? 'truncate text-foreground' : 'truncate text-fg-muted'}>
            {selecionada ? (selecionada.codigo ? `${selecionada.codigo} — ${selecionada.nome}` : selecionada.nome) : placeholder}
          </span>
          <Search className="size-4 shrink-0 text-fg-muted" />
        </button>
      )}
      {aberto && (
        <div className="absolute z-30 mt-1 max-h-72 w-full overflow-y-auto rounded-md border border-border bg-card shadow-popover" role="listbox">
          {visiveis.length === 0 && <p className="p-3 text-xs text-fg-helper">{vazio}</p>}
          {visiveis.map((o, i) => (
            <button
              key={o.id || '__limpar'}
              type="button"
              role="option"
              aria-selected={o.id === value}
              aria-disabled={o.desabilitada || undefined}
              title={o.motivo}
              className={`block w-full px-3 py-2 text-left text-sm ${o.desabilitada ? 'cursor-not-allowed text-danger-text opacity-70' : i === indice ? 'bg-muted' : 'hover:bg-muted/60'}`}
              onMouseEnter={() => setIndice(i)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => escolher(o)}
            >
              {o.codigo ? (
                <>
                  <span className="font-medium">{o.codigo}</span> — {o.nome}
                </>
              ) : (
                o.nome
              )}
            </button>
          ))}
          {resultados.length > visiveis.length && (
            <p className="border-t border-border p-2 text-xs text-fg-helper">
              Mostrando {visiveis.length} de {resultados.length}. Digite mais para refinar.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
