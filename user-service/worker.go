package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const TypeUserViewed = "user:viewed"

type Queue struct {
	client *asynq.Client
}

func NewQueue(rdb *redis.Client) *Queue {
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr: rdb.Options().Addr,
	})
	return &Queue{client: client}
}

func (q *Queue) EnqueueView(ctx context.Context, userID string) error {
	payload, _ := json.Marshal(map[string]string{"user_id": userID})
	task := asynq.NewTask(TypeUserViewed, payload)
	_, err := q.client.EnqueueContext(ctx, task,
		asynq.MaxRetry(3),
		asynq.Queue("default"),
	)
	return err
}

// ---- Worker side ----

func StartWorker(addr string) {
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: addr},
		asynq.Config{Concurrency: 10, Queues: map[string]int{"default": 1}},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeUserViewed, handleUserViewed)

	if err := srv.Run(mux); err != nil {
		log.Fatal(err)
	}
}

func handleUserViewed(ctx context.Context, t *asynq.Task) error {
	var p struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	log.Printf("recording view for user %s", p.UserID)
	// e.g. write to analytics DB, increment counter, etc.
	return nil
}
