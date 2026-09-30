package main

import (
	"context"
	"errors"
	"time"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Simulated slow DB
func loadUserFromDB(ctx context.Context, id string) (*User, error) {
	time.Sleep(300 * time.Millisecond) // simulate latency
	if id == "missing" {
		return nil, errors.New("not found")
	}
	return &User{ID: id, Name: "Alice", Email: "alice@example.com"}, nil
}
