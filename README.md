# DarkWave 🌊

A lightweight, encrypted TCP chat server and client written in Go. Connect to rooms, chat with others, and keep conversations private — only people with the right key can read what's being said.

> Currently in active development. UI and audio channels coming in future releases.

---

## What it does

- Real-time messaging over raw TCP
- Multiple chat rooms — create or join any room with `/join`
- Friend system — send, accept, and decline friend requests
- Password-protected accounts with Argon2 hashing
- Message persistence via PostgreSQL
- Single-session enforcement — logging in from a new location kicks the old session

---

## Project structure

```
DarkWave/
├── main.go        — entry point, wires everything together
├── server.go      — server lifecycle (start, stop, listen)
├── client.go      — client connection handling
├── room.go        — room management and broadcasting
├── commands.go    — command parsing and handling (/join, /nick, /friends, etc.)
├── dispatch.go    — message routing loop
├── models.go      — structs (Client, Server, Room, Message)
├── db.go          — Repo struct and database connection
├── store.go       — all database query methods
├── crypto.go      — Argon2 password hashing and verification
└── cmd/client/    — terminal client
```

---

## Getting started

### Prerequisites

- Go 1.21+
- PostgreSQL

### Setup

1. Create a PostgreSQL database:
   ```bash
   createdb darkwave
   ```

2. Create a `.env` file in the project root:
   ```
   DB_URL=postgres://user:password@localhost:5432/darkwave?sslmode=disable
   APP_PORT=:3000
   ```

3. Run the server:
   ```bash
   go run .
   ```

4. In a separate terminal, run the client:
   ```bash
   cd cmd/client
   go run .
   ```

`APP_PORT` defaults to `:3000` if not set.

---

## Commands

| Command | Description |
|---|---|
| `/nick <name>` | Set your nickname and log in |
| `/join <room>` | Join or create a room |
| `/rooms` | List available rooms |
| `/friends` | List your friends |
| `/addfriend <nick>` | Send a friend request |
| `/friendreqs` | View pending friend requests |
| `/acceptfriend <nick>` | Accept a friend request |
| `/declinefriend <nick>` | Decline a friend request |
| `/help` or `/?` | Show help |
| `/quit` | Disconnect |

---

## Roadmap

- [x] TCP chat server
- [x] Multiple rooms
- [x] Password-protected accounts (Argon2)
- [x] Friend system
- [x] Message persistence
- [x] Terminal client
- [x] Refactored server and client architecture
- [ ] Native desktop UI (Fyne)
- [ ] Audio channels (WebRTC via Pion)
- [ ] End-to-end message encryption (AES)

---

## Tech stack

- **Go** — server and client
- **PostgreSQL** — message and user persistence
- **pgx** — PostgreSQL driver
- **Argon2** — password hashing
- **godotenv** — environment variable loading

---

## License

MIT
