create table room_members (
    user_id bigint references clients(id),
    room_id bigint references rooms(id),
    joined_at timestamptz default now(),
    primary key (user_id, room_id)
);