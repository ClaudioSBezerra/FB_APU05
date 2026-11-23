import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CheckCircle2, XCircle, PlugZap } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';

// Configurações (só administrador): integração SAP (leitura diária de centros de custo e contas).
// API: GET/PUT /api/admin/configuracoes e POST /api/admin/configuracoes/sap/{cc|gl}/testar
// (handlers/configuracoes.go). O Authorization é injetado pelo interceptor global de fetch.

interface ResultadoSAP {
  ok: boolean;
  total: number;
  mensagem: string;
  duracao_ms: number;
  origem: 'agendada' | 'manual';
}
interface ServicoSAP {
  chave: 'cc' | 'gl' | 'bg';
  diaria: boolean; // false = consulta sob demanda (saldo), sem leitura das 06:00
  nome: string;
  ativo: boolean;
  url_base: string;
  apikey_definida: boolean;
  ultima_execucao: string;
  ultimo_resultado?: ResultadoSAP;
}
interface Config {
  sap: {
    horario: string;
    usuario: string;
    senha_definida: boolean;
    apikey_definida: boolean;
    apikey_ambiente: boolean;
    sap_client: string;
    servicos: ServicoSAP[];
  };
}

async function chamar<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  return data as T;
}

const quando = (iso: string) =>
  iso ? new Date(iso).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' }) : 'nunca';

function ResultadoLinha({ r, ultima }: { r?: ResultadoSAP; ultima: string }) {
  if (!r) return <p className="text-xs text-muted-foreground">Ainda não foi executado.</p>;
  const Icone = r.ok ? CheckCircle2 : XCircle;
  return (
    <p className="flex items-start gap-1.5 text-xs" role="status">
      <Icone className={r.ok ? 'mt-0.5 size-3.5 shrink-0 text-success-text' : 'mt-0.5 size-3.5 shrink-0 text-danger-text'} />
      <span>
        <span className="font-medium">
          {quando(ultima)} ({r.origem === 'agendada' ? 'agendada' : 'teste manual'}):
        </span>{' '}
        {r.mensagem}
        {r.ok && r.duracao_ms > 0 ? ` (${r.duracao_ms} ms)` : ''}
      </span>
    </p>
  );
}

export default function Configuracoes() {
  const { data, isLoading, error } = useQuery<Config>({
    queryKey: ['configuracoes'],
    queryFn: () => chamar<Config>('/api/admin/configuracoes'),
  });
  if (isLoading) return <div className="p-6 text-sm text-muted-foreground">Carregando…</div>;
  if (error || !data) {
    return (
      <div role="alert" className="p-6 text-sm text-danger-text">
        Não foi possível carregar as configurações.
      </div>
    );
  }
  return <Formulario data={data} />;
}

// Os rascunhos nascem dos dados carregados (estado inicial) e seguem com o usuário: depois de salvar
// ou testar, os resultados mostrados vêm de `data` (atualizado), sem apagar o que ele está digitando.
function Formulario({ data }: { data: Config }) {
  const queryClient = useQueryClient();
  const [horario, setHorario] = useState(() => data.sap.horario || '06:00');
  const [usuario, setUsuario] = useState(() => data.sap.usuario);
  const [senha, setSenha] = useState('');
  const [apikey, setApikey] = useState('');
  const [sapClient, setSapClient] = useState(() => data.sap.sap_client);
  const [apikeysSv, setApikeysSv] = useState<Record<string, string>>({});
  const [urls, setUrls] = useState<Record<string, string>>(() =>
    Object.fromEntries(data.sap.servicos.map((s) => [s.chave, s.url_base])),
  );
  const [ativos, setAtivos] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(data.sap.servicos.map((s) => [s.chave, s.ativo])),
  );

  const salvar = useMutation({
    mutationFn: (corpo: object) =>
      chamar<Config>('/api/admin/configuracoes', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(corpo),
      }),
    onSuccess: (nova) => {
      queryClient.setQueryData(['configuracoes'], nova);
      setSenha('');
      setApikey('');
      setApikeysSv({});
      toast.success('Configurações salvas');
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const testar = useMutation({
    mutationFn: (chave: string) =>
      chamar<ResultadoSAP>(`/api/admin/configuracoes/sap/${chave}/testar`, { method: 'POST' }),
    onSuccess: (r) => {
      queryClient.invalidateQueries({ queryKey: ['configuracoes'] });
      if (r.ok) toast.success(r.mensagem);
      else toast.error(r.mensagem);
    },
    onError: (err: Error) => toast.error(err.message),
  });

  function salvarSAP() {
    const servicos: Record<string, { ativo: boolean; url_base: string; apikey?: string }> = {};
    for (const s of data?.sap.servicos ?? []) {
      const k = (apikeysSv[s.chave] ?? '').trim();
      servicos[s.chave] = { ativo: !!ativos[s.chave], url_base: (urls[s.chave] ?? '').trim(), ...(k ? { apikey: k } : {}) };
    }
    salvar.mutate({
      sap: {
        horario,
        usuario: usuario.trim(),
        ...(senha ? { senha } : {}),
        ...(apikey ? { apikey: apikey.trim() } : {}),
        sap_client: sapClient.trim(),
        servicos,
      },
    });
  }

  const temApikey = data.sap.apikey_definida || data.sap.apikey_ambiente;
  const temCredencial = temApikey || (data.sap.usuario !== '' && data.sap.senha_definida);

  return (
    <div className="mx-auto grid max-w-3xl gap-8 p-6">
      <h1 className="text-lg font-semibold text-foreground">Configurações</h1>

      <section aria-labelledby="cfg-sap" className="grid gap-5 rounded-lg border bg-card p-5">
        <div>
          <h2 id="cfg-sap" className="text-base font-semibold">
            Integração SAP
          </h2>
          <p className="text-xs text-muted-foreground">
            O sistema lê o SAP (somente leitura) uma vez por dia, no horário abaixo (horário de Recife). Hoje a leitura
            confere o acesso e conta os registros; a gravação nos cadastros entra depois que o mapeamento for definido.
          </p>
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <Label htmlFor="sap-horario">Horário da leitura diária</Label>
            <Input id="sap-horario" type="time" value={horario} onChange={(e) => setHorario(e.target.value)} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sap-usuario">Usuário do SAP</Label>
            <Input
              id="sap-usuario"
              autoComplete="off"
              value={usuario}
              onChange={(e) => setUsuario(e.target.value)}
              placeholder="usuário técnico"
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sap-senha">Senha do SAP</Label>
            <Input
              id="sap-senha"
              type="password"
              autoComplete="new-password"
              value={senha}
              onChange={(e) => setSenha(e.target.value)}
              placeholder={data.sap.senha_definida ? '•••••••• (definida)' : 'não definida'}
            />
          </div>
        </div>
        <p className="-mt-2 text-xs text-muted-foreground">
          A senha é gravada criptografada e nunca é mostrada de volta. Deixe o campo vazio para manter a atual.
          {data.sap.senha_definida && (
            <>
              {' '}
              <button
                type="button"
                className="underline underline-offset-2 hover:text-foreground"
                onClick={() => salvar.mutate({ sap: { limpar_senha: true, servicos: {} } })}
              >
                Remover senha
              </button>
            </>
          )}
        </p>

        <div className="grid gap-3 rounded-md border bg-muted/30 p-4">
          <div>
            <h3 className="text-sm font-semibold">Gateway SAP (Kong) — apikey</h3>
            <p className="text-xs text-muted-foreground">
              Se a apikey estiver preenchida, o sistema lê o SAP pelo gateway interno (URL <code>http://10.131.1.80:8000/sap/…</code>) e
              ignora o usuário e a senha acima. Sem apikey, usa o usuário e a senha direto no SAP.
            </p>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="grid gap-1.5 sm:col-span-2">
              <Label htmlFor="sap-apikey">Apikey do gateway</Label>
              <Input
                id="sap-apikey"
                type="password"
                autoComplete="new-password"
                value={apikey}
                onChange={(e) => setApikey(e.target.value)}
                placeholder={
                  data.sap.apikey_definida
                    ? '•••••••• (definida)'
                    : data.sap.apikey_ambiente
                      ? 'em uso a do servidor (variável de ambiente)'
                      : 'não definida'
                }
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="sap-client">Mandante (sap-client)</Label>
              <Input
                id="sap-client"
                inputMode="numeric"
                maxLength={3}
                value={sapClient}
                onChange={(e) => setSapClient(e.target.value)}
                placeholder="300"
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            Esta é a apikey <strong>geral</strong>: vale para todo serviço que não tiver a sua própria (campo dentro de cada serviço abaixo, para quando o
            Kong emitir uma chave por API). Gravada criptografada, nunca mostrada de volta. Deixe vazio para manter a atual.
            {data.sap.apikey_definida && (
              <>
                {' '}
                <button
                  type="button"
                  className="underline underline-offset-2 hover:text-foreground"
                  onClick={() => salvar.mutate({ sap: { limpar_apikey: true, servicos: {} } })}
                >
                  Remover apikey
                </button>
              </>
            )}
          </p>
        </div>

        <div className="grid gap-4">
          {data.sap.servicos.map((s) => (
            <div key={s.chave} className="grid gap-3 rounded-md border p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <h3 className="text-sm font-semibold">{s.nome}</h3>
                  <Badge variant={ativos[s.chave] ? 'default' : 'secondary'}>{ativos[s.chave] ? 'Ativo' : 'Desligado'}</Badge>
                </div>
                <div className="flex items-center gap-2">
                  <Label htmlFor={`sap-ativo-${s.chave}`} className="text-xs text-muted-foreground">
                    {s.diaria ? 'Ler todo dia' : 'Consultar na hora'}
                  </Label>
                  <Switch
                    id={`sap-ativo-${s.chave}`}
                    checked={!!ativos[s.chave]}
                    onCheckedChange={(v) => setAtivos((a) => ({ ...a, [s.chave]: v }))}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor={`sap-url-${s.chave}`}>URL do serviço</Label>
                <Input
                  id={`sap-url-${s.chave}`}
                  value={urls[s.chave] ?? ''}
                  onChange={(e) => setUrls((u) => ({ ...u, [s.chave]: e.target.value }))}
                  className="font-mono text-xs"
                />
                <p className="text-xs text-muted-foreground">
                  Direto no SAP: só https, em servidor de um domínio da Ferreira Costa. Pelo gateway (com apikey): http da rede interna,
                  ex.: <code>http://10.131.1.80:8000/sap/qas/{s.chave === 'cc' ? 'cost-center-read' : 'gl-account-read'}/</code>.{s.chave === 'bg' && ' Saldo orçamentário (OData V4): consulta direta no SAP com o usuário e a senha acima (ou apikey própria); mostra o saldo no formulário como aviso.'} Troque pela de
                  produção quando for liberada.
                </p>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor={`sap-apikey-${s.chave}`}>Apikey deste serviço (opcional)</Label>
                <Input
                  id={`sap-apikey-${s.chave}`}
                  type="password"
                  autoComplete="new-password"
                  value={apikeysSv[s.chave] ?? ''}
                  onChange={(e) => setApikeysSv((a) => ({ ...a, [s.chave]: e.target.value }))}
                  placeholder={s.apikey_definida ? '•••••••• (definida para este serviço)' : 'vazio = usa a apikey geral acima'}
                />
                {s.apikey_definida && (
                  <button
                    type="button"
                    className="w-fit text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
                    onClick={() => salvar.mutate({ sap: { servicos: { [s.chave]: { limpar_apikey: true } } } })}
                  >
                    Remover a apikey deste serviço (volta a usar a geral)
                  </button>
                )}
              </div>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <ResultadoLinha r={s.ultimo_resultado} ultima={s.ultima_execucao} />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => testar.mutate(s.chave)}
                  disabled={!temCredencial || testar.isPending}
                  title={temCredencial ? 'Faz uma leitura de 1 registro no SAP' : 'Salve a apikey (ou o usuário e a senha) antes de testar'}
                >
                  <PlugZap className="mr-1 size-3.5" />
                  {testar.isPending && testar.variables === s.chave ? 'Testando...' : 'Testar conexão'}
                </Button>
              </div>
            </div>
          ))}
        </div>

        <div>
          <Button size="sm" onClick={salvarSAP} disabled={salvar.isPending}>
            Salvar integração SAP
          </Button>
        </div>
      </section>
    </div>
  );
}
