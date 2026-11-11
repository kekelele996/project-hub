FROM node:22-alpine AS build
WORKDIR /app
COPY package*.json ./
# Lockfile resolves to registry.npmmirror.com; keep audit off the default registry.
RUN npm config set registry https://registry.npmmirror.com \
    && npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=20s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1/ || exit 1
