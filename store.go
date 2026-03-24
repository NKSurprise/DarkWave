package main

import "context"

func (r *Repo) listRooms() ([]string, error) {
	rows, err := r.pool.Query(context.Background(), `
		select name from rooms order by name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func (r *Repo) UpsertClientByNick(ctx context.Context, nick string) (id int64, retNick string, err error) {
	err = r.pool.QueryRow(ctx, `
        insert into clients(nick) values ($1)
        on conflict(nick) do update set nick = excluded.nick
        returning id, nick
    `, nick).Scan(&id, &retNick)
	return id, retNick, err
}

// create-or-get room by name
func (r *Repo) UpsertRoomByName(ctx context.Context, name string) (id int64, retName string, err error) {
	err = r.pool.QueryRow(ctx, `
        insert into rooms(name) values ($1)
        on conflict(name) do update set name = excluded.name
        returning id, name
    `, name).Scan(&id, &retName)
	return id, retName, err
}

func (r *Repo) declineFriendRequest(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		update friend_requests
		set status = 'declined', responded_at = now()
		where from_id = $1 and to_id = $2 and status = 'pending'
	`, fromID, toID)
	return err
}

func (r *Repo) addFriend(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		insert into friends(user_id, friend_id, created_at)
		values ($1, $2, now())
	`, fromID, toID)
	return err
}

func (r *Repo) acceptFriendRequest(ctx context.Context, fromID, toID int64) error {
	_, err := r.pool.Exec(ctx, `
		update friend_requests
		set status = 'accepted', responded_at = now()
		where from_id = $1 and to_id = $2 and status = 'pending'
	`, fromID, toID)

	return err
}

func (r *Repo) getFriendsByUserID(ctx context.Context, userID int64) ([]Friend, error) {
	rows, err := r.pool.Query(ctx, `
		select friend_id, created_at from friends where user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []Friend
	for rows.Next() {
		var f Friend
		if err := rows.Scan(&f.friendID, &f.createdAt); err != nil {
			return nil, err
		}
		friends = append(friends, f)
	}
	return friends, nil
}

func (r *Repo) getFriendRequestsByUserID(ctx context.Context, userID int64) ([]FriendRequest, error) {
	rows, err := r.pool.Query(ctx, `
		select id, from_id, to_id, status, created_at, responded_at
		from friend_requests
		where to_id = $1 and status = 'pending'
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []FriendRequest
	for rows.Next() {
		var fr FriendRequest
		if err := rows.Scan(&fr.ID, &fr.fromID, &fr.toID, &fr.status, &fr.createdAt, &fr.respondedAt); err != nil {
			return nil, err
		}
		requests = append(requests, fr)
	}
	return requests, nil
}

func (r *Repo) sendFriendRequest(fromID, toID int64) error {
	_, err := r.pool.Exec(context.Background(), `
		insert into friend_requests(from_id, to_id, status, created_at)
		values ($1, $2, 'pending', now())
	`, fromID, toID)
	return err
}

func (r *Repo) findClientIDByNick(nick string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(context.Background(), `
		select id from clients where nick = $1
	`, nick).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (r *Repo) checkClientHasPassword(ctx context.Context, id int64) (bool, error) {
	var storedHash string
	err := r.pool.QueryRow(ctx, `
		select password from clients where id = $1
	`, id).Scan(&storedHash)
	if err != nil {
		return false, err
	}
	return storedHash != "", nil
}

func (r *Repo) getClientByID(ctx context.Context, id int64) (*Client, error) {
	var nick string
	err := r.pool.QueryRow(ctx, `
		select nick from clients where id = $1
	`, id).Scan(&nick)
	if err != nil {
		return nil, err
	}
	return &Client{UserID: id, nick: nick}, nil
}

func (r *Repo) SavePassword(password string, id int64) error {
	_, err := r.pool.Exec(context.Background(), `
		UPDATE clients SET password = $1 WHERE id = $2
	`, password, id)
	return err
}

func (r *Repo) GetPasswordById(id int64) (string, error) {
	var password string
	err := r.pool.QueryRow(context.Background(), `
		select password from clients where id = $1
	`, id).Scan(&password)
	if err != nil {
		return "", err
	}
	return password, nil
}

func (r *Repo) SaveMessage(msg Message) error {
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO messages (room_id, from_id, body)
		VALUES ($1, $2, $3)
	`,
		msg.room.ID,
		msg.from.UserID,
		string(msg.payload),
	)
	return err
}

func (r *Repo) GetRecentMessages(roomID int64) ([]DBMessage, error) {
	rows, err := r.pool.Query(context.Background(), `
		SELECT id, room_id, from_id, body, sent_at
		FROM messages
		WHERE room_id = $1
		ORDER BY sent_at DESC
		LIMIT 50
	`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []DBMessage
	for rows.Next() {
		var m DBMessage
		if err := rows.Scan(&m.ID, &m.RoomID, &m.FromID, &m.Body, &m.SentAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (r *Repo) GetRecentMessagesWithUsers(roomID int64) ([]MessageWithSender, error) {
	rows, err := r.pool.Query(context.Background(), `
		SELECT m.body, m.sent_at, c.nick
		FROM messages m
		JOIN clients c ON m.from_id = c.id
		WHERE m.room_id = $1
		ORDER BY m.sent_at DESC
		LIMIT 50
	`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageWithSender
	for rows.Next() {
		var m MessageWithSender
		if err := rows.Scan(&m.Body, &m.SentAt, &m.Nick); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
