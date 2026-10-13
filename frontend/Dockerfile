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

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://localhost/ || exit 1
