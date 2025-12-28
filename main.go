package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Client struct {
	conn       net.Conn
	out        chan Message
	nick       string
	activeRoom *Room
	wmu        sync.Mutex
}

type Room struct {
	Name    string
	Inbox   chan Message
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

type Message struct {
	room    *Room
	from    *Client
	payload []byte
}

type Server struct {
	listenAddr string
	ln         net.Listener
	quitch     chan struct{}

	// rooms registry
	mu    sync.RWMutex
	rooms map[string]*Room

	// global inbox (roomed messages)
	msgch chan Message
}

func NewServer(addr string) *Server {
	s := &Server{
		listenAddr: addr,
		quitch:     make(chan struct{}),
		msgch:      make(chan Message, 1024),
		rooms:      make(map[string]*Room),
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
		_ = s.sendLine(c, "** connected. type /help")

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

		if !strings.HasPrefix(text, "/") {
			// copy payload so the buffer isn't reused
			payload := append([]byte(nil), []byte(text)...)
			s.msgch <- Message{room: c.activeRoom, from: c, payload: payload}
			continue
		}

		// normal chat: keep \n so PS ReadLine() prints
		payload := append([]byte(nil), []byte(text)...)
		s.msgch <- Message{room: c.activeRoom, from: c, payload: payload}

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

const helpText = "" +
	"** commands **\n" +
	"  /help                 show this help\n" +
	"  /nick <name>          set your nickname\n" +
	"  /rooms                list available rooms\n" +
	"  /join <room>          join or create a room (e.g., /join main)\n" +
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
		old := c.nick
		c.nick = parts[1]

		if err := s.sendLine(c, fmt.Sprintf("** nick: %s -> %s", old, c.nick)); err != nil {
			fmt.Println("sendLine /nick:", err)
		}

	case "/join":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /join <room>")
			return
		}
		room := s.getOrCreateRoom(parts[1])
		s.joinRoom(c, room)
		s.sendLine(c, "** joined %s", room.Name)

	case "/quit":
		s.sendLine(c, "** bye")
		_ = c.conn.Close()

	default:
		s.sendLine(c, "** unknown command: %s (type /help)", name)
	}
}

func main() {
	s := NewServer(":3000")
	log.Fatal(s.Start())
}
