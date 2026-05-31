# Pockethost

Pockethost is a PocketBase-based master service for provisioning isolated
PocketBase tenant instances from a single binary.

The master process embeds PocketBase for its own control plane, serves the web
UI, and starts tenant PocketBase processes as isolated child processes. Each
tenant gets its own data directory under the configured data root.

## Features

- Master UI for authenticated tenant provisioning.
- Host-based tenant routing for `*.POCKETHOST_BASE_HOST`.
- Isolated tenant data directories.
- Single Go binary with the Vite frontend embedded at build time.
- Docker image suitable for self-hosted deployments.

## Requirements

- Go 1.26.1 or newer.
- Bun 1.3 or newer for frontend builds.
- Docker and Docker Compose for container deployments.

## Local Development

Build the frontend assets and run the PocketBase master:

```powershell
bun install --cwd web
bun run --cwd web build
go run . serve --http 127.0.0.1:8090
```

Open `http://127.0.0.1:8090` for the master UI.

Run the Go test suite with:

```powershell
go test ./...
```

## Docker

Build and run the local image with Compose:

```powershell
docker compose up -d --build
```

The published image is available from GitHub Container Registry:

```powershell
docker pull ghcr.io/gametec-live/pockethost:latest
```

Example Compose service using the published image:

```yaml
services:
  pockethost:
    image: ghcr.io/gametec-live/pockethost:latest
    restart: unless-stopped
    environment:
      POCKETHOST_BASE_HOST: pocketbase.example.com
      POCKETHOST_DATA_DIR: /pb_data
    ports:
      - "80:8090"
    volumes:
      - pockethost_data:/pb_data

volumes:
  pockethost_data:
```

Set `POCKETHOST_BASE_HOST` to the public master hostname. Tenant subdomains are
resolved below that host, for example `tenant-a.pocketbase.example.com`.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `POCKETHOST_BASE_HOST` | `pocketbase.example.com` | Base host for the master UI and tenant subdomains. |
| `POCKETHOST_DATA_DIR` | `./pb_data` | Master data directory. Tenant data is stored below `tenants/` inside this directory. |

The master serves the UI on the base host and proxies
`*.POCKETHOST_BASE_HOST` to the matching tenant process.

## Architecture

- The master is a Go binary embedding PocketBase and the Vite UI.
- The frontend uses the PocketBase SDK for auth and `pb.send()` for custom
  provisioning endpoints.
- Tenant databases run as direct child processes of the same binary through
  `os/exec`.
- Each tenant has an isolated data directory under
  `POCKETHOST_DATA_DIR/tenants`.
