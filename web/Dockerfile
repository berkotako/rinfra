# RInfra web console (Next.js static export) served by nginx.
#
# The control-plane API base is baked in at build time via NEXT_PUBLIC_RINFRA_API
# (the browser talks to it directly), defaulting to the dockerised server on
# http://localhost:8080. Override with --build-arg or the compose build arg.

# ---- build ----
FROM node:20-alpine AS build
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

ARG NEXT_PUBLIC_RINFRA_API=http://localhost:8080
ENV NEXT_PUBLIC_RINFRA_API=$NEXT_PUBLIC_RINFRA_API
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ---- runtime ----
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/out /usr/share/nginx/html
EXPOSE 80
