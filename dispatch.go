package main

import "fmt"

func (s *Server) dispatchLoop() {
	for m := range s.msgch {

		// go func(msg Message) {       <--- - if we wanted to save asynchronously (not blocking the dispatch loop)
		if err := s.repo.SaveMessage(m); err != nil {
			fmt.Println("failed to save message:", err)
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
