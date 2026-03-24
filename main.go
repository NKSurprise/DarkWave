package main

import (
	"log"
	"os"

	"context"

	// PostgreSQL driver

	"github.com/joho/godotenv"
	// needed for password hashing
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func main() {
	_ = godotenv.Load()

	dbURL := mustEnv("DB_URL")
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = ":3000"
	}

	pool := mustInitPool(dbURL)
	defer pool.Close()

	repo := NewRepo(pool)
	s := NewServer(port, repo)
	log.Fatal(s.Start(context.Background()))
}
