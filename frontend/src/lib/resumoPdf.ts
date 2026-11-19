// Gera o PDF do "Resumo de despesas de TI" no navegador, a partir do HTML do relatório que o painel monta
// (o mesmo do "Exportar resumo (PDF)"). O painel roda isolado (iframe sandbox) e não consegue desenhar o PDF;
// aqui, no app, o HTML é renderizado num iframe SEM scripts (sandbox="allow-same-origin" apenas): mesmo que algum
// texto vindo da planilha tivesse HTML, nada executa. As bibliotecas só são baixadas quando o recurso é usado.

const LARGURA_PX = 703; // 186 mm (largura útil do A4 com margem de 12 mm) em pixels CSS

export interface ResumoPdf {
  blob: Blob;
  paginas: number;
}

export async function gerarPdfDoResumo(relHtml: string): Promise<ResumoPdf> {
  const [{ default: html2canvas }, { jsPDF }, css] = await Promise.all([
    import('html2canvas'),
    import('jspdf'),
    fetch('/painel-ti/painel.css').then((r) => (r.ok ? r.text() : '')),
  ]);

  const frame = document.createElement('iframe');
  frame.setAttribute('sandbox', 'allow-same-origin');
  frame.setAttribute('aria-hidden', 'true');
  frame.style.cssText = `position:fixed;left:-10000px;top:0;width:${LARGURA_PX}px;height:1200px;border:0`;
  frame.srcdoc =
    `<!doctype html><meta charset="utf-8"><style>${css}</style>` +
    `<style>html,body{margin:0;background:#fff}#rel{display:block!important;width:${LARGURA_PX}px}</style>` +
    `<div id="rel">${relHtml}</div>`;
  const carregou = new Promise<void>((ok) => {
    frame.onload = () => ok();
  });
  document.body.appendChild(frame);
  try {
    await carregou;
    const doc = frame.contentDocument;
    const rel = doc?.getElementById('rel');
    if (!doc || !rel) throw new Error('não foi possível montar o relatório');
    await new Promise((r) => setTimeout(r, 120));
    frame.style.height = `${rel.scrollHeight + 20}px`;

    const canvas = await html2canvas(rel, { scale: 2, backgroundColor: '#ffffff', logging: false, windowWidth: LARGURA_PX });
    const topo = rel.getBoundingClientRect().top;
    const escala = canvas.width / rel.offsetWidth;
    const y = (e: Element) => Math.round((e.getBoundingClientRect().top - topo) * escala);

    // pontos onde pode quebrar a página: início dos blocos, das linhas de tabela e dos itens agrupados
    const candidatos = [...new Set([...rel.querySelectorAll(':scope > *, tbody tr, .keep > *')].map(y))].sort((a, b) => a - b);
    // cada título leva junto o primeiro conteúdo depois dele (1ª linha da tabela, 1º cartão ou o gráfico): nunca fica título/cabeçalho sozinho no pé
    const conteudos = [...rel.querySelectorAll('tbody tr, .blk > .c, svg')];
    const titulos = [...rel.querySelectorAll('h2')].map((h) => {
      const base = h.getBoundingClientRect().bottom;
      const primeiro = conteudos.find((c) => c.getBoundingClientRect().top >= base - 1);
      const fimBloco = primeiro && primeiro.getBoundingClientRect().top - base < 160 ? primeiro.getBoundingClientRect().bottom : base + 90 / escala;
      return [y(h), Math.round((fimBloco - topo) * escala)] as const;
    });
    const alturaPagina = Math.floor((canvas.width * 271) / 186); // 271 mm de altura útil, na mesma proporção

    const paginas: Array<[number, number]> = [];
    let ini = 0;
    while (ini < canvas.height) {
      let fim = ini + alturaPagina;
      if (fim >= canvas.height) fim = canvas.height;
      else {
        const melhor = candidatos.filter((c) => c > ini + alturaPagina * 0.35 && c <= fim).pop();
        if (melhor) fim = melhor;
        // título sozinho no fim da página: leva junto para a próxima
        const orfao = titulos.find(([t, b]) => t > ini + alturaPagina * 0.2 && t < fim && b > fim);
        if (orfao) fim = orfao[0];
      }
      paginas.push([ini, fim]);
      ini = fim;
    }

    const pdf = new jsPDF({ unit: 'mm', format: 'a4', orientation: 'portrait', compress: true });
    paginas.forEach(([a, b], i) => {
      const h = b - a;
      const parte = document.createElement('canvas');
      parte.width = canvas.width;
      parte.height = h;
      const ctx = parte.getContext('2d');
      if (!ctx) throw new Error('canvas indisponível');
      ctx.fillStyle = '#fff';
      ctx.fillRect(0, 0, parte.width, h);
      ctx.drawImage(canvas, 0, a, canvas.width, h, 0, 0, canvas.width, h);
      if (i > 0) pdf.addPage();
      pdf.addImage(parte.toDataURL('image/jpeg', 0.92), 'JPEG', 12, 12, 186, (186 * h) / canvas.width);
    });
    return { blob: pdf.output('blob'), paginas: paginas.length };
  } finally {
    frame.remove();
  }
}
