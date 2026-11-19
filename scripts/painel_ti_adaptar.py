#!/usr/bin/env python3
"""Adapta o "Painel de Despesas - TI.html" (Welton) ao sistema -> frontend/public/painel-ti/.

Uso:  python3 -I scripts/painel_ti_adaptar.py "caminho/Painel de Despesas - TI.html"

O painel do Welton é um HTML único (com leitor de Excel, dados embutidos e botões de sincronização). No sistema ele roda
num <iframe sandbox> sem rede: os dados e o perfil chegam por postMessage do sistema (ControleTI.tsx) e "Importar planilha"
pede ao sistema para abrir a aba de importação. Este script refaz essa adaptação a cada versão nova:
  - separa CSS / HTML / JS (o leitor de Excel e os dados embutidos ficam de fora);
  - troca a camada de sincronização por mensagens (fcx-pronto, fcx-modelo, fcx-eu, fcx-importar, fcx-enviar-resumo);
  - mantém a fonte Manrope embutida (as 3 primeiras linhas do painel.css atual);
  - acrescenta o botão "Enviar por e-mail" ao lado de "Exportar resumo (PDF)".
Depois: conferir com o smoke test (docs/referencia/04-CONTROLE-TI.md) e abrir o painel no navegador.
"""
import re, sys, os

if len(sys.argv) != 2:
    sys.exit(__doc__)
src = open(sys.argv[1], encoding="utf-8", errors="replace").read()
P = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "frontend", "public", "painel-ti")

scripts = [m.group(0) for m in re.finditer(r"<script[^>]*>.*?</script>", src, re.S)]
app = [s for s in scripts if "'use strict'" in s and "function loadModel" in s]
if len(app) != 1:
    sys.exit("não achei o script do painel (esperava 1, achei %d)" % len(app))
body = re.sub(r"<script[^>]*>.*?</script>", "<!--SCRIPT-->", src, flags=re.S)

# ---- CSS: fonte embutida do painel atual (3 primeiras linhas) + css novo
ours = open(os.path.join(P, "painel.css"), encoding="utf-8").read().split("\n")
css = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", body, re.S))
open(os.path.join(P, "painel.css"), "w", encoding="utf-8").write("\n".join(ours[:3]) + "\n" + css.lstrip("\n"))

# ---- HTML
h = re.sub(r"<style[^>]*>.*?</style>", '<link rel="stylesheet" href="painel.css">', body, flags=re.S)
h = h.replace('<meta name="fcx-api" content="">\n', "")
h = re.sub(r'<div class="drop" id="drop">.*?</div>\n', "", h, flags=re.S)
h = re.sub(r'\s*<input type="file"[^>]*>', "", h)
h = h.replace('<button class="btn primary" id="btnMain" title="Publica a planilha de despesas e sincroniza com o SAP">⟳ Publicar planilha e sincronizar SAP</button>',
              '<button class="btn primary" id="btnMain" title="Abre a aba Importar planilha do Controle de Despesas (Despesas TI, Orçamento TI, KSB1, KSB2 e FBL1N)">⟳ Importar planilha</button>')
h = re.sub(r'<button id="btnSrv"[^>]*>.*?</button>\s*', "", h, flags=re.S)
h = re.sub(r"(<!--SCRIPT-->\s*)+(<!--FCXDATA--><!--SCRIPT--><!--/FCXDATA-->\s*)?(<!--SCRIPT-->\s*)*", '<script src="painel.js"></script>\n', h)
i = h.index('<button class="btn primary" id="btnPdf"'); j = h.index("</button>", i) + len("</button>")
h = h[:j] + '\n<button class="btn" id="btnMail" title="Envia o PDF do resumo por e-mail (gere e salve o PDF antes, depois anexe o arquivo)">Enviar por e-mail</button>' + h[j:]

# ---- JS
s = app[0]
s = re.sub(r"^<script>", "", s); s = re.sub(r"</script>\s*$", "", s)
s = "\n".join(l for l in s.split("\n") if not l.startswith("const TEMPLATE = "))
head = s[: s.index("function showErr(msg)")]
j0 = s.index("/* ---------- fechamento automático do dia 6 ---------- */"); k0 = s.index("function repCard")
close = s[j0:k0].replace("if (need) $('#cbGo').onclick = ()=>$('#btnMain').click();", "if (need && $('#cbGo')) $('#cbGo').onclick = irImportar;")
head = head.replace("['#btnMain','#btnSrv','#closeBanner','#prevBanner'].forEach", "['#btnMain','#btnSrv','#closeBanner','#prevBanner','#btnMail'].forEach", 1)
adapter = r'''function showErr(msg){ const e=$('#err'); e.textContent = msg; e.style.display = msg?'block':'none'; if (msg) showOk(''); }
function showOk(msg){ const e=$('#okmsg'); if (!e) return; e.innerHTML = msg; e.style.display = msg?'block':'none'; }
const nrm = s => String(s||'').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g,'');
function setPrev(){}

// ---- Integração com o sistema (FB_APU05) ----------------------------------
// O painel roda num iframe isolado (sandbox, sem acesso à sessão do sistema): não lê arquivo nem
// chama API. O sistema (servidor) manda o modelo JÁ FILTRADO pelo perfil (o gestor só recebe os
// próprios CCs) e o perfil por mensagem; "Importar planilha" pede ao sistema para abrir a aba de importação.
const API = { url:'', hosted:true };
const irImportar = ()=> window.parent.postMessage({ type:'fcx-importar' }, '*');
['#btnLoad','#btnMain','#btnPlan','#btnSync','#btnFbl'].forEach(q=>{ const b=$(q); if (b) b.onclick = irImportar; });
// "Enviar por e-mail": o painel não fala com o servidor; avisa o sistema, que abre o diálogo de envio
// já com o mês e o gestor escolhidos aqui (o PDF é anexado lá, depois de salvo).
if ($('#btnMail')) $('#btnMail').onclick = ()=>{
  const D = IDX.D || compute(), mes = S.mes || (D.ymY.filter(m=>m<=M.meta.lastClosed).pop() || D.ymY[0]);
  const g = GESTORES.find(x=>x.k===S.gest);
  window.parent.postMessage({ type:'fcx-enviar-resumo', mes, mesRotulo: ymLabel(mes), gest: S.gest||'', gestNome: g ? g.nome : '', quem: g ? g.desc : (S.cc ? 'CC '+S.cc : 'TI') }, '*');
};
window.addEventListener('message', e=>{
  if (e.source !== window.parent) return;
  const d = e.data; if (!d) return;
  if (d.type === 'fcx-eu'){ setAcl(d.eu); if (M) render(); return; }
  if (d.type !== 'fcx-modelo' || !d.modelo) return;
  try{ showErr(''); loadModel(d.modelo); }
  catch(err){ console.error(err); showErr('Não consegui montar o painel: '+err.message); }
});
'''
js = head + adapter + close + "\nwindow.parent.postMessage({ type:'fcx-pronto' }, '*');\n})();\n"
for a, b in (("Publicar planilha e sincronizar SAP", "Importar planilha"), ("Publicar planilha e sincronizar", "Importar planilha")):
    js = js.replace(a, b); h = h.replace(a, b)
js = re.sub(r"clique em “Publicar planilha[^”]*”", "use “Importar planilha” (botão no topo do painel)", js)
open(os.path.join(P, "index.html"), "w", encoding="utf-8").write(h)
open(os.path.join(P, "painel.js"), "w", encoding="utf-8").write(js)
print("ok: painel.js %d bytes, index.html %d, painel.css atualizado" % (len(js), len(h)))
