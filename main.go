package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type Client struct {
	UserID     int64
	conn       net.Conn
	out        chan Message
	nick       string
	activeRoom *Room
	friends    []Friend
	friendReqs []FriendRequest
	wmu        sync.Mutex
}

type FriendRequest struct {
	ID          int64
	fromNick    *Client
	toNick      *Client
	fromID      int64  // new
	toID        int64  // new
	status      string // pending, accepted, declined
	createdAt   time.Time
	respondedAt *time.Time
}

type Friend struct {
	friendID  int64   // new
	userID    int64   // new
	friend    *Client // old
	createdAt time.Time
}

type Room struct {
	ID      int64
	Name    string
	Inbox   chan Message
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

type Message struct {
	ID      int64
	room    *Room   //old
	from    *Client //old
	roomID  int64   //new
	fromID  int64   //new
	payload []byte
}

type Repo struct {
	pool *pgxpool.Pool
}

type Server struct {
	listenAddr string
	ln         net.Listener
	quitch     chan struct{}

	// rooms registry
	mu            sync.RWMutex
	rooms         map[string]*Room
	clientsByUser map[int64]*Client
	repo          *Repo

	// global inbox (roomed messages)
	msgch chan Message
}

func NewRepo(p *pgxpool.Pool) *Repo {
	return &Repo{pool: p}
}

func NewServer(addr string, repo *Repo) *Server {
	s := &Server{
		listenAddr:    addr,
		quitch:        make(chan struct{}),
		msgch:         make(chan Message, 1024),
		rooms:         make(map[string]*Room),
		clientsByUser: make(map[int64]*Client),
		repo:          repo,
	}
	// create #main with a bigger buffer (absorbs bursts)
	s.rooms["#main"] = &Room{
		Name:    "#main",
		Inbox:   make(chan Message, 1024),
		clients: make(map[*Client]struct{}),
	}
	return s
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()
	s.ln = ln
	fmt.Println("listening on", ln.Addr().String())

	// one broadcaster per room
	for _, r := range s.rooms {
		go s.roomBroadcastLoop(r)
	}
	// global dispatcher: takes Message (with room) and pushes into that room’s Inbox
	go s.dispatchLoop()

	// accept clients
	go s.acceptLoop()

	<-s.quitch
	return nil
}

func (s *Server) Stop() {
	select {
	case s.quitch <- struct{}{}:
	default:
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			fmt.Println("accept:", err)
			return
		}

		host, port, _ := net.SplitHostPort(conn.RemoteAddr().String())
		_ = host
		c := &Client{
			conn: conn,
			out:  make(chan Message, 32),
			nick: "guest-" + port,
		}

		mainRoom := s.getOrCreateRoom("#main")
		s.joinRoom(c, mainRoom)
		_ = s.sendLine(c, "** connected. type /help | /?")

		go s.writeLoop(c)
		go s.readLoop(c)
	}
}

func (s *Server) getOrCreateRoom(name string) *Room {
	if name == "" {
		name = "#main"
	}
	if !strings.HasPrefix(name, "#") && !strings.HasPrefix(name, "dm:") {
		name = "#" + name
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rooms[name]; ok {
		return r
	}
	r := &Room{
		Name:    name,
		Inbox:   make(chan Message, 256),
		clients: make(map[*Client]struct{}),
	}
	s.rooms[name] = r
	go s.roomBroadcastLoop(r)
	return r
}

func (s *Server) dispatchLoop() {
	for m := range s.msgch {
		// forward to the room’s inbox (copy is already done in readLoop)
		m.room.Inbox <- m
		// server-side log
		from := m.from.nick
		fmt.Printf("[%s] (%s) %s", m.room.Name, from, string(m.payload))
		fmt.Printf("DISPATCH -> %s\n", m.room.Name)
	}
}

func (s *Server) roomBroadcastLoop(r *Room) {
	fmt.Printf("BROADCAST %s to %d clients\n", r.Name, len(r.clients))
	for msg := range r.Inbox {
		// fan-out to clients in this room
		r.mu.RLock()
		for c := range r.clients {
			select {
			case c.out <- msg:
			default:
				// drop for this client to avoid stalling the room
				// (could count/log per client)
			}
		}
		r.mu.RUnlock()
	}
}

func (s *Server) joinRoom(c *Client, r *Room) {
	if c.activeRoom != nil {
		old := c.activeRoom
		old.mu.Lock()
		delete(old.clients, c)
		old.mu.Unlock()
	}
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
	c.activeRoom = r

	s.sendLine(c, "** joined %s", r.Name) // newline guaranteed
}

func (s *Server) listRooms() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.rooms))
	for n := range s.rooms {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func cleanInput(s string) string {
	s = strings.TrimRight(s, "\r\n")
	s = strings.TrimSpace(s)
	// strip BOM and common zero-width chars at the start
	s = strings.TrimPrefix(s, "\uFEFF") // BOM
	s = strings.TrimPrefix(s, "\u200B") // zero-width space
	s = strings.TrimPrefix(s, "\u200C") // ZWNJ
	s = strings.TrimPrefix(s, "\u200D") // ZWJ
	s = strings.TrimPrefix(s, "\u2060") // word joiner
	return s
}

func (s *Server) readLoop(c *Client) {
	defer func() {
		if c.activeRoom != nil {
			c.activeRoom.mu.Lock()
			delete(c.activeRoom.clients, c)
			c.activeRoom.mu.Unlock()
		}
		close(c.out)
		_ = c.conn.Close()
		fmt.Println("client disconnected:", c.nick)
	}()

	r := bufio.NewReader(c.conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		text := cleanInput(line)
		if text == "" {
			continue
		}

		if strings.HasPrefix(text, "/") {
			fmt.Println("DISPATCH CMD:", strconv.Quote(text)) // add: proves we hit command path
			s.handleCommand(c, text)
			continue
		}

		if c.UserID == 0 {
			s.sendLine(c, "** set your nick first with /nick <name>")
			continue
		}
		if c.activeRoom == nil {
			s.sendLine(c, "** join a room first with /join <room>")
			continue
		}

		body := text
		roomID := c.activeRoom.ID
		userID := c.UserID

		go func(roomId, userId int64, body string) {
			if s.repo != nil {
				return
			}

			if _, err := s.repo.AddMessage(context.Background(), roomID, userID, body); err != nil {
				fmt.Println("db add message error:", err)
			}

		}(roomID, userID, body)

		out := append([]byte(nil), (body + "\n")...)

		s.msgch <- Message{room: c.activeRoom, from: c, payload: out}
	}
}

func (s *Server) writeLoop(c *Client) {
	for m := range c.out {
		line := fmt.Sprintf("[%s] (%s) %s\n", m.room.Name, m.from.nick, string(m.payload))
		if _, err := c.conn.Write([]byte(line)); err != nil {
			return
		}
	}
}

func (s *Server) sendLine(c *Client, format string, args ...any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	// Optional: detect closed conn early
	// _ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

	n, err := fmt.Fprintf(c.conn, format+"\n", args...) // newline required
	if err != nil {
		fmt.Println("sendLine write error:", err)
		return err
	}
	fmt.Printf("sendLine wrote %d bytes: %q\n", n, fmt.Sprintf(format, args...))
	return nil
}

func (s *Server) safeWrite(c *Client, msg []byte) error {
	_, err := c.conn.Write(msg)
	return err
}

func (s *Server) registerClientSingle(c *Client) {
	s.mu.Lock()
	if old := s.clientsByUser[c.UserID]; old != nil && old != c {
		_ = s.sendLine(old, "** you were logged out (new login elsewhere)")
		_ = old.conn.Close() // triggers its cleanup defer
	}
	s.clientsByUser[c.UserID] = c
	s.mu.Unlock()
}

func (s *Server) unregisterClientSingle(c *Client) {
	s.mu.Lock()
	if s.clientsByUser[c.UserID] == c {
		delete(s.clientsByUser, c.UserID)
	}
	s.mu.Unlock()
}

func (s *Server) onlineByUserID(userID int64) (*Client, bool) {
	s.mu.RLock()
	cc, ok := s.clientsByUser[userID]
	s.mu.RUnlock()
	return cc, ok
}

func (r *Repo) UpsertClientByNick(ctx context.Context, nick string) (id int64, retNick string, err error) {
	err = r.pool.QueryRow(ctx, `
        insert into clients(nick) values ($1)
        on conflict(nick) do update set nick = excluded.nick
        returning id, nick
    `, nick).Scan(&id, &retNick)
	return
}

// create-or-get room by name
func (r *Repo) UpsertRoomByName(ctx context.Context, name string) (id int64, retName string, err error) {
	err = r.pool.QueryRow(ctx, `
        insert into rooms(name) values ($1)
        on conflict(name) do update set name = excluded.name
        returning id, name
    `, name).Scan(&id, &retName)
	return
}

// store a chat message
func (r *Repo) AddMessage(ctx context.Context, roomID, clientID int64, body string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
        insert into messages(room_id, client_id, body)
        values ($1,$2,$3)
        returning id
    `, roomID, clientID, body).Scan(&id)
	return id, err
}

func (r *Repo) declineFriendRequest(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		update friend_requests
		set status = 'declined', responded_at = now()
		where from_id = $1 and to_id = $2 and status = 'pending'
	`, fromID, toID)
	return err
}

func (r *Repo) addFriend(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		insert into friends(user_id, friend_id, created_at)
		values ($1, $2, now())
	`, fromID, toID)
	return err
}

func (r *Repo) acceptFriendRequest(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		update friend_requests
		set status = 'accepted', responded_at = now()
		where from_id = $1 and to_id = $2 and status = 'pending'
	`, fromID, toID)

	return err
}

func (r *Repo) getFriendsByUserID(ctx context.Context, userID int64) ([]Friend, error) {
	rows, err := r.pool.Query(ctx, `
		select friend_id, created_at from friends where user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []Friend
	for rows.Next() {
		var f Friend
		if err := rows.Scan(&f.friendID, &f.createdAt); err != nil {
			return nil, err
		}
		friends = append(friends, f)
	}
	return friends, nil
}

func (r *Repo) getFriendRequestsByUserID(ctx context.Context, userID int64) ([]FriendRequest, error) {
	rows, err := r.pool.Query(ctx, `
		select id, from_id, to_id, status, created_at, responded_at
		from friend_requests
		where to_id = $1 and status = 'pending'
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []FriendRequest
	for rows.Next() {
		var fr FriendRequest
		if err := rows.Scan(&fr.ID, &fr.fromID, &fr.toID, &fr.status, &fr.createdAt, &fr.respondedAt); err != nil {
			return nil, err
		}
		requests = append(requests, fr)
	}
	return requests, nil
}

func (r *Repo) sendFriendRequest(fromID, toID int64) error {
	_, err := r.pool.Exec(context.Background(), `
		insert into friend_requests(from_id, to_id, status, created_at)
		values ($1, $2, 'pending', now())
	`, fromID, toID)
	return err
}

func (r *Repo) findClientIDByNick(nick string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(context.Background(), `
		select id from clients where nick = $1
	`, nick).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *Repo) getClientByID(ctx context.Context, id int64) (*Client, error) {
	var nick string
	err := r.pool.QueryRow(ctx, `
		select nick from clients where id = $1
	`, id).Scan(&nick)
	if err != nil {
		return nil, err
	}
	return &Client{UserID: id, nick: nick}, nil
}

const helpText = "" +
	"** commands **\n" +
	"  /help | /?            show this help\n" +
	"  /nick <name>          set your nickname\n" +
	"  /rooms                list available rooms\n" +
	"  /join <room>          join or create a room (e.g., /join main)\n" +
	"  /friends              list your friends\n" +
	"  /addfriend <nick>     add a friend by nickname\n" +
	"  /friendreqs           list pending friend requests\n" +
	"  /acceptfriend <nick>  accept a friend request\n" +
	"  /declinefriend <nick> decline a friend request\n" +
	"  /quit                 disconnect\n"

func (s *Server) handleCommand(c *Client, cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	name := parts[0]

	fmt.Println("CMD NAME:", name)

	switch name {
	case "/help", "/?":
		if err := s.sendLine(c, helpText); err != nil {
			fmt.Println("sendLine /help:", err)
		}

	case "/rooms":
		names := s.listRooms()
		var out string
		if len(names) == 0 {
			out = "** rooms: (none)"
		} else {
			out = "** rooms: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, out); err != nil {
			fmt.Println("sendLine /rooms:", err)
		}
	case "/nick":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /nick <name>")
			return
		}
		id, retNick, err := s.repo.UpsertClientByNick(context.Background(), parts[1])
		if err != nil {
			s.sendLine(c, "** error setting nick: %v", err)
			return
		}

		old := c.nick
		c.nick = retNick
		c.UserID = id
		s.sendLine(c, "** nick: %s -> %s (#%d)", old, retNick, id)
	case "/join":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /join <room>")
			return
		}
		id, retName, err := s.repo.UpsertRoomByName(context.Background(), parts[1])
		if err != nil {
			s.sendLine(c, "** error: %v", err)
			return
		}

		room := s.getOrCreateRoom(retName)
		room.ID = id
		room.Name = retName
		s.joinRoom(c, room)
		s.sendLine(c, "** joined %s", room.Name)
	case "/friends":
		friends, err := s.repo.getFriendsByUserID(context.Background(), c.UserID)

		if err != nil {
			s.sendLine(c, "** error fetching friends: %v", err)
			return
		}
		var out string
		if len(friends) == 0 {
			out = "** friends: (none)"
		} else {
			var names []string
			for _, f := range friends {
				names = append(names, f.friend.nick)
			}
			out = "** friends: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, out); err != nil {
			fmt.Println("sendLine /friends:", err)
		}
	case "/addfriend":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /addfriend <nick>")
			return
		}
		nick := parts[1]

		toID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		s.repo.sendFriendRequest(c.UserID, toID)
		s.sendLine(c, "** friend request sent to: %s", nick)
	case "/friendreqs":

		reqs, err := s.repo.getFriendRequestsByUserID(context.Background(), c.UserID)
		if err != nil {
			s.sendLine(c, "** error fetching friend requests: %v", err)
			return
		}
		var out string
		if len(reqs) == 0 {
			out = "** friend requests: (none)"
		} else {
			var names []string
			for _, fr := range reqs {
				names = append(names, fr.fromNick.nick)
			}
			out = "** friend requests: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, out); err != nil {
			fmt.Println("sendLine /friendreqs:", err)
		}
	case "/acceptfriend":

		nick := parts[1]
		fromID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		if err := s.repo.acceptFriendRequest(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error accepting friend request from: %s", nick)
			return
		}
		if err := s.repo.addFriend(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error adding friend: %s", nick)
			return
		}
		s.sendLine(c, "** accepted friend request from: %s", nick)
	case "/declinefriend":
		nick := parts[1]
		fromID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		if err := s.repo.declineFriendRequest(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error declining friend request from: %s", nick)
			return
		}
		s.sendLine(c, "** declined friend request from: %s", nick)
	case "/quit":
		s.sendLine(c, "** bye")
		_ = c.conn.Close()

	default:
		s.sendLine(c, "** unknown command: %s (type /help)", name)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func main() {
	_ = godotenv.Load()

	dbURL := mustEnv("DB_URL")
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = ":3000"
	}

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatal("parse DB_URL:", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatal("connect:", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal("db ping failed:", err)
	}

	fmt.Println("DB connected ✅  listening on", port)

	repo := NewRepo(pool)

	s := NewServer(":3000", repo)
	log.Fatal(s.Start())
}
