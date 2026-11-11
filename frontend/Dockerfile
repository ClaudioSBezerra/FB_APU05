# FB_APU05 — Frontend produção (Coolify)
# Build estático (Vite) servido por nginx; /api é reverse-proxy para o
# container `api` da mesma rede do compose — o frontend só chama caminhos
# relativos (fetch('/api/...'), ver App.tsx/AuthContext.tsx), então web e
# api precisam ficar atrás do mesmo domínio.

FROM node:20-alpine AS builder
WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.27-alpine

# curl: só para o HEALTHCHECK (a imagem base não garante wget/curl).
# nginx sem root (Trivy DS-0002): pid em /tmp, cache gravável pelo usuário
# nginx e sem a diretiva `user` (só faz sentido com master root). A porta 80
# continua: o Docker libera portas baixas para não-root dentro do container
# (net.ipv4.ip_unprivileged_port_start=0), então o compose e a rota do túnel
# Cloudflare (http://web:80) não mudam.
RUN apk --no-cache add curl \
 && sed -i -e '/^user /d' -e 's#^pid .*#pid /tmp/nginx.pid;#' /etc/nginx/nginx.conf \
 && chown -R nginx:nginx /var/cache/nginx

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

USER nginx

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost/ || exit 1
