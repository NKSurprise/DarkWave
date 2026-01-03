-- 00_types.sql
DO $$
BEGIN
  CREATE TYPE friend_req_status AS ENUM ('pending','accepted','declined');
EXCEPTION
  WHEN duplicate_object THEN
    NULL;
END;
$$;

-- 10_rooms.sql
CREATE TABLE IF NOT EXISTS rooms (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 20_clients.sql
CREATE TABLE IF NOT EXISTS clients (
  id         BIGSERIAL PRIMARY KEY,
  nick       TEXT NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 30_messages.sql
CREATE TABLE IF NOT EXISTS messages (
  id       BIGSERIAL PRIMARY KEY,
  room_id  BIGINT NOT NULL REFERENCES rooms(id)   ON DELETE CASCADE,
  from_id  BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  body     TEXT   NOT NULL,
  sent_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_messages_room_time
  ON messages(room_id, sent_at DESC);

-- 40_friend_requests.sql
CREATE TABLE IF NOT EXISTS friend_requests (
  id           BIGSERIAL PRIMARY KEY,
  from_id      BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  to_id        BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  status       friend_req_status NOT NULL DEFAULT 'pending',
  created_at   timestamptz NOT NULL DEFAULT now(),
  responded_at timestamptz
);

-- prevent duplicate pending requests
CREATE UNIQUE INDEX IF NOT EXISTS uq_friend_req_pair_pending
  ON friend_requests(from_id, to_id)
  WHERE status = 'pending';

-- 50_friends.sql
-- store accepted friendship once as (min, max)
CREATE TABLE IF NOT EXISTS friends (
  user_id   BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  friend_id BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, friend_id)
);
