create table voice_channels (
    id bigserial primary key,
    room_id bigint references rooms(id),
    name text not null,
    created_at timestamptz default now(),
    unique(room_id, name)
);