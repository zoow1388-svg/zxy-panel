FROM node:20-bookworm-slim AS builder
WORKDIR /app
ARG VITE_BASE_PATH=/
ENV VITE_BASE_PATH=${VITE_BASE_PATH}
ENV npm_config_registry=https://registry.npmmirror.com \
    npm_config_audit=false \
    npm_config_fund=false \
    npm_config_progress=false
COPY package.json ./
RUN npm cache clean --force && npm install --no-audit --no-fund --progress=false --registry=https://registry.npmmirror.com
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 5173
