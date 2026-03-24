package main

import (
	"fmt"
	"strings"
)

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

func (s *Server) roomBroadcastLoop(r *Room) {
	r.mu.RLock()
	fmt.Printf("BROADCAST %s to %d clients\n", r.Name, len(r.clients))
	r.mu.RUnlock()
	for msg := range r.Inbox {
		r.mu.RLock()
		clients := make([]*Client, 0, len(r.clients))
		for c := range r.clients {
			clients = append(clients, c)
		}
		r.mu.RUnlock() // release lock before sending
		for _, c := range clients {
			line := fmt.Sprintf("[%s] (%s) %s", msg.room.Name, msg.from.nick, strings.TrimSpace(string(msg.payload)))
			s.sendLine(c, "%s", line)
		}
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
