package completion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	appconfig "github.com/AbhinavGarg815/video-streaming-platform/internal/config"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Consumer struct {
	queueURL   string
	client     *sqs.Client
	repository *Repository
}

var ErrInvalidCompletionPayload = errors.New("invalid completion payload")

func NewConsumer(ctx context.Context, env appconfig.Env, pool *pgxpool.Pool) (*Consumer, error) {
	queueURL := strings.TrimSpace(env.CompletionQueueURL)
	if queueURL == "" {
		return nil, fmt.Errorf("COMPLETION_QUEUE_URL is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(env.AWSRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(env.AWSAccessKeyID, env.AWSSecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &Consumer{
		queueURL:   queueURL,
		client:     sqs.NewFromConfig(awsCfg),
		repository: NewRepository(pool),
	}, nil
}

func (c *Consumer) Run(ctx context.Context) error {
	log.Printf("completion worker started; queue=%s", c.queueURL)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		result, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &c.queueURL,
			MaxNumberOfMessages: 5,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   120,
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}

			log.Printf("receive completion message failed: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, message := range result.Messages {
			body := ""
			if message.Body != nil {
				body = *message.Body
			}

			if err := c.handleMessage(ctx, body); err != nil {
				log.Printf("handle completion message failed: %v", err)

				if errors.Is(err, ErrInvalidCompletionPayload) && message.ReceiptHandle != nil {
					if _, deleteErr := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
						QueueUrl:      &c.queueURL,
						ReceiptHandle: message.ReceiptHandle,
					}); deleteErr != nil {
						log.Printf("delete invalid completion message failed: %v", deleteErr)
					}
				}

				continue
			}

			if message.ReceiptHandle != nil {
				if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &c.queueURL,
					ReceiptHandle: message.ReceiptHandle,
				}); err != nil {
					log.Printf("delete completion message failed: %v", err)
				}
			}
		}
	}
}

func (c *Consumer) handleMessage(ctx context.Context, body string) error {
	message, err := decodeCompletionMessage(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompletionPayload, err)
	}

	if strings.TrimSpace(message.VideoID) == "" && strings.TrimSpace(message.SourceKey) == "" {
		return fmt.Errorf("%w: video_id or source_key is required", ErrInvalidCompletionPayload)
	}

	if err := c.repository.ApplyCompletion(ctx, message); err != nil {
		return err
	}

	log.Printf("completion applied: video_id=%s status=%s outputs=%d", message.VideoID, message.Status, len(message.Outputs))
	return nil
}

func decodeCompletionMessage(body string) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, fmt.Errorf("empty message body")
	}

	var message Message
	if err := json.Unmarshal([]byte(body), &message); err == nil {
		return message, nil
	}

	type snsEnvelope struct {
		Message string `json:"Message"`
	}

	var envelope snsEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return Message{}, err
	}

	if strings.TrimSpace(envelope.Message) == "" {
		return Message{}, fmt.Errorf("sns envelope missing Message")
	}

	if err := json.Unmarshal([]byte(envelope.Message), &message); err != nil {
		return Message{}, err
	}

	return message, nil
}
