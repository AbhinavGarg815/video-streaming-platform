package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AbhinavGarg815/video-streaming-platform/internal/completion"
	"github.com/AbhinavGarg815/video-streaming-platform/internal/config"
	"github.com/AbhinavGarg815/video-streaming-platform/internal/database"
)

func main() {
	env, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := database.NewPool(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	consumer, err := completion.NewConsumer(ctx, env, pool)
	if err != nil {
		log.Fatalf("initialize completion consumer: %v", err)
	}

	if err := consumer.Run(ctx); err != nil {
		log.Fatalf("completion consumer stopped: %v", err)
	}
}
