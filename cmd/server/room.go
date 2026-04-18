package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// getOrCreateRoom fetches an existing room or creates a new one
// if roomID > 0, it sets the ID on a newly created room (fixes race condition)
func (s *Server) getOrCreateRoom(name string, roomID int64) *Room {
	if name == "" {
		name = "#main"
	}
	if !strings.HasPrefix(name, "#") && !strings.HasPrefix(name, "dm:") {
		name = "#" + name
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rooms[name]; ok {
		// If room ID was 0 and we now have one, update it
		if r.ID == 0 && roomID > 0 {
			r.ID = roomID
		}
		return r
	}
	r := &Room{
		ID:      roomID,
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
			var line string
			if strings.HasPrefix(msg.room.Name, "dm:") {
				line = fmt.Sprintf("[%s] (%s) %s",
					time.Now().Format("02/01 15:04"),
					msg.from.nick,
					strings.TrimSpace(string(msg.payload)))
			} else {
				line = fmt.Sprintf("[%s] (%s) %s",
					time.Now().Format("02/01 15:04"),
					msg.from.nick,
					strings.TrimSpace(string(msg.payload)))
			}
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
		s.broadcastToRoom(old, fmt.Sprintf("** left: %s", c.nick))
	}

	// broadcast join BEFORE adding c to room so c doesn't receive it
	s.broadcastToRoom(r, fmt.Sprintf("** joined: %s", c.nick))

	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
	c.activeRoom = r

	// Send all users who have access to this room (from DB)
	allMembers, err := s.repo.getRoomMembers(context.Background(), r.ID)
	if err != nil || len(allMembers) == 0 {
		// Fallback: just send currently connected nicks
		nicks := r.memberNicks()
		s.sendLine(c, "** members: %s", strings.Join(nicks, ", "))
	} else {
		s.sendLine(c, "** members: %s", strings.Join(allMembers, ", "))
		// Tell client which of those are currently online (connected to server)
		onlineSet := s.connectedNicksSet()
		var onlineNicks []string
		for _, nick := range allMembers {
			if onlineSet[nick] {
				onlineNicks = append(onlineNicks, nick)
			}
		}
		if len(onlineNicks) > 0 {
			s.sendLine(c, "** online: %s", strings.Join(onlineNicks, ", "))
		}
	}

	s.sendLine(c, "** joined %s", r.Name)
}

func (r *Room) memberNicks() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var nicks []string
	for c := range r.clients {
		nicks = append(nicks, c.nick)
	}
	return nicks
}

func (r *Room) memberNicksSet() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := make(map[string]bool, len(r.clients))
	for c := range r.clients {
		set[c.nick] = true
	}
	return set
}

func (s *Server) connectedNicksSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := make(map[string]bool, len(s.clientsByUser))
	for _, c := range s.clientsByUser {
		set[c.nick] = true
	}
	return set
}
