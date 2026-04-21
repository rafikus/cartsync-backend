# CartSync — Backend

![Health Status](https://kusy.net/cartsync/health)

Go + PostgreSQL REST API with WebSocket sync for the CartSync shopping list app.

## Stack

- **Go 1.22** — stdlib `net/http` router, no framework
- **PostgreSQL 16** — via `pgx/v5`
- **WebSockets** — `gorilla/websocket`
- **JWT** — `golang-jwt/jwt/v5`
- **bcrypt** — `golang.org/x/crypto`

---

## Quick start (Docker)

```bash
cp .env.example .env          # edit JWT_SECRET at minimum
go mod tidy
docker compose up --build
```

Server runs on `http://localhost:3000`.

---

## Quick start (local)

**Prerequisites:** Go 1.22+, PostgreSQL 16+

```bash
# 1. Create the database
psql -U postgres -c "CREATE DATABASE cartsync;"

# 2. Configure environment
cp .env.example .env
# Edit .env with your DB credentials and JWT secret

# 3. Install dependencies
go mod tidy

# 4. Run
go run ./cmd/server
```

Migrations run automatically on startup.

---

## API reference

All protected endpoints require `Authorization: Bearer <token>`.

### Auth

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/auth/register` | `{email, password, name}` | Create account |
| POST | `/auth/login` | `{email, password}` | Sign in, returns token |
| GET  | `/auth/me` | — | Current user + their `listIds` |

### Lists

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/lists` | `{name}` | Create a new list |
| POST | `/lists/join` | `{inviteCode}` | Join by invite code |
| GET  | `/lists/:id` | — | Fetch list with members + items |
| PATCH | `/lists/:id` | `{name}` | Rename list |

### Items

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/lists/:id/items` | `{name, quantity, unit}` | Add item |
| PATCH | `/lists/:id/items/:itemId` | `{name?, quantity?, unit?, checked?}` | Update item |
| DELETE | `/lists/:id/items/:itemId` | — | Remove item |

### Stores

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/lists/:id/stores` | — | List saved stores |
| POST | `/lists/:id/stores` | `{name}` | Add store |
| POST | `/lists/:id/stores/:storeId/order` | `{order: string[]}` | Record check order for aisle learning |

### Trips

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/lists/:id/trips` | — | Trip history |
| POST | `/lists/:id/trips` | `{storeId?}` | Complete current shop, clears checked items |
| POST | `/lists/:id/trips/:tripId/receipt` | `multipart: receipt` | Attach receipt image |

### Suggestions

| Method | Path | Description |
|--------|------|-------------|
| GET | `/lists/:id/suggestions` | Items from history not currently on list |

---

## WebSocket

```
ws://localhost:3000/ws/lists/:listId?token=<jwt>
```

Connect after loading the list. The server broadcasts JSON messages to all members:

```ts
type WsMessageType =
  | "item:added"       // payload: Item
  | "item:updated"     // payload: Item
  | "item:removed"     // payload: { id: string }
  | "item:checking"    // payload: { itemId: string }  — partner is checking
  | "list:updated"     // payload: Partial<List>
  | "store:updated"    // payload: Store
  | "trip:completed"   // payload: Trip
  | "ping" | "pong"
```

The client can send `item:checking` to signal to the partner that an item is being checked (for the live highlight). The server forwards it to other members.

---

## Project structure

```
cartsync-backend/
├── cmd/server/main.go          HTTP server, router, migration runner
├── internal/
│   ├── db/
│   │   ├── db.go               pgxpool connection
│   │   └── models.go           shared structs
│   ├── auth/
│   │   ├── jwt.go              issue + parse JWT
│   │   └── handler.go          /auth/* handlers
│   ├── middleware/
│   │   └── auth.go             JWT middleware, CORS
│   ├── ws/
│   │   ├── hub.go              connection registry + broadcast
│   │   └── handler.go          WebSocket upgrade
│   ├── lists/
│   │   ├── handler.go          list CRUD
│   │   └── items.go            item CRUD
│   ├── stores/
│   │   └── handler.go          store management + order learning
│   ├── trips/
│   │   └── handler.go          trip completion + receipt upload
│   └── suggestions/
│       └── handler.go          frequency-based suggestions
└── migrations/
    └── 001_init.sql            full schema
```

---

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `cartsync` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode (`disable` / `require`) |
| `JWT_SECRET` | `change-me-in-production` | Token signing secret |
| `PORT` | `3000` | HTTP port |
| `UPLOAD_DIR` | `./uploads` | Receipt storage directory |

---

## Production notes

- **Receipts** are saved to `UPLOAD_DIR` on disk. For production swap `trips/handler.go:AttachReceipt` with an S3/GCS upload and store the URL.
- **CORS** in `middleware/auth.go` allows all origins. Restrict `Access-Control-Allow-Origin` to your app's domain.
- **JWT_SECRET** must be a long random string. Generate one with `openssl rand -base64 48`.
- **WebSocket ping/pong** runs every 25s on the client side. Configure a matching read deadline on the server if deploying behind a load balancer with short idle timeouts.
