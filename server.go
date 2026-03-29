package main

import (
	"context"
	"fmt"
	"net"
)

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
		ID:      1,
		Name:    "#main",
		Inbox:   make(chan Message, 1024),
		clients: make(map[*Client]struct{}),
	}
	return s
}

func (s *Server) Start(ctx context.Context) error {
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
	go s.acceptLoop(ctx)

	<-ctx.Done()
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

func (s *Server) notifyFriendsOnline(c *Client) {
	ctx := context.Background()
	friends, err := s.repo.getFriendsByUserID(ctx, c.UserID)
	if err != nil {
		return
	}
	for _, f := range friends {
		if friend, ok := s.onlineByUserID(f.friendID); ok {
			// tell the friend that c is now online
			s.sendLine(friend, "** status: %s online", c.nick)
			// tell c which friends are already online
			s.sendLine(c, "** status: %s online", friend.nick)
		}
	}
}

func (s *Server) notifyFriendsOffline(c *Client) {
	ctx := context.Background()
	friends, err := s.repo.getFriendsByUserID(ctx, c.UserID)
	if err != nil {
		return
	}
	for _, f := range friends {
		if friend, ok := s.onlineByUserID(f.friendID); ok {
			s.sendLine(friend, "** status: %s offline", c.nick)
		}
	}
}

func (s *Server) broadcastToRoom(r *Room, msg string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		s.sendLine(c, msg)
	}
}
