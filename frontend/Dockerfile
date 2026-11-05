# syntax=docker/dockerfile:1
# Both Dockerfiles use the repository root as build context.
FROM node:24-alpine AS build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/index.html frontend/tsconfig.json frontend/vite.config.ts ./
COPY frontend/src ./src
# This URL is used by the HOST BROWSER, not by a container.
ARG VITE_API_BASE_URL=http://localhost:8080
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN npm run build

FROM nginx/nginx-unprivileged:stable-alpine AS runtime
COPY --from=build --chown=101:101 /app/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
USER 101:101
EXPOSE 8080
# Skip image template scripts: configuration is static and the filesystem is read-only.
ENTRYPOINT ["nginx", "-g", "daemon off;"]
