# Cocina Server

Self-hosted real-time chat backend for the Cocina collaboration platform. Built in Go with SQLite, WebSockets, organizations/workspaces/channels, an admin panel, and optional integration with Cocina Identity.

## Architecture Overview

```
┌──────────────┐     ┌──────────────────┐      ┌─────────────┐
│   Client     │────▶│  Cocina Server   │────▶│    SQLite   │
│  (Web/Mobile)│◀────│  (Go + WebSocket)│◀────│   Database  │
└──────────────┘     └──────────────────┘      └─────────────┘
        │                    │
        │                    ├── /admin (built-in panel)
        │                    └── optional ──▶ Cocina Identity (JWKS)
        ▼
   WebSocket Hub (presence, typing, live messages)
```

## Project Structure

```
servidor-local/
├── cmd/server/main.go       # Application entry point
├── config/                  # Environment configuration
├── types/                   # Data models and DTOs
├── database/                # SQLite layer and migrations
├── auth/                    # JWT, local auth, identity validation
├── org/                     # Organizations, workspaces, channels, invites
├── messaging/               # Messages, DMs, WebSocket hub
├── network/                 # Public URL / tunnel settings
├── handlers/                # HTTP API handlers and routing
├── admin/                   # Embedded admin UI (/admin)
├── identityclient/          # Cocina Identity server registration
├── version/                 # Server version constant
├── Dockerfile
├── docker-compose.yml       # Server-only compose (optional tunnel)
└── README.md
```

## Features

- User registration and login with JWT (local auth)
- Optional Cocina Identity integration (`local`, `identity`, or `dual` auth modes)
- Organizations, workspaces, channels, and direct messages (DMs)
- REST API for channel messages and legacy 1:1 messaging
- WebSocket hub for real-time messages, presence, and typing indicators
- Built-in admin panel at `/admin` (bootstrap owner, users/roles, invitations, network)
- Invite links with token preview, registration via `invite_token`, and accept flow
- Network settings (public API/WS URLs, frontend URL, health probe, Cloudflare tunnel)
- SQLite persistence, CORS, graceful shutdown, `/health` endpoint

## Getting Started

### Prerequisites

- Go 1.21+
- Docker (optional)

### Local Development

1. Clone the repository:
```bash
git clone https://github.com/montvlein/cocina-chat-local-server.git
cd cocina-chat-local-server
```

2. Install dependencies:
```bash
go mod download
```

3. Copy environment file and run:
```bash
cp .env.example .env
go run ./cmd/server/
```

The server starts at `http://localhost:8090`.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `COCINA_PORT` | `8090` | HTTP server port |
| `COCINA_DB_PATH` | `./data/cocina.db` | SQLite database path |
| `COCINA_SERVER_URL` | `http://localhost:{PORT}/api/v1` | Public API base URL |
| `COCINA_JWT_SECRET` | *(dev default)* | Secret for local JWT signing |
| `COCINA_AUTH_MODE` | `dual` if Identity URL set, else `local` | `local`, `identity`, or `dual` |
| `COCINA_IDENTITY_URL` | — | Base URL of Cocina Identity |
| `COCINA_IDENTITY_ISSUER` | same as `COCINA_IDENTITY_URL` | Expected JWT `iss` claim |
| `COCINA_IDENTITY_JWKS_URL` | `{IDENTITY_URL}/.well-known/jwks.json` | JWKS endpoint override |
| `COCINA_IDENTITY_API_KEY` | — | API key (`ck_...`) to link this server to an org |
| `COCINA_SETUP_TOKEN` | — | Required for remote `/admin` saves via tunnel (`X-Setup-Token`) |

See `.env.example` for a minimal local setup.

### Admin Panel (`/admin`)

Open **http://localhost:8090/admin**.

- On first run: create the **owner** administrator account.
- After setup: server status, users/roles, invitations, and network configuration.
- Admin API under `/api/v1/admin/*` (Bearer token from an admin/owner account).

From **localhost**, network settings can be saved without a setup token. From a public domain (e.g. Cloudflare Tunnel), set `COCINA_SETUP_TOKEN` in `.env` and send it as `X-Setup-Token` when saving.

### Docker

**This repo only** (API server):

```bash
docker compose up --build
```

**With Cloudflare Tunnel** (set `CLOUDFLARE_TUNNEL_TOKEN` in `.env`; map hostname → `http://cocina-server:8090`):

```bash
docker compose --profile tunnel up --build
```

**Full stack** (server + web client + optional tunnel/identity) from the monorepo root (`../`):

```bash
cd ..
docker compose up --build
# tunnel:  docker compose --profile tunnel up --build
# identity: docker compose --profile identity up --build
```

With tunnel on the full stack, map `api.*` → `http://cocina-server:8090` and `chat.*` → `http://cocina-web:80`.

### Identity Integration

When `COCINA_IDENTITY_URL` is set:

1. On startup, if `COCINA_IDENTITY_API_KEY` is set, the server registers via `POST /api/v1/server/register` on Identity and syncs the linked org locally (workspace + `#general` channel).
2. Identity JWTs are validated via JWKS (RS256).
3. Users are lazy-provisioned on first access (`global_identity_id = JWT.sub`).
4. In `dual` mode, local register/login still works for development.

Example `.env`:

```bash
COCINA_IDENTITY_URL=http://host.docker.internal:8080
COCINA_IDENTITY_ISSUER=http://localhost:8080
COCINA_IDENTITY_API_KEY=ck_your_key_from_identity_panel
COCINA_AUTH_MODE=dual
```

`COCINA_IDENTITY_ISSUER` must match the issuer in Identity JWT tokens.

## API Endpoints

### Health

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Liveness check |

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register user (`invite_token` optional) |
| POST | `/api/v1/auth/login` | Login and receive tokens |
| POST | `/api/v1/auth/refresh` | Refresh access token |
| POST | `/api/v1/auth/logout` | Invalidate session |

### Users

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/users/me` | Current user profile |
| PATCH | `/api/v1/users/me/presence` | Update presence status |

### Organizations & Channels

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/orgs` | List organizations for the current user |
| GET | `/api/v1/orgs/{orgId}/workspaces` | List workspaces in an org |
| GET | `/api/v1/workspaces/{workspaceId}/channels` | List channels in a workspace |
| POST | `/api/v1/workspaces/{workspaceId}/dms` | Create or get a DM channel |
| GET | `/api/v1/channels/{channelId}/messages` | Channel message history (`limit`, `before`/`cursor`) |
| POST | `/api/v1/channels/{channelId}/messages` | Send a message to a channel |

### Messages (legacy 1:1)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/messages/send` | Send message (`receiver_id` and/or `channel_id`) |
| GET | `/api/v1/messages/history` | Direct message history (`limit`, `before`) |
| GET | `/api/v1/conversations` | List DM conversations |

### Invitations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/invitations/{token}` | Preview invitation (public) |
| POST | `/api/v1/invitations/{token}/accept` | Accept invitation (authenticated) |

### Admin API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/admin/session` | Setup state and current admin session |
| POST | `/api/v1/admin/bootstrap` | Create first owner (only when no admin exists) |
| GET | `/api/v1/admin/status` | Server summary (admin) |
| GET | `/api/v1/admin/users` | List org members |
| PATCH | `/api/v1/admin/users/{userId}/role` | Update member role |
| GET | `/api/v1/admin/invitations` | List invitations |
| POST | `/api/v1/admin/invitations` | Create invitation |
| DELETE | `/api/v1/admin/invitations/{id}` | Revoke invitation |
| GET | `/api/v1/admin/network` | Get network settings |
| POST | `/api/v1/admin/network` | Save network settings |
| POST | `/api/v1/admin/network/test` | Probe reachability |

### WebSocket

| Protocol | Endpoint | Description |
|----------|----------|-------------|
| WS | `/ws` | Real-time messaging (Bearer token) |

### Admin UI

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin` | Embedded administration panel |

## API Examples

### Register

```bash
curl -X POST http://localhost:8090/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "username": "johndoe",
    "password": "securepassword123",
    "invite_token": "optional-invite-token"
  }'
```

### Send a Channel Message

```bash
curl -X POST http://localhost:8090/api/v1/channels/{channelId}/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
    "content": "Hello channel!",
    "content_type": "text"
  }'
```

### Get Channel History

```bash
curl "http://localhost:8090/api/v1/channels/{channelId}/messages?limit=50" \
  -H "Authorization: Bearer <access_token>"
```

## License

This project is licensed under the **Cocina Server Dual Use License** (custom;
not OSI-approved). See [LICENSE](LICENSE) for the full text.

**Free use (no license fee)** — You may use, modify, and deploy this software
when the service is offered free of charge to end users, or for personal,
educational, research, or non-profit use. Keep the copyright notice and license
in distributed source copies.

**Commercial use (paid services)** — If you offer a paid product or service
(SaaS, subscriptions, paid access, etc.), you must either:

- **Option A:** Display visible attribution to end users (*"Powered by
  [Cocina Server](https://github.com/montvlein/cocina-chat-local-server) by
  Fabricio Montivero"* in the UI and public docs), or
- **Option B:** Obtain a [commercial license](mailto:montvlein@gmail.com) in
  writing from the copyright holder.

Copyright (c) 2026 Fabricio Montivero. The software is provided "AS IS", without
warranty of any kind.
