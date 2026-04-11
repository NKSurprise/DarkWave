# DarkWave 🌊

A lightweight native desktop chat app built in Go. Real-time messaging, encrypted DMs, peer-to-peer voice channels, and room management — without the bloat of Electron.

![DarkWave](Images/gemini-2.png)

---

## Features

- 💬 Real-time text messaging with room support
- 🔊 Voice channels with peer-to-peer audio (WebRTC)
- 🔇 Noise suppression (RNNoise) and voice activity detection
- 🟢 Speaking indicators — see who's talking in real time
- 🔐 Encrypted DMs — AES-256-GCM end-to-end, server never sees content
- 👥 Friend system with real-time online/offline status
- 🏠 Room member tracking
- 🎨 Custom accent color themes (Red, Blue, Green, Purple)
- 💾 Message persistence via PostgreSQL
- 🪶 Native desktop UI — ~50MB RAM vs Discord's 500MB+

---

## Download

Download the latest build from the [Actions tab](https://github.com/NKSurprise/DarkWave/actions) → latest successful run → Artifacts.

| Platform | File |
|---|---|
| Windows | `DarkWave-windows.zip` — extract and run `DarkWave.exe` |
| Linux | `DarkWave-linux` — make executable and run |
| Mac | `DarkWave-mac` — make executable and run |

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
- PostgreSQL
- GCC (for CGO)
  - **Windows**: [MSYS2](https://www.msys2.org/) UCRT64
    ```bash
    pacman -S mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-portaudio mingw-w64-ucrt-x86_64-opus mingw-w64-ucrt-x86_64-opusfile mingw-w64-ucrt-x86_64-rnnoise mingw-w64-ucrt-x86_64-pkg-config
    ```
  - **Linux (Ubuntu)**:
    ```bash
    sudo apt-get install gcc libportaudio2 portaudio19-dev libopus-dev libopusfile-dev pkg-config libgl1-mesa-dev
    # rnnoise from source:
    git clone https://github.com/xiph/rnnoise.git
    cd rnnoise && ./autogen.sh && ./configure && make && sudo make install
    ```
  - **Mac**:
    ```bash
    brew install portaudio opus opusfile pkg-config
    git clone https://github.com/xiph/rnnoise.git
    cd rnnoise && ./autogen.sh && ./configure && make && sudo make install
    ```

### Setup

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

4. Run the client (MSYS2 UCRT64 on Windows):
   ```bash
   go run ./cmd/app/
   ```

Port forward `:3000` and `:3001` on your router so friends can connect from outside your network.

---

## Commands

| Command | Description |
|---|---|
| `/nick <name>` | Set nickname and log in |
| `/join <room>` | Join or create a room |
| `/leave` | Leave current room |
| `/dm <nick>` | Open encrypted DM with a friend |
| `/createvoice <name>` | Create a voice channel (room creator only) |
| `/rooms` | List your rooms |
| `/friends` | List friends |
| `/addfriend <nick>` | Send a friend request |
| `/friendreqs` | View pending requests |
| `/acceptfriend <nick>` | Accept a friend request |
| `/declinefriend <nick>` | Decline a friend request |
| `/help` | Show help |
| `/quit` | Disconnect |

---

## How DM encryption works

Messages are encrypted client-side. The server only ever sees ciphertext and cannot read DM content.

```
alice types "hey"
→ key = SHA256("alice:bob")   ← sorted alphabetically, same for both sides
→ encrypt with AES-256-GCM
→ send ciphertext to server
→ server forwards to bob
→ bob derives same key independently
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
- [x] Encrypted DMs (AES-256-GCM)
- [x] Custom accent color theming
- [x] Voice channels (WebRTC peer-to-peer)
- [x] Noise suppression (RNNoise) + VAD
- [x] Speaking indicators
- [x] Audio device selection
- [x] Server address on login screen
- [x] GitHub Actions CI builds (Windows, Linux, Mac)
- [ ] End-to-end encryption for rooms
- [ ] Screen sharing (1080p60, hardware encoding)

---

## Tech stack

| | |
|---|---|
| Language | Go |
| UI | Fyne |
| Voice | Pion WebRTC |
| Audio | PortAudio + Opus |
| Noise suppression | RNNoise |
| Database | PostgreSQL + pgx |
| Password hashing | Argon2 |
| DM encryption | AES-256-GCM |
| Signalling | gorilla/websocket |

---

## License

MIT
