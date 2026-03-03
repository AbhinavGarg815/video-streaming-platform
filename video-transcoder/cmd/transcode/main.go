package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/config"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/ffmpeg"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/queue"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/storage"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/worker"
)

func main() {
	env, err := config.Load()
	if err != nil {
		log.Fatalf("load env: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	clients, err := queue.NewAWSClients(ctx, env.AWSRegion, env.AWSAccessKeyID, env.AWSSecretAccessKey)
	if err != nil {
		log.Fatalf("init aws clients: %v", err)
	}

	sqsClient := queue.NewSQS(clients.SQS, env.TranscodeQueueURL, env.CompletionQueueURL)
	s3Client := storage.NewS3(clients.S3)
	transcoder := ffmpeg.New(env.FFmpegBinary)

	w := worker.New(env, sqsClient, s3Client, transcoder)
	if err := w.Run(ctx); err != nil {
		log.Fatalf("worker stopped with error: %v", err)
	}
}
