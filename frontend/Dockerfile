FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json ./
RUN npm install

# Declare build args BEFORE COPY so Docker doesn't cache the build step
# when these values change
ARG VITE_API_URL=http://localhost:8200
ARG VITE_ADMIN_PATH=admin

COPY . .

RUN echo "VITE_API_URL=${VITE_API_URL}" > .env && \
    echo "VITE_ADMIN_PATH=${VITE_ADMIN_PATH}" >> .env && \
    npm run build

# ---

FROM nginx:1.27-alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
