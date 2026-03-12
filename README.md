# DarkWave
An app that sends messegase to others or specific person but cant read messegase without specific key

## Setup

1. Create a PostgreSQL database (example):
   - `createdb darkwave`

2. Add credentials in `.env` (file is in repository root):
   - `DB_URL=postgres://user:password@localhost:5432/darkwave?sslmode=disable`
   - `APP_PORT=:3000`

3. Run:
   - `go run .`

4. If you change `.env`, restart.

## Notes

- `main.go` loads `.env` through `github.com/joho/godotenv` and requires `DB_URL` to be present.
- `APP_PORT` defaults to `:3000` when omitted.

