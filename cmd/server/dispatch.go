package main

import "fmt"

func (s *Server) dispatchLoop() {
	for m := range s.msgch {

		// go func(msg Message) {       <--- - if we wanted to save asynchronously (not blocking the dispatch loop)
		if err := s.repo.SaveMessage(m); err != nil {
			fmt.Printf("ERROR: failed to save message from %s to room %d: %v\n", m.from.nick, m.room.ID, err)
			fmt.Printf("  RoomID: %d, FromID: %d, Payload: %q\n", m.room.ID, m.from.UserID, string(m.payload))
		} else {
			fmt.Printf("✓ Message saved: room=%d, from=%s (ID:%d), len=%d\n", m.room.ID, m.from.nick, m.from.UserID, len(m.payload))
		}
		// }(m)

		// forward to the room’s inbox (copy is already done in readLoop)
		m.room.Inbox <- m

		// server-side log
		from := m.from.nick
		fmt.Printf("DISPATCH -> %s\n", m.room.Name)
		fmt.Printf("[%s] (%s) %s", m.room.Name, from, string(m.payload))
	}
}
