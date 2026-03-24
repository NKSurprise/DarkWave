package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return // clean shutdown
			default:
				fmt.Println("accept:", err)
				close(s.quitch) // unexpected error, signal shutdown
				return
			}
		}

		host, port, _ := net.SplitHostPort(conn.RemoteAddr().String())
		_ = host
		c := &Client{
			conn: conn,
			out:  make(chan string, 32),
			nick: "guest-" + port,
		}

		go s.writeLoop(c)
		go s.readLoop(c)
	}
}

func (s *Server) readLoop(c *Client) {
	defer func() {
		s.unregisterClientSingle(c)
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

		out := append([]byte(nil), (text + "\n")...)

		s.msgch <- Message{room: c.activeRoom, from: c, payload: out}
	}
}

func (s *Server) writeLoop(c *Client) {
	for line := range c.out {
		if _, err := c.conn.Write([]byte(line + "\n")); err != nil {
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

// create-or-get client by nick

func (s *Server) readLoopPassword(c *Client) (string, error) {

	r := bufio.NewReader(c.conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return "** error: %s", err
	}

	passwordPlainText := cleanInput(line)
	for i := 0; 5 > len(passwordPlainText); i++ {
		// Simulate password input processing
		s.sendLine(c, "** password should be at least 5 characters")
		passwordPlainText, err = r.ReadString('\n')
		if err != nil {
			return "** error: %s", err
		}

	}

	return passwordPlainText, nil
}
