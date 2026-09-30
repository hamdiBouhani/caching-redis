package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	cache := NewCache(rdb)
	pub := NewPublisher(rdb)
	q := NewQueue(rdb)
	srv := NewServer(cache, pub, q)

	// Start asynq worker in background
	go StartWorker("localhost:6379")

	// Start subscriber in background
	go pub.Subscribe(context.Background(), "user.updated", func(payload []byte) {
		log.Printf("event user.updated: %s", payload)
	})

	// --- Gin setup ---
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode) // change to gin.DebugMode for dev
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Health check
	router.GET("/healthz", func(c *gin.Context) {
		if err := rdb.Ping(c.Request.Context()).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Routes
	router.GET("/user", srv.GetUser)
	router.POST("/user/update", srv.UpdateUser)

	// --- Graceful shutdown ---
	srvHTTP := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Println("listening on :8080")
		if err := srvHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srvHTTP.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	if err := rdb.Close(); err != nil {
		log.Printf("redis close error: %v", err)
	}
	log.Println("bye")
}
