package main

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
)

type Publisher struct {
	rdb *redis.Client
}

func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb}
}

func (p *Publisher) Publish(ctx context.Context, channel string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return p.rdb.Publish(ctx, channel, raw).Err()
}

// Subscribe runs a blocking loop — call in a goroutine.
func (p *Publisher) Subscribe(ctx context.Context, channel string, handle func([]byte)) {
	sub := p.rdb.Subscribe(ctx, channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			handle([]byte(msg.Payload))
		}
	}
}
