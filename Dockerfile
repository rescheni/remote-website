FROM node:22-alpine AS frontend
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/cmd/relayd/web/dist ./cmd/relayd/web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /relayd ./cmd/relayd

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /relayd /usr/local/bin/relayd
EXPOSE 80 443 8443 7500
ENTRYPOINT ["relayd"]
CMD ["-config", "/etc/relayd/config.yaml"]
