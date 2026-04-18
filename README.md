# DarkWave

A native desktop chat application written in Go. TCP-based server, end-to-end encrypted DMs, peer-to-peer voice channels with noise suppression, and real-time presence — packaged in a lightweight native UI.

![DarkWave](Images/gemini-2.png)

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Download](#download)
- [Self-Hosting](#self-hosting)
- [Client Setup](#client-setup)
- [Commands](#commands)
- [Encryption](#encryption)
- [Voice System](#voice-system)
- [Presence System](#presence-system)
- [Roadmap](#roadmap)
- [Tech Stack](#tech-stack)
- [License](#license)

---

## Features

- Real-time text messaging with persistent rooms
- Voice channels with peer-to-peer audio via WebRTC
- Noise suppression (RNNoise) and voice activity detection
- Speaking indicators — real-time visual feedback for who is talking
- Per-peer volume control with right-click popup slider
- End-to-end encrypted DMs — AES-256-GCM, server never sees plaintext
- Friend system with real-time online/offline status
- Room member list showing all members with live presence indicators (● online / ○ offline)
- Password-protected accounts using Argon2id
- Message history persistence via PostgreSQL
- Custom accent color themes (Red, Blue, Green, Purple)
- GitHub Actions CI builds for Windows, Linux, and macOS

---

## Architecture

```
┌─────────────┐        TCP :3000        ┌──────────────────┐
│  Client A   │ ──────────────────────▶ │                  │
│  (Fyne UI)  │ ◀────────────────────── │   Go TCP Server  │──── PostgreSQL
└─────────────┘                         │                  │
                                        └──────────────────┘
┌─────────────┐        TCP :3000              │  WS :3001
│  Client B   │ ──────────────────────▶       │
│  (Fyne UI)  │ ◀──────────────────────  ┌───▼───────────────┐
└─────────────┘                          │  Signal Server    │
                                         │  (WebSocket/HTTP) │
                     WebRTC P2P          └───────────────────┘
 Client A ◀─────────────────────────────────────▶ Client B
```

**Server (`cmd/server`)** — TCP server handling auth, rooms, commands, message dispatch, presence, and friend state. Stores messages and membership in PostgreSQL.

**Signal Server** — WebSocket server on `:3001` used for WebRTC signalling (offer/answer/ICE). Also exposes an HTTP `/members` endpoint polled by clients to display who is in each voice channel.

**Client (`cmd/app`)** — Fyne-based desktop UI. Maintains a single TCP connection to the server and a separate WebSocket connection per voice channel join.

---

## Download

Grab the latest release from [Releases](https://github.com/NKSurprise/DarkWave/releases).

| Platform | File |
|---|---|
| Windows | `DarkWave-windows.zip` — extract and run `DarkWave.exe` |
| Linux | `DarkWave-linux` — `chmod +x` and run |
| macOS | `DarkWave-mac` — `chmod +x` and run |

---

## Connecting

On the login screen enter:
- **Server address** — provided by your host (e.g. `85.130.x.x:3000`)
- **Nickname** — your username
- **Password** — created on first login

---

## Self-hosting

### Prerequisites

- Go 1.21+
- PostgreSQL 14+

### Database Setup

```bash
createdb darkwave
```

2. Create `.env` in the project root:
   ```
   DB_URL=postgres://user:password@localhost:5432/darkwave?sslmode=disable
   APP_PORT=:3000
   WS_PORT=:3001
   ```

### Running the Server

```bash
cd cmd/server
go run .
```

Port-forward `:3000` (TCP chat) and `:3001` (WebSocket signalling) on your router for external access.

---

## Client Setup

Download the release zip from [Releases](https://github.com/NKSurprise/DarkWave/releases), extract, and run. All native libraries (PortAudio, Opus, RNNoise) are bundled — nothing else to install.

On the login screen provide:
- **Server address** — e.g. `85.130.x.x:3000`
- **Nickname** — your username
- **Password** — set on first login, verified with Argon2id on subsequent logins

---

## Commands

All commands are typed in the message input box.

| Command | Description |
|---|---|
| `/nick <name>` | Set nickname and log in (prompts for password) |
| `/join <room>` | Join or create a room |
| `/leave` | Leave current room |
| `/dm <nick>` | Open an encrypted DM session with a friend |
| `/createvoice <name>` | Create a voice channel in the current room (room creator only) |
| `/voicechannels` | List voice channels in the current room |
| `/rooms` | List your rooms |
| `/friends` | List your friends |
| `/addfriend <nick>` | Send a friend request |
| `/friendreqs` | View pending friend requests |
| `/acceptfriend <nick>` | Accept a friend request |
| `/declinefriend <nick>` | Decline a friend request |
| `/online` | Refresh online status of friends |
| `/help` | Show help |
| `/quit` | Disconnect |

---

## Encryption

### DM Encryption

Messages are encrypted client-side before being sent. The server only ever forwards ciphertext.

```
Alice types "hey"
→ key = SHA-256("alice:bob")   ← nicks sorted alphabetically, both sides derive independently
→ plaintext encrypted with AES-256-GCM (random nonce per message)
→ ciphertext sent to server
→ server forwards ciphertext to Bob
→ Bob derives same key independently
→ AES-256-GCM decrypt → "hey"
```

The server has no access to the key and cannot read DM content.

### Room Messages

Room messages are currently plaintext on the wire (TCP). End-to-end encryption for rooms is on the roadmap.

---

## Voice System

Voice channels use a hybrid WebRTC + signalling server approach:

1. Client connects to the signal server over WebSocket at `ws://<host>:3001`
2. Signal server brokers WebRTC offer/answer/ICE candidate exchange between peers
3. Once ICE completes, audio flows directly peer-to-peer (no server relay for audio data)
4. Audio pipeline per peer: **PortAudio capture → RNNoise (noise suppression) → Opus encode → WebRTC send**
5. Receive pipeline: **WebRTC receive → Opus decode → RNNoise VAD → PortAudio playback**
6. VAD (voice activity detection) drives the speaking indicators in the UI
7. Per-peer volume is adjustable via right-click on a member in the voice panel

Audio device preferences (input/output) are saved per-user via `fyne.Preferences`.

---

## Presence System

The member panel shows all users who have access to a room, not just those currently in it.

- On room join the server queries `room_members` for all DB members and sends `** members: ...`
- It then sends `** online: ...` listing those currently connected to the server
- When any user connects or disconnects, the server pushes `** presence: <nick> online/offline` to every client currently sitting in a room that user has access to
- `** joined: <nick>` / `** left: <nick>` events track movement between rooms in real time

This means ● / ○ reflects **server-wide connectivity**, not just presence in the current room — matching the behaviour of Discord's member sidebar.

---

## Roadmap

- [x] TCP chat server with multiple rooms
- [x] Password-protected accounts (Argon2id)
- [x] Friend system with real-time online/offline status
- [x] Persistent room membership (PostgreSQL)
- [x] Message history persistence
- [x] Room member list with live presence indicators
- [x] Native desktop UI (Fyne)
- [x] Encrypted DMs (AES-256-GCM)
- [x] Custom accent color theming
- [x] Voice channels (WebRTC peer-to-peer)
- [x] Noise suppression (RNNoise) + VAD
- [x] Speaking indicators
- [x] Per-peer volume control
- [x] Audio device selection
- [x] GitHub Actions CI (Windows, Linux, macOS)
- [ ] End-to-end encryption for rooms
- [ ] Screen sharing (1080p60, hardware encoding)
- [ ] Message reactions
- [ ] File transfers

---

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go |
| UI framework | Fyne |
| Voice transport | Pion WebRTC |
| Audio I/O | PortAudio |
| Audio codec | Opus |
| Noise suppression | RNNoise |
| Database | PostgreSQL + pgx |
| Password hashing | Argon2id |
| DM encryption | AES-256-GCM |
| WebSocket signalling | gorilla/websocket |

---

## License

MIT
