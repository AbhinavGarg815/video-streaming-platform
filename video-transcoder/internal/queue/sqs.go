package queue

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type AWSClients struct {
	SQS *sqs.Client
	S3  *s3.Client
}

func NewAWSClients(ctx context.Context, region, accessKeyID, secretAccessKey string) (AWSClients, error) {
	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return AWSClients{}, fmt.Errorf("load aws config: %w", err)
	}

	return AWSClients{
		SQS: sqs.NewFromConfig(cfg),
		S3:  s3.NewFromConfig(cfg),
	}, nil
}

type Message struct {
	Body          string
	ReceiptHandle string
}

type SQS struct {
	client             *sqs.Client
	transcodeQueueURL  string
	completionQueueURL string
}

func NewSQS(client *sqs.Client, transcodeQueueURL, completionQueueURL string) *SQS {
	return &SQS{
		client:             client,
		transcodeQueueURL:  transcodeQueueURL,
		completionQueueURL: completionQueueURL,
	}
}

func (q *SQS) Receive(ctx context.Context, maxMessages, waitTimeSeconds, visibilityTimeout int32) ([]Message, error) {
	result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &q.transcodeQueueURL,
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     waitTimeSeconds,
		VisibilityTimeout:   visibilityTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("receive sqs message: %w", err)
	}

	out := make([]Message, 0, len(result.Messages))
	for _, message := range result.Messages {
		out = append(out, Message{
			Body:          valueOrEmpty(message.Body),
			ReceiptHandle: valueOrEmpty(message.ReceiptHandle),
		})
	}

	return out, nil
}

func (q *SQS) Delete(ctx context.Context, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &q.transcodeQueueURL,
		ReceiptHandle: &receiptHandle,
	})
	if err != nil {
		return fmt.Errorf("delete sqs message: %w", err)
	}

	return nil
}

func (q *SQS) SendCompletion(ctx context.Context, body string) error {
	_, err := q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &q.completionQueueURL,
		MessageBody: &body,
		MessageAttributes: map[string]types.MessageAttributeValue{
			"event_type": {
				DataType:    strPtr("String"),
				StringValue: strPtr("video.transcode.completed"),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("send completion message: %w", err)
	}

	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func strPtr(value string) *string {
	return &value
}
