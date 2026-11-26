import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// Anexos com trocar e remover antes do envio (B2 de 26/11), usado no formulário de abertura e nos comentários (F12).
export interface Anexo {
  chave: number;
  nome: string;
  contentType: string;
  base64: string;
}


function fileParaBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const resultado = reader.result as string;
      resolve(resultado.split(',')[1] ?? '');
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export const EXTENSOES_ANEXO = /\.(pdf|jpe?g|png|docx?|xlsx?|msg|eml)$/i;
const ACCEPT_ANEXO = '.pdf,.jpg,.jpeg,.png,.doc,.docx,.xls,.xlsx,.msg,.eml';
const MAX_ANEXO_BYTES = 10 * 1024 * 1024;

export async function lerAnexo(file: File, chave: number): Promise<Anexo | null> {
  // Avisa já na escolha (o servidor confere de novo pelo conteúdo do arquivo).
  if (!EXTENSOES_ANEXO.test(file.name)) {
    toast.error(`"${file.name}" não é um tipo aceito. Aceitos: PDF, imagem (JPG/PNG), Word, Excel e e-mail (.msg ou .eml).`);
    return null;
  }
  if (file.size > MAX_ANEXO_BYTES) {
    toast.error(`"${file.name}" passa de 10 MB.`);
    return null;
  }
  return { chave, nome: file.name, contentType: file.type || 'application/octet-stream', base64: await fileParaBase64(file) };
}


// B2 (26/11): vários anexos; cada um pode ser removido ou trocado antes do envio. O envio leva só o que ficou.
export function CampoAnexos({ anexos, onChange, rotulo = 'Anexos', obrigatorio = true }: { anexos: Anexo[]; onChange: (a: Anexo[]) => void; rotulo?: string; obrigatorio?: boolean }) {
  const [chaveInput, setChaveInput] = useState(0);
  const proxima = useRef(1);
  const trocarInput = useRef<HTMLInputElement>(null);
  const [trocando, setTrocando] = useState<number | null>(null);

  async function adicionar(e: React.ChangeEvent<HTMLInputElement>) {
    const arquivos = Array.from(e.target.files ?? []);
    setChaveInput((k) => k + 1); // limpa o campo de arquivo: o nome fica só na lista abaixo
    const lidos = (await Promise.all(arquivos.map((f) => lerAnexo(f, proxima.current++)))).filter(Boolean) as Anexo[];
    if (lidos.length) onChange([...anexos, ...lidos]);
  }

  async function trocar(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f || trocando === null) return;
    const novo = await lerAnexo(f, proxima.current++);
    if (novo) onChange(anexos.map((a) => (a.chave === trocando ? novo : a)));
    setTrocando(null);
  }

  return (
    <div className="grid gap-1.5">
      <Label>
        {rotulo} {obrigatorio && <span className="text-destructive" aria-hidden>*</span>}
      </Label>
      <Input key={chaveInput} type="file" multiple accept={ACCEPT_ANEXO} onChange={adicionar} aria-label="Adicionar anexo" />
      <input ref={trocarInput} type="file" accept={ACCEPT_ANEXO} className="hidden" onChange={trocar} aria-hidden tabIndex={-1} />
      <p className="text-xs text-fg-helper">Aceitos: PDF, imagem (JPG/PNG), Word, Excel e e-mail (.msg ou .eml), até 10 MB cada.</p>
      {anexos.length > 0 && (
        <ul className="divide-y divide-border rounded-md border border-border">
          {anexos.map((a) => (
            <li key={a.chave} className="flex items-center gap-2 px-3 py-1.5 text-sm">
              <span className="min-w-0 flex-1 truncate" title={a.nome}>
                {a.nome}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setTrocando(a.chave);
                  trocarInput.current?.click();
                }}
              >
                Trocar
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={() => onChange(anexos.filter((x) => x.chave !== a.chave))}>
                Remover
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

