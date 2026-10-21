import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { KeyRound, Eye, EyeOff, Copy } from 'lucide-react';

// ERP Bridge — Credenciais (réplica da tela equivalente do FB_APU02,
// adaptada ao padrão single-company do FB_APU05 — sem X-Company-ID, sem
// agendamento/histórico de runs, só a configuração que o daemon externo
// precisa para se conectar ao Oracle/SAP e consultar VW_COLABORADORES no
// servidor FCCORP). O backend nunca se conecta ao Oracle: só guarda as
// credenciais cifradas e as serve ao daemon via API key
// (GET /api/erp-bridge/credentials, handlers/erp_bridge.go).
interface BridgeConfig {
  erp_type: string;
  oracle_dsn: string;
  fbtax_email: string;
  fbtax_password_set: boolean;
  oracle_usuario: string;
  oracle_senha_set: boolean;
  api_key: string;
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  if (resp.status === 204) return undefined as T;
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(data?.error || `Erro HTTP ${resp.status}`);
  }
  return data as T;
}

const ERPBridgeCredenciais = () => {
  const queryClient = useQueryClient();

  const { data: cfg, isLoading, error } = useQuery({
    queryKey: ['erp-bridge-config'],
    queryFn: () => fetchJSON<BridgeConfig>('/api/erp-bridge/config'),
  });

  const [erpType, setErpType] = useState('sap_s4hana');
  const [oracleDsn, setOracleDsn] = useState('');
  const [fbtaxEmail, setFbtaxEmail] = useState('');
  const [fbtaxPassword, setFbtaxPassword] = useState('');
  const [oracleUsuario, setOracleUsuario] = useState('');
  const [oracleSenha, setOracleSenha] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [showFbtaxPwd, setShowFbtaxPwd] = useState(false);
  const [showOraclePwd, setShowOraclePwd] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);
  const [formInicializado, setFormInicializado] = useState(false);

  // Inicializa os campos editáveis a partir do GET assim que ele chega —
  // durante a renderização (padrão React "adjusting state during render"),
  // não num efeito: evita um re-render extra após o commit e um flash de
  // campos vazios antes do efeito disparar.
  if (cfg && !formInicializado) {
    setFormInicializado(true);
    setErpType(cfg.erp_type || 'sap_s4hana');
    setOracleDsn(cfg.oracle_dsn || '');
    setFbtaxEmail(cfg.fbtax_email || '');
    setOracleUsuario(cfg.oracle_usuario || '');
    setApiKey(cfg.api_key || '');
  }

  const salvarMutation = useMutation({
    mutationFn: () => {
      const body: Record<string, string> = { erp_type: erpType, oracle_dsn: oracleDsn };
      if (fbtaxEmail) body.fbtax_email = fbtaxEmail;
      if (fbtaxPassword) body.fbtax_password = fbtaxPassword;
      if (oracleUsuario) body.oracle_usuario = oracleUsuario;
      if (oracleSenha) body.oracle_senha = oracleSenha;
      return fetchJSON('/api/erp-bridge/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
    },
    onSuccess: () => {
      setFbtaxPassword('');
      setOracleSenha('');
      toast.success('Credenciais salvas com segurança.');
      queryClient.invalidateQueries({ queryKey: ['erp-bridge-config'] });
    },
    onError: (err: Error) => toast.error(`Erro ao salvar credenciais: ${err.message}`),
  });

  const gerarChaveMutation = useMutation({
    mutationFn: () =>
      fetchJSON<{ api_key: string }>('/api/erp-bridge/config/generate-api-key', { method: 'POST' }),
    onSuccess: (data) => {
      setApiKey(data.api_key);
      setShowApiKey(true);
      toast.success('Nova API key gerada. Copie e atualize o config.yaml no servidor do daemon.');
      queryClient.invalidateQueries({ queryKey: ['erp-bridge-config'] });
    },
    onError: (err: Error) => toast.error(`Erro ao gerar API key: ${err.message}`),
  });

  function handleGerarChave() {
    if (
      !apiKey ||
      window.confirm(
        'Confirmar: regenerar a API Key? A chave atual para de funcionar na hora — o ERP Bridge fica parado até o daemon ser atualizado com a nova chave.',
      )
    ) {
      gerarChaveMutation.mutate();
    }
  }

  if (isLoading) {
    return <p className="p-6 text-sm text-muted-foreground">Carregando...</p>;
  }

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6 p-6">
      <div>
        <h1 className="text-lg font-semibold text-foreground">ERP Bridge — Credenciais</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Credenciais armazenadas criptografadas (AES-256). Visível apenas para administradores.
        </p>
      </div>

      {error && <p className="text-sm text-destructive">{(error as Error).message}</p>}

      <div className="flex flex-col gap-5 rounded-md border border-border p-4">
        <p className="flex items-center gap-2 text-sm font-medium">
          <KeyRound className="h-4 w-4" /> Credenciais do Daemon Bridge
        </p>
        <p className="rounded bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          As senhas são criptografadas com AES-256 antes de serem salvas no banco. O daemon Bridge
          busca as credenciais automaticamente via API key — remova <code>email</code>,{' '}
          <code>password</code> e <code>senha</code> do <code>config.yaml</code> após configurar.
        </p>

        <div className="flex flex-col gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Tipo de ERP</p>
          <div className="flex flex-wrap gap-3">
            <div className="flex min-w-48 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">Modo de integração</Label>
              <Select value={erpType} onValueChange={setErpType}>
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="oracle_xml">Oracle XML (legado)</SelectItem>
                  <SelectItem value="sap_s4hana">Conexão SFC (FCCORP)</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex min-w-64 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">Oracle DSN (ex: host:1521/FCCORP)</Label>
              <Input
                value={oracleDsn}
                onChange={(e) => setOracleDsn(e.target.value)}
                className="font-mono"
                placeholder="10.131.1.118:1521/fccorp"
              />
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">FBTax</p>
          <div className="flex flex-wrap gap-3">
            <div className="flex min-w-48 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">E-mail</Label>
              <Input
                value={fbtaxEmail}
                onChange={(e) => setFbtaxEmail(e.target.value)}
                placeholder="usuario@empresa.com.br"
              />
            </div>
            <div className="flex min-w-48 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">
                Senha {cfg?.fbtax_password_set && <span className="ml-1 text-green-600">✓ configurada</span>}
              </Label>
              <div className="relative">
                <Input
                  type={showFbtaxPwd ? 'text' : 'password'}
                  value={fbtaxPassword}
                  onChange={(e) => setFbtaxPassword(e.target.value)}
                  className="pr-8"
                  placeholder={cfg?.fbtax_password_set ? '••••••••' : 'Nova senha'}
                />
                <button
                  type="button"
                  onClick={() => setShowFbtaxPwd((v) => !v)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {showFbtaxPwd ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Oracle ERP</p>
          <div className="flex flex-wrap gap-3">
            <div className="flex min-w-36 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">Usuário</Label>
              <Input value={oracleUsuario} onChange={(e) => setOracleUsuario(e.target.value)} placeholder="fcosta" />
            </div>
            <div className="flex min-w-36 flex-1 flex-col gap-1.5">
              <Label className="text-xs text-muted-foreground">
                Senha {cfg?.oracle_senha_set && <span className="ml-1 text-green-600">✓ configurada</span>}
              </Label>
              <div className="relative">
                <Input
                  type={showOraclePwd ? 'text' : 'password'}
                  value={oracleSenha}
                  onChange={(e) => setOracleSenha(e.target.value)}
                  className="pr-8"
                  placeholder={cfg?.oracle_senha_set ? '••••••••' : 'Nova senha'}
                />
                <button
                  type="button"
                  onClick={() => setShowOraclePwd((v) => !v)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {showOraclePwd ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">API Key do Daemon</p>
          <p className="text-xs text-muted-foreground">
            Adicione ao <code>config.yaml</code> como <code>api_key: "..."</code> na seção <code>fbtax</code>.
          </p>
          <div className="flex items-end gap-2">
            <div className="relative flex-1">
              <Input
                readOnly
                type={showApiKey ? 'text' : 'password'}
                value={apiKey}
                className="pr-16 font-mono"
                placeholder="Clique em Gerar para criar a chave"
              />
              <div className="absolute right-1 top-1/2 flex -translate-y-1/2 gap-1">
                <button
                  type="button"
                  onClick={() => setShowApiKey((v) => !v)}
                  className="p-1 text-muted-foreground hover:text-foreground"
                >
                  {showApiKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
                {apiKey && (
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard.writeText(apiKey);
                      toast.success('Chave copiada!');
                    }}
                    className="p-1 text-muted-foreground hover:text-foreground"
                    title="Copiar"
                  >
                    <Copy className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
            <Button size="sm" variant="outline" onClick={handleGerarChave} disabled={gerarChaveMutation.isPending}>
              {gerarChaveMutation.isPending ? 'Gerando...' : apiKey ? 'Regenerar' : 'Gerar chave'}
            </Button>
          </div>
        </div>

        <div className="flex justify-end border-t pt-3">
          <Button onClick={() => salvarMutation.mutate()} disabled={salvarMutation.isPending}>
            {salvarMutation.isPending ? 'Salvando...' : 'Salvar credenciais'}
          </Button>
        </div>
      </div>
    </div>
  );
};

export default ERPBridgeCredenciais;
