# Cocina Server MVP

MVP backend for the Cocina real-time collaboration platform, built in Go with SQLite. This is a temporary solution designed to be migrated to PocketBase or a custom architecture in the future.

## Architecture Overview

```
┌──────────────┐     ┌──────────────────┐      ┌─────────────┐
│   Client     │────▶│  Cocina Server   │────▶│    SQLite   │
│  (Web/Mobile)│◀────│  (Go + WebSocket)│◀────│   Database  │
└──────────────┘     └──────────────────┘      └─────────────┘
                         │
                         ▼
                   ┌─────────────┐
                   │ WebSocket   │
                   │ Hub         │
                   └─────────────┘
```

## Project Structure

```
servidor-mvp/
├── cmd/server/main.go          # Application entry point
├── config/config.go           # Configuration management
├── types/types.go             # Data models and DTOs
├── database/database.go       # Database layer (SQLite)
├── auth/
│   ├── jwt.go                 # Token generation/validation
│   └── service.go             # Authentication logic
├── messaging/
│   ├── service.go             # Message CRUD operations
│   └── websocket.go           # WebSocket hub and real-time messaging
├── handlers/handlers.go       # HTTP request handlers
├── Dockerfile                 # Container configuration
└── README.md                  # This file
```

## Features (MVP)

- User registration and login with JWT authentication
- REST API for sending and retrieving messages
- WebSocket support for real-time messaging
- SQLite database for data persistence
- Graceful shutdown handling

## Getting Started

### Prerequisites

- Go 1.21+
- SQLite3 (for local development)
- Docker (optional, for containerized deployment)

### Local Development

1. Clone the repository:
```bash
git clone <repository-url>
cd PROYECTOS/Cocina/servidor-mvp
```

2. Install dependencies:
```bash
go mod download
```

3. Run the server:
```bash
go run ./cmd/server/
```

The server will start on `http://localhost:8090`

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| COCINA_PORT | 8090 | HTTP server port |
| COCINA_DB_PATH | ./data/cocina.db | SQLite database path |
| COCINA_JWT_SECRET | cocina-mvp-secret-key-change-in-production | JWT signing secret |

### Docker Deployment

Build and run with Docker:
```bash
docker build -t cocina-server .
docker run -p 8090:8090 -v $(pwd)/data:/data cocina-server
```

Or using docker-compose:
```yaml
version: '3.8'
services:
  cocina-server:
    build: .
    ports:
      - "8090:8090"
    volumes:
      - ./data:/data
    environment:
      - COCINA_PORT=8090
      - COCINA_JWT_SECRET=your-secret-key-here
```

## API Endpoints

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register a new user |
| POST | `/api/v1/auth/login` | Login and get tokens |
| POST | `/api/v1/auth/refresh` | Refresh access token |
| POST | `/api/v1/auth/logout` | Logout and invalidate session |

### Users

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/users/me` | Get current user profile |

### Messages

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/messages/send` | Send a message |
| GET | `/api/v1/messages/history?limit=50&before=msg_id` | Get message history |

### WebSocket

| Protocol | Endpoint | Description |
|----------|----------|-------------|
| WS | `ws://localhost:8090/ws` | Real-time messaging |

## API Examples

### Register a User

```bash
curl -X POST http://localhost:8090/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "username": "johndoe",
    "password": "securepassword123"
  }'
```

### Login

```bash
curl -X POST http://localhost:8090/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "securepassword123"
  }'
```

### Send a Message

```bash
curl -X POST http://localhost:8090/api/v1/messages/send \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
    "receiver_id": "user2_id",
    "content": "Hello!",
    "content_type": "text"
  }'
```

### Get Message History

```bash
curl http://localhost:8090/api/v1/messages/history?limit=50 \
  -H "Authorization: Bearer <access_token>"
```

## Future Migration to PocketBase

This MVP is designed with future migration in mind. The key considerations for migrating to PocketBase include:

### 1. Database Abstraction Layer

The current `database/` package provides a simple abstraction. When migrating to PocketBase, you can replace the SQLite implementation with PocketBase's embedded database while keeping the same interface.

### 2. User Management

PocketBase handles user authentication natively. The migration path involves:
- Removing custom auth service (`auth/service.go`)
- Using PocketBase's built-in auth endpoints
- Adapting token handling to use PocketBase's JWT system

### 3. Data Collections

When migrating, create PocketBase collections that match the current schema:
```json
{
  "name": "users",
  "schema": {
    "email": {"type": "email"},
    "username": {"type": "text"},
    "display_name": {"type": "text"}
  }
}
```

### 4. Real-time Features

PocketBase has built-in real-time subscriptions that can replace the custom WebSocket hub implementation.

## License

Internal use only - Cocina Project
