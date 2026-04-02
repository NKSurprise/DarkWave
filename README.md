# DarkWave 🌊

A lightweight, native desktop chat app built in Go. Real-time messaging, encrypted DMs, voice channels, and room management — without the bloat of Electron.

---

## What it does

- Real-time messaging over TCP
- Multiple chat rooms — join, leave, create
- Voice channels per room with peer-to-peer audio (WebRTC)
- Noise suppression (RNNoise) and voice activity detection
- Speaking indicators — see who's talking in real time
- Audio device selection (microphone and speaker)
- Friends system with real-time online/offline status
- Encrypted direct messages (AES-GCM, end-to-end)
- Password-protected accounts with Argon2 hashing
- Room member tracking
- Message persistence via PostgreSQL
- Native desktop UI built with Fyne (Windows, Mac, Linux)
- Accent color theming — red, blue, green, or purple
- Single-session enforcement

---

## Project structure

```
DarkWave/
├── main.go        — entry point
├── server.go      — server lifecycle
├── client.go      — client connection handling
├── room.go        — room management and broadcasting
├── commands.go    — command parsing
├── dispatch.go    — message routing
├── signal.go      — WebRTC signalling server (WebSocket :3001)
├── models.go      — structs
├── db.go          — database connection
├── store.go       — database queries
├── crypto.go      — Argon2 password hashing
└── cmd/
    ├── client/    — legacy terminal client
    └── app/       — native desktop client (Fyne)
        ├── main.go     — login screen, theme picker
        ├── chat.go     — main chat UI
        ├── voice.go    — WebRTC voice client
        ├── rnnoise.go  — noise suppression
        ├── settings.go — audio device settings
        ├── net.go      — TCP connection
        ├── crypto.go   — AES-GCM DM encryption
        └── theme.go    — custom theme
```

---

## Getting started

### Prerequisites

- Go 1.21+
- PostgreSQL (or Supabase)
- GCC — required by Fyne
  - **Windows**: install via [MSYS2](https://www.msys2.org/) UCRT64, then:
    ```bash
    pacman -S mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-portaudio mingw-w64-ucrt-x86_64-opus mingw-w64-ucrt-x86_64-opusfile mingw-w64-ucrt-x86_64-rnnoise
    ```
  - **Linux/Mac**: install via your package manager

### Server setup

1. Create a PostgreSQL database:
   ```bash
   createdb darkwave
   ```

2. Create `.env` in the project root:
   ```
   DB_URL=postgres://user:password@localhost:5432/darkwave?sslmode=disable
   APP_PORT=:3000
   WS_PORT=:3001
   ```

3. Run the server:
   ```bash
   go run .
   ```

### Client setup

**Windows (MSYS2 UCRT64 terminal):**
```bash
cd /d/git/DarkWave
go run ./cmd/app/
```

**Linux/Mac:**
```bash
go run ./cmd/app/
```

On the login screen enter the server address, your nickname and password.

---

## Commands

| Command | Description |
|---|---|
| `/nick <n>` | Set nickname and log in |
| `/join <room>` | Join or create a room |
| `/leave` | Leave current room |
| `/dm <nick>` | Open encrypted DM |
| `/createvoice <name>` | Create a voice channel (room creator only) |
| `/voicechannels` | List voice channels in current room |
| `/rooms` | List your rooms |
| `/friends` | List friends |
| `/addfriend <nick>` | Send friend request |
| `/friendreqs` | View pending requests |
| `/acceptfriend <nick>` | Accept friend request |
| `/declinefriend <nick>` | Decline friend request |
| `/help` or `/?` | Show help |
| `/quit` | Disconnect |

---

## Voice channels

Voice channels appear under each text room in the sidebar. Click a voice channel to join — audio is peer-to-peer via WebRTC, the server only handles signalling.

Features:
- Noise suppression via RNNoise
- Voice activity detection (only transmits when speaking)
- Speaking indicators in the voice panel
- Microphone and speaker device selection via Audio settings

---

## How DM encryption works

Messages are encrypted client-side with AES-256-GCM. The server only ever sees ciphertext.

```
alice types "hey"
→ key = SHA256("alice:bob")   (sorted, same for both sides)
→ encrypt with AES-GCM
→ send ciphertext to server
→ server forwards to bob
→ bob derives same key
→ decrypt → "hey"
```

---

## Roadmap

- [x] TCP chat server
- [x] Multiple rooms with persistent membership
- [x] Password-protected accounts (Argon2)
- [x] Friend system with online/offline status
- [x] Message persistence
- [x] Room member tracking
- [x] Native desktop UI (Fyne)
- [x] Encrypted DMs (AES-GCM)
- [x] Custom accent color theming
- [x] Voice channels (WebRTC peer-to-peer)
- [x] Noise suppression (RNNoise)
- [x] Speaking indicators
- [ ] End-to-end encryption for rooms
- [ ] Screen sharing (1080p60, hardware encoding)

---

## Tech stack

- **Go** — server and client
- **Fyne** — native desktop UI
- **Pion WebRTC** — peer-to-peer voice
- **PortAudio** — microphone and speaker
- **Opus** — audio codec
- **RNNoise** — noise suppression
- **PostgreSQL** — persistence
- **pgx** — PostgreSQL driver
- **Argon2** — password hashing
- **AES-GCM** — DM encryption
- **gorilla/websocket** — WebRTC signalling

---

## License

MIT
