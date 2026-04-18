package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type SignalMessage struct {
	Type    string `json:"type"`    // "join", "offer", "answer", "ice", "leave"
	From    string `json:"from"`    // nick of sender
	To      string `json:"to"`      // nick of target (empty = broadcast)
	Channel string `json:"channel"` // voice channel name
	Payload string `json:"payload"` // SDP or ICE candidate JSON
}

type VoiceClient struct {
	nick    string
	channel string
	conn    *websocket.Conn
	mu      sync.Mutex
}

type SignalServer struct {
	clients map[string]*VoiceClient // nick → client
	mu      sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewSignalServer() *SignalServer {
	return &SignalServer{
		clients: make(map[string]*VoiceClient),
	}
}

func (ss *SignalServer) Start(addr string) {
	http.HandleFunc("/ws", ss.handleWS)
	http.HandleFunc("/members", ss.handleMembers)
	fmt.Println("signal server listening on", addr)
	go http.ListenAndServe(addr, nil)
}

// handleMembers returns the nicks currently in a given voice channel.
// GET /members?channel=roomname/channelname
func (ss *SignalServer) handleMembers(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	ss.mu.RLock()
	var nicks []string
	for nick, vc := range ss.clients {
		if vc.channel == channel {
			nicks = append(nicks, nick)
		}
	}
	ss.mu.RUnlock()
	if nicks == nil {
		nicks = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nicks)
}

func (ss *SignalServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("ws upgrade error:", err)
		return
	}

	vc := &VoiceClient{conn: conn}

	defer func() {
		ss.removeClient(vc)
		conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg SignalMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "join":
			vc.nick = msg.From
			vc.channel = msg.Channel
			ss.mu.Lock()
			ss.clients[msg.From] = vc
			ss.mu.Unlock()

			// tell the joiner who's already there
			// use a different type so existing members DON'T initiate calls
			ss.mu.RLock()
			for nick, existing := range ss.clients {
				if nick != msg.From && existing.channel == msg.Channel {
					existingMsg := SignalMessage{
						Type:    "existing", // ← new type, not "join"
						From:    nick,
						Channel: msg.Channel,
					}
					data, _ := json.Marshal(existingMsg)
					vc.mu.Lock()
					vc.conn.WriteMessage(websocket.TextMessage, data)
					vc.mu.Unlock()
				}
			}
			ss.mu.RUnlock()

			// notify existing members that someone new joined
			ss.broadcastToChannel(msg.Channel, msg, msg.From)
		case "leave":
			ss.removeClient(vc)
			fmt.Printf("VOICE: %s left #%s\n", msg.From, msg.Channel)

		case "offer", "answer", "ice":
			fmt.Printf("VOICE RELAY: %s → %s type=%s\n", msg.From, msg.To, msg.Type)
			ss.sendTo(msg.To, msg)
		case "speaking":
			fmt.Printf("VOICE: %s is speaking in #%s\n", msg.From, msg.Channel)
			ss.broadcastToChannel(msg.Channel, msg, msg.From)
		}
	}
}

func (ss *SignalServer) sendTo(nick string, msg SignalMessage) {
	ss.mu.RLock()
	target, ok := ss.clients[nick]
	ss.mu.RUnlock()
	if !ok {
		return
	}
	data, _ := json.Marshal(msg)
	target.mu.Lock()
	target.conn.WriteMessage(websocket.TextMessage, data)
	target.mu.Unlock()
}

func (ss *SignalServer) broadcastToChannel(channel string, msg SignalMessage, excludeNick string) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	data, _ := json.Marshal(msg)
	count := 0
	for nick, vc := range ss.clients {
		if nick != excludeNick && vc.channel == channel {
			fmt.Printf("VOICE BROADCAST: notifying %s in %s\n", nick, channel)
			vc.mu.Lock()
			vc.conn.WriteMessage(websocket.TextMessage, data)
			vc.mu.Unlock()
			count++
		}
	}
	if count == 0 {
		fmt.Printf("VOICE BROADCAST: no peers in %s to notify\n", channel)
	}
}

func (ss *SignalServer) removeClient(vc *VoiceClient) {
	if vc.nick == "" {
		return
	}
	ss.mu.Lock()
	delete(ss.clients, vc.nick)
	ss.mu.Unlock()
	if vc.channel != "" {
		leave := SignalMessage{
			Type:    "leave",
			From:    vc.nick,
			Channel: vc.channel,
		}
		ss.broadcastToChannel(vc.channel, leave, vc.nick)
	}
}
