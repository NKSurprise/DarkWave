# DarkWave 🌊

A lightweight, native desktop chat app built in Go. Real-time messaging, encrypted DMs, friend system, and room management — without the bloat of Electron.

> Currently in active development. Audio channels coming in a future release.

---

## What it does

- Real-time messaging over TCP
- Multiple chat rooms — join, leave, create
- Friends system with real-time online/offline status
- Encrypted direct messages (AES-GCM, end-to-end)
- Password-protected accounts with Argon2 hashing
- Room member tracking — see who's in a room live
- Message persistence via PostgreSQL
- Native desktop UI built with Fyne (Windows, Mac, Linux)
- Accent color theming — red, blue, green, or purple
- Single-session enforcement — new login kicks the old session

---

## Project structure

```
DarkWave/
├── main.go        — entry point, wires everything together
├── server.go      — server lifecycle (start, stop, listen)
├── client.go      — client connection handling
├── room.go        — room management, member tracking, broadcasting
├── commands.go    — command parsing (/join, /nick, /dm, /leave, etc.)
├── dispatch.go    — message routing loop
├── models.go      — structs (Client, Server, Room, Message, Friend)
├── db.go          — Repo struct and database connection
├── store.go       — all database query methods
├── crypto.go      — Argon2 password hashing and verification
└── cmd/
    ├── client/    — legacy terminal client
    └── app/       — native desktop client (Fyne)
        ├── main.go    — app entry, login screen, theme picker
        ├── chat.go    — main chat UI (rooms, friends, DMs, members)
        ├── net.go     — TCP connection and message handling
        ├── crypto.go  — AES-GCM encryption for DMs
        └── theme.go   — custom DarkWave theme
```

---

## Getting started

### Prerequisites

- Go 1.21+
- PostgreSQL (or Supabase)
- GCC (required by Fyne for CGO)
  - **Windows**: install via [MSYS2](https://www.msys2.org/) — use the UCRT64 terminal
  - **Linux/Mac**: install via your package manager

### Server setup

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

### Client setup (desktop UI)

**Windows (MSYS2 UCRT64 terminal):**
```bash
cd /d/git/DarkWave
go run ./cmd/app/
```

**Linux/Mac:**
```bash
go run ./cmd/app/
```

### Terminal client (legacy)
```bash
go run ./cmd/client/
```

---

## Commands

| Command | Description |
|---|---|
| `/nick <n>` | Set your nickname and log in |
| `/join <room>` | Join or create a room |
| `/leave` | Leave the current room |
| `/dm <nick>` | Open an encrypted DM with a friend |
| `/rooms` | List your rooms |
| `/friends` | List your friends |
| `/addfriend <nick>` | Send a friend request |
| `/friendreqs` | View pending friend requests |
| `/acceptfriend <nick>` | Accept a friend request |
| `/declinefriend <nick>` | Decline a friend request |
| `/help` or `/?` | Show help |
| `/quit` | Disconnect |

---

## How DM encryption works

Direct messages are encrypted client-side before being sent. The server only ever sees ciphertext and cannot read DM content.

The shared key is derived from both usernames using SHA-256, sorted alphabetically so both sides always derive the same key independently. Messages are encrypted with AES-256-GCM, which provides both confidentiality and authenticity.

```
alice types "hey" 
→ key = SHA256("alice:bob")
→ encrypt with AES-GCM 
→ send ciphertext to server
→ server forwards ciphertext to bob
→ bob derives same key
→ decrypt → "hey"
```

---

## Roadmap

- [x] TCP chat server
- [x] Multiple rooms with persistent membership
- [x] Password-protected accounts (Argon2)
- [x] Friend system with real-time online/offline status
- [x] Message persistence
- [x] Room member tracking
- [x] Native desktop UI (Fyne)
- [x] Encrypted DMs (AES-GCM)
- [x] Custom accent color theming
- [ ] Audio channels (WebRTC via Pion)
- [ ] End-to-end encryption for rooms

---

## Tech stack

- **Go** — server and client
- **Fyne** — native desktop UI
- **PostgreSQL** — message and user persistence
- **pgx** — PostgreSQL driver
- **Argon2** — password hashing
- **AES-GCM** — DM encryption
- **godotenv** — environment variable loading

---

## License

MIT
