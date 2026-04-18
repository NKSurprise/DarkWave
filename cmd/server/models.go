package main

import (
	"net"
	"sync"
	"time"
)

type Client struct {
	UserID     int64
	conn       net.Conn
	out        chan string
	nick       string
	password   string
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
	room    *Room
	from    *Client
	payload []byte
}

type DBMessage struct {
	ID     int64
	RoomID int64
	FromID int64
	Body   string
	SentAt time.Time
}

type MessageWithSender struct {
	Body   string
	SentAt time.Time
	Nick   string
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
