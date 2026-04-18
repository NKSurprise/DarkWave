package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(p *pgxpool.Pool) *Repo {
	return &Repo{pool: p}
}

func mustInitPool(dbURL string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatal("parse DB_URL:", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatal("connect:", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal("db ping failed:", err)
	}
	fmt.Println("DB connected ✅  listening on", dbURL)
	return pool
}
