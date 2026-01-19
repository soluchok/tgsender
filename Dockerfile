FROM node:20-alpine AS web-build

WORKDIR /web

COPY web/package*.json ./
RUN npm ci

COPY web/ .
RUN npm run build

FROM golang:alpine AS build

WORKDIR /app
COPY . .

RUN go build -o /app/tgsender ./main.go

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
LABEL org.opencontainers.image.source=https://github.com/soluchok/tgsender

COPY --from=build /app/tgsender /tgsender
COPY --from=web-build /web/dist /web/dist

ENTRYPOINT ["/tgsender", "serve", "--static-dir=/web/dist"]
