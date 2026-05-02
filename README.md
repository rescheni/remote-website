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

### Server (relayd) — Docker

```bash
# Login to GitHub Container Registry (required for private repo)
echo $GITHUB_TOKEN | docker login ghcr.io -u rescheni --password-stdin

# Download files (private repo: use token; public repo: remove -H arg)
curl -H "Authorization: token $GITHUB_TOKEN" \
  -O https://raw.githubusercontent.com/rescheni/remote-website/main/docker-compose.yaml
curl -H "Authorization: token $GITHUB_TOKEN" \
  -O https://raw.githubusercontent.com/rescheni/remote-website/main/config.example.yaml

cp config.example.yaml config.yaml
# edit config.yaml with your domain and ports
docker compose up -d
```

### Server (relayd) — Binary

```bash
curl -L -o relayd https://github.com/rescheni/remote-website/releases/latest/download/relayd-linux-amd64
chmod +x relayd
curl -H "Authorization: token $GITHUB_TOKEN" \
  -O https://raw.githubusercontent.com/rescheni/remote-website/main/config.example.yaml
cp config.example.yaml config.yaml
./relayd -config config.yaml
```

### Client (relayc) — Binary

```bash
curl -L -o relayc https://github.com/rescheni/remote-website/releases/latest/download/relayc-linux-amd64
chmod +x relayc
curl -H "Authorization: token $GITHUB_TOKEN" \
  -O https://raw.githubusercontent.com/rescheni/remote-website/main/client.example.yaml
cp client.example.yaml client.yaml
# edit client.yaml with your server address
./relayc -config client.yaml
```

> **Private repo?** Create a token at [Settings > Personal access tokens](https://github.com/settings/tokens) with `read:packages` scope, then `export GITHUB_TOKEN=<your-token>`. Release binary downloads also need `gh auth login` or a token for private repos.

## Ports

| Port | Service | Description |
|------|---------|-------------|
| 80 | HTTP proxy | External users access your sites here |
| 443 | HTTPS proxy | External users access your sites here (TLS) |
| 1443 | Tunnel WS | relayc clients connect to this port |
| 17500 | Dashboard | Web UI for managing routes and clients |

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
