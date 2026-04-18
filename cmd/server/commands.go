package main

import (
	"bufio"
	"context"
	argon2stuff "darkwave/cmd/server/passwordHandling"
	"fmt"
	"strings"
)

const helpText = "" +
	"** commands **\n" +
	"  /help | /?            show this help\n" +
	"  /nick <name>          set your nickname\n" +
	"  /rooms                list available rooms\n" +
	"  /join <room>          join or create a room (e.g., /join main)\n" +
	"  /leave                leave current room\n" +
	"  /friends              list your friends\n" +
	"  /addfriend <nick>     add a friend by nickname\n" +
	"  /dm <nick>            open encrypted DM\n" +
	"  /friendreqs           list pending friend requests\n" +
	"  /acceptfriend <nick>  accept a friend request\n" +
	"  /declinefriend <nick> decline a friend request\n" +
	"  /online               check online status of friends\n" +
	"  /createvoice <name>   create a voice channel (room creator only)\n" +
	"  /voicechannels        list voice channels in current room\n" +
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

func (s *Server) handleCommand(c *Client, cmd string, r *bufio.Reader) {
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
		names, err := s.repo.getUserRooms(context.Background(), c.UserID)
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
		s.sendLine(c, "%s", out)
	case "/createvoice":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /createvoice <name>")
			return
		}
		if c.activeRoom == nil {
			s.sendLine(c, "** join a room first")
			return
		}
		isCreator, err := s.repo.isRoomCreator(context.Background(), c.UserID, c.activeRoom.ID)
		if err != nil || !isCreator {
			s.sendLine(c, "** only the room creator can create voice channels")
			return
		}
		_, err = s.repo.createVoiceChannel(context.Background(), c.activeRoom.ID, parts[1])
		if err != nil {
			s.sendLine(c, "** error creating voice channel: %v", err)
			return
		}
		s.broadcastToRoom(c.activeRoom, fmt.Sprintf("** voicechannel added: %s", parts[1]))
		s.sendLine(c, "** voice channel %s created", parts[1])
	case "/voicechannels":
		if c.activeRoom == nil {
			s.sendLine(c, "** join a room first")
			return
		}
		names, err := s.repo.getVoiceChannels(context.Background(), c.activeRoom.ID)
		if err != nil {
			s.sendLine(c, "** error fetching voice channels: %v", err)
			return
		}
		if len(names) == 0 {
			s.sendLine(c, "** voice channels: (none)")
			return
		}
		s.sendLine(c, "** voice channels: %s", strings.Join(names, ", "))
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
		passwordPlainText, err := s.readLoopPassword(c, r)
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
				passwordPlainText, err = s.readLoopPassword(c, r)
				if err != nil {
					s.sendLine(c, "** error reading password: %s", err)
					return
				}
			}
		}

		s.sendLine(c, "** welcome, %s!", retNick)
		s.registerClientSingle(c)
		s.notifyFriendsOnline(c)
		s.notifyRoomMembersPresence(c, "online")

		// Ensure #main room exists in database with correct ID
		mainID, mainName, err := s.repo.UpsertRoomByName(context.Background(), "#main", id)
		if err != nil {
			s.sendLine(c, "** warning: could not ensure main room: %v", err)
		}

		// Add user to #main room in database (non-fatal if fails)
		if err := s.repo.addUserToRoom(context.Background(), id, mainID); err != nil {
			fmt.Printf("WARNING: failed to add user %d to room #main (ID=%d): %v\n", id, mainID, err)
		}

		// Get room from server cache, ensure it has the correct ID from database
		mainRoom := s.getOrCreateRoom(mainName, mainID)
		s.joinRoom(c, mainRoom)
		_ = s.sendLine(c, "** connected. type /help | /?")

		// Send user their rooms list so they see #main
		names, err := s.repo.getUserRooms(context.Background(), id)
		if err != nil {
			fmt.Printf("WARNING: failed to fetch rooms for user %d: %v\n", id, err)
			// fallback: at least show #main
			s.sendLine(c, "** rooms: #main")
		} else {
			if len(names) == 0 {
				s.sendLine(c, "** rooms: (none)")
			} else {
				s.sendLine(c, "** rooms: %s", strings.Join(names, ", "))
			}
		}

		// load recent messages
		msgs, err := s.repo.GetRecentMessagesWithUsers(mainRoom.ID)
		if err != nil {
			s.sendLine(c, "** error fetching recent messages: %v", err)
		} else {
			for _, msg := range msgs {
				s.sendLine(c, "[%s] %s: %s", msg.SentAt.Format("02/01 15:04"), msg.Nick, msg.Body)
			}
		}
	case "/join":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /join <room>")
			return
		}
		id, retName, err := s.repo.UpsertRoomByName(context.Background(), parts[1], c.UserID)
		if err != nil {
			s.sendLine(c, "** error: %v", err)
			return
		}
		// save membership to DB
		if err := s.repo.addUserToRoom(context.Background(), c.UserID, id); err != nil {
			s.sendLine(c, "** error joining room: %v", err)
			return
		}
		room := s.getOrCreateRoom(retName, id)
		room.Name = retName
		s.joinRoom(c, room)

		// Send updated rooms list to client so they see the new room
		names, err := s.repo.getUserRooms(context.Background(), c.UserID)
		if err == nil {
			if len(names) == 0 {
				s.sendLine(c, "** rooms: (none)")
			} else {
				s.sendLine(c, "** rooms: %s", strings.Join(names, ", "))
			}
		}

		// load recent messages
		msgs, err := s.repo.GetRecentMessagesWithUsers(room.ID)
		if err != nil {
			s.sendLine(c, "** error fetching recent messages: %v", err)
		} else {
			for _, msg := range msgs {
				s.sendLine(c, "[%s] %s: %s", msg.SentAt.Format("02/01 15:04"), msg.Nick, msg.Body)
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
	case "/leave":
		if c.activeRoom == nil {
			s.sendLine(c, "** you are not in a room")
			return
		}
		room := c.activeRoom // save reference before clearing
		roomID := room.ID
		roomName := room.Name

		if err := s.repo.removeUserFromRoom(context.Background(), c.UserID, roomID); err != nil {
			s.sendLine(c, "** error leaving room: %v", err)
			return
		}
		room.mu.Lock()
		delete(room.clients, c)
		room.mu.Unlock()
		c.activeRoom = nil

		s.broadcastToRoom(room, fmt.Sprintf("** left: %s", c.nick))
		s.sendLine(c, "** you left %s", roomName)
	case "/online":
		friends, err := s.repo.getFriendsByUserID(context.Background(), c.UserID)
		if err != nil {
			return
		}
		for _, f := range friends {
			if friend, ok := s.onlineByUserID(f.friendID); ok {
				s.sendLine(c, "** status: %s online", friend.nick)
			}
		}
	case "/dm":
		if len(parts) < 2 {
			s.sendLine(c, "usage: /dm <nick>")
			return
		}
		targetID, err := s.repo.findClientIDByNick(parts[1])
		if err != nil {
			s.sendLine(c, "** no user with nick: %s", parts[1])
			return
		}
		// create deterministic room name so both users get same room
		var roomName string
		if c.UserID < targetID {
			roomName = fmt.Sprintf("dm:%d-%d", c.UserID, targetID)
		} else {
			roomName = fmt.Sprintf("dm:%d-%d", targetID, c.UserID)
		}
		id, retName, err := s.repo.UpsertRoomByName(context.Background(), roomName, c.UserID)
		if err != nil {
			s.sendLine(c, "** error creating dm: %v", err)
			return
		}
		room := s.getOrCreateRoom(retName, id)
		room.Name = retName
		s.joinRoom(c, room)
		// tell client who they're DMing and what the room is
		s.sendLine(c, "** dm: %s %s", parts[1], retName)

		msgs, err := s.repo.GetRecentMessagesWithUsers(room.ID)
		if err != nil {
			s.sendLine(c, "** error fetching recent messages: %v", err)
		} else {
			for _, msg := range msgs {
				s.sendLine(c, "[%s] (%s) %s", msg.SentAt.Format("02/01 15:04"), msg.Nick, msg.Body)
			}
		}
	case "/quit":
		s.sendLine(c, "** bye")
		_ = c.conn.Close()

	default:
		s.sendLine(c, "** unknown command: %s (type /help | /?)", name)
	}
}
