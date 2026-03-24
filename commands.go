package main

import (
	"context"
	argon2stuff "darkwave/passwordHandling"
	"fmt"
	"strings"
)

const helpText = "" +
	"** commands **\n" +
	"  /help | /?            show this help\n" +
	"  /nick <name>          set your nickname\n" +
	"  /rooms                list available rooms\n" +
	"  /join <room>          join or create a room (e.g., /join main)\n" +
	"  /friends              list your friends\n" +
	"  /addfriend <nick>     add a friend by nickname\n" +
	"  /friendreqs           list pending friend requests\n" +
	"  /acceptfriend <nick>  accept a friend request\n" +
	"  /declinefriend <nick> decline a friend request\n" +
	"  /quit                 disconnect\n"

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
		names, err := s.repo.listRooms()
		if err != nil {
			s.sendLine(c, "** error: %v", err)
			return
		}
		var out string
		if len(names) == 0 {
			out = "** rooms: (none)"
		} else {
			out = "** rooms: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, "%s", out); err != nil {
			fmt.Println("sendLine /rooms:", err)
		}
	case "/nick":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /nick <name>")
			return
		}
		id, retNick, err := s.repo.UpsertClientByNick(context.Background(), parts[1])
		if err != nil {
			s.sendLine(c, "** error setting nick: %v", err)
			return
		}

		user, err := s.repo.getClientByID(context.Background(), id)
		if err != nil {
			s.sendLine(c, "** error fetching client: %v", err)
			return
		}

		//input password
		passwordPlainText, err := s.readLoopPassword(c)
		if err != nil {
			s.sendLine(c, "** error reading password: %s", err)
			return
		}

		//check if client has password already
		userHasPassword, err := s.repo.checkClientHasPassword(context.Background(), id)
		if err != nil {
			s.sendLine(c, "** error checking password: %s", err)
			return
		}

		//has a password already
		if !userHasPassword {
			user.nick = retNick
			user.UserID = id
			c.nick = retNick
			c.UserID = id

			//hashing plain text password
			password, err := argon2stuff.HashPassword(passwordPlainText, (*argon2stuff.Argon2Params)(param))
			if err != nil {
				s.sendLine(c, "** error hashing password: %s", err)
				return
			}

			if err := s.repo.SavePassword(password, id); err != nil {
				s.sendLine(c, "** error saving password: %v", err)
				return
			}
		} else {
			c.nick = user.nick
			c.UserID = user.UserID
			dbPassword, err := s.repo.GetPasswordById(id)
			if err != nil {
				s.sendLine(c, "** error fetching stored password: %s", err)
				return
			}

			for {
				match, err := VerifyPassword(passwordPlainText, dbPassword)
				if err != nil {
					s.sendLine(c, "** error verifying password: %s", err)
					return
				}
				if match {
					break
				}
				s.sendLine(c, "** please re-enter password for %s:", retNick)
				passwordPlainText, err = s.readLoopPassword(c)
				if err != nil {
					s.sendLine(c, "** error reading password: %s", err)
					return
				}
			}
		}

		s.sendLine(c, "** welcome, %s!", retNick)
		s.registerClientSingle(c)

		mainRoom := s.getOrCreateRoom("#main")
		s.joinRoom(c, mainRoom)
		_ = s.sendLine(c, "** connected. type /help | /?")

		// load recent messages
		msgs, err := s.repo.GetRecentMessagesWithUsers(mainRoom.ID)
		if err != nil {
			s.sendLine(c, "** error fetching recent messages: %v", err)
		} else {
			for _, msg := range msgs {
				s.sendLine(c, "[%s] %s: %s", msg.SentAt.Format("15:04"), msg.Nick, msg.Body)
			}
		}

	case "/join":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /join <room>")
			return
		}
		id, retName, err := s.repo.UpsertRoomByName(context.Background(), parts[1])
		if err != nil {
			s.sendLine(c, "** error: %v", err)
			return
		}

		room := s.getOrCreateRoom(retName)
		room.ID = id
		room.Name = retName
		s.joinRoom(c, room)
		// load recent messages
		msgs, err := s.repo.GetRecentMessagesWithUsers(room.ID)
		if err != nil {
			s.sendLine(c, "** error fetching recent messages: %v", err)
		} else {
			for _, msg := range msgs {
				s.sendLine(c, "[%s] %s: %s", msg.SentAt.Format("15:04"), msg.Nick, msg.Body)
			}
		}
	case "/friends":
		friends, err := s.repo.getFriendsByUserID(context.Background(), c.UserID)

		if err != nil {
			s.sendLine(c, "** error fetching friends: %v", err)
			return
		}
		var out string
		if len(friends) == 0 {
			out = "** friends: (none)"
		} else {
			var names []string
			for _, f := range friends {
				names = append(names, f.friend.nick)
			}
			out = "** friends: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, "%s", out); err != nil {
			fmt.Println("sendLine /friends:", err)
		}
	case "/addfriend":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /addfriend <nick>")
			return
		}
		nick := parts[1]

		toID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		s.repo.sendFriendRequest(c.UserID, toID)
		s.sendLine(c, "** friend request sent to: %s", nick)
	case "/friendreqs":

		reqs, err := s.repo.getFriendRequestsByUserID(context.Background(), c.UserID)
		if err != nil {
			s.sendLine(c, "** error fetching friend requests: %v", err)
			return
		}
		var out string
		if len(reqs) == 0 {
			out = "** friend requests: (none)"
		} else {
			var names []string
			for _, fr := range reqs {
				names = append(names, fr.fromNick.nick)
			}
			out = "** friend requests: " + strings.Join(names, ", ")
		}
		if err := s.sendLine(c, "%s", out); err != nil {
			fmt.Println("sendLine /friendreqs:", err)
		}
	case "/acceptfriend":

		if len(parts) < 2 {
			s.sendLine(c, "usage: /acceptfriend <nick>")
			return
		}

		nick := parts[1]
		fromID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		if err := s.repo.acceptFriendRequest(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error accepting friend request from: %s", nick)
			return
		}
		if err := s.repo.addFriend(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error adding friend: %s", nick)
			return
		}
		s.sendLine(c, "** accepted friend request from: %s", nick)
	case "/declinefriend":

		if len(parts) < 2 {
			s.sendLine(c, "usage: /declinefriend <nick>")
			return
		}

		nick := parts[1]
		fromID, err := s.repo.findClientIDByNick(nick)
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", nick)
			return
		}
		if err := s.repo.declineFriendRequest(context.Background(), fromID, c.UserID); err != nil {
			s.sendLine(c, "** error declining friend request from: %s", nick)
			return
		}
		s.sendLine(c, "** declined friend request from: %s", nick)
	case "/quit":
		s.sendLine(c, "** bye")
		_ = c.conn.Close()

	default:
		s.sendLine(c, "** unknown command: %s (type /help | /?)", name)
	}
}
