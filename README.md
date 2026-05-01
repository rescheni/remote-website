# Relay Tunnel

Lightweight reverse proxy tunnel for NAT traversal, inspired by frp.

- **relayd**: server (VPS), handles HTTP requests and routes to clients via WebSocket
- **relayc**: client (phone/desktop), one persistent WS connection, proxies requests to local services

## Features

- Domain subdomain routing (`app1.example.com` → client A)
- Path prefix routing (`example.com/service-b` → client B)
- WebSocket tunnel with auto-reconnect
- Embedded Vue3 dashboard

## Quick Start

### Server (Docker)

```bash
cp config.example.yaml config.yaml
# edit config.yaml with your ports
docker compose up -d
```

### Server (Binary)

```bash
./relayd -config config.yaml
```

### Client

```bash
cp client.example.yaml client.yaml
# edit client.yaml with your server address and routes
./relayc -config client.yaml
```

## Build

```bash
# All platforms
make all

# Just Go binaries
make build

# Just frontend
make web
```

## Config

See `config.example.yaml` and `client.example.yaml`.
