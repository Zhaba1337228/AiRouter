FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json ./
RUN npm install

# Build args (baked into the JS bundle)
ARG VITE_API_URL=http://localhost:8200
ARG VITE_ADMIN_PATH=admin
ARG VITE_APP_BASE=/admin

COPY . .

RUN echo "VITE_API_URL=${VITE_API_URL}" > .env && \
    echo "VITE_ADMIN_PATH=${VITE_ADMIN_PATH}" >> .env && \
    echo "VITE_APP_BASE=${VITE_APP_BASE}" >> .env && \
    npm run build

# ---

FROM nginx:1.27-alpine

COPY --from=builder /app/dist /usr/share/nginx/html

# nginx.conf is processed by the official image's envsubst entrypoint at
# CONTAINER STARTUP (not build time). This avoids any Docker build-cache
# issues when ADMIN_PATH changes — just `docker compose up -d` is enough.
COPY nginx.conf /etc/nginx/templates/default.conf.template

# Only substitute ${APP_BASE}; leave nginx variables like $uri untouched
ENV NGINX_ENVSUBST_FILTER=APP_BASE
ENV APP_BASE=/admin

EXPOSE 80
