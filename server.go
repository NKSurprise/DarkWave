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
