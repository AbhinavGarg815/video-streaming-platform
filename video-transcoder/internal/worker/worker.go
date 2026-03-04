package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/config"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/ffmpeg"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/job"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/queue"
	"github.com/AbhinavGarg815/video-streaming-platform/video-transcoder/internal/storage"
)

type Worker struct {
	env        config.Env
	queue      *queue.SQS
	storage    *storage.S3
	transcoder *ffmpeg.Transcoder
}

var ErrIgnoredMessage = errors.New("ignored message")

func New(env config.Env, queueClient *queue.SQS, storageClient *storage.S3, transcoder *ffmpeg.Transcoder) *Worker {
	return &Worker{
		env:        env,
		queue:      queueClient,
		storage:    storageClient,
		transcoder: transcoder,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := os.MkdirAll(w.env.WorkDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	log.Printf("transcoder worker started; queue=%s", w.env.TranscodeQueueURL)

	for {
		select {
		case <-ctx.Done():
			log.Printf("transcoder worker shutting down")
			return nil
		default:
		}

		messages, err := w.queue.Receive(ctx, w.env.SQSMaxMessages, w.env.SQSWaitTimeSeconds, w.env.VisibilityTimeout)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			log.Printf("receive message error: %v", err)
			time.Sleep(w.env.PollInterval)
			continue
		}

		if len(messages) == 0 {
			continue
		}

		for _, msg := range messages {
			if err := w.processMessage(ctx, msg); err != nil {
				log.Printf("process message failed: %v", err)
			}
		}
	}
}

func (w *Worker) processMessage(ctx context.Context, msg queue.Message) error {
	transcodeJob, err := decodeTranscodeJob(msg.Body)
	if err != nil {
		_ = w.queue.Delete(ctx, msg.ReceiptHandle)
		if errors.Is(err, ErrIgnoredMessage) {
			return nil
		}
		return fmt.Errorf("invalid message body: %w", err)
	}

	if err := validateJob(&transcodeJob, w.env.OriginalVideoBucket); err != nil {
		_ = w.queue.Delete(ctx, msg.ReceiptHandle)
		if strings.TrimSpace(transcodeJob.VideoID) != "" {
			_ = w.sendCompletion(ctx, transcodeJob, "failed", nil, err)
		}
		return err
	}

	outputs, processErr := w.processJob(ctx, transcodeJob)
	if processErr != nil {
		_ = w.sendCompletion(ctx, transcodeJob, "failed", nil, processErr)
		return processErr
	}

	if err := w.sendCompletion(ctx, transcodeJob, "completed", outputs, nil); err != nil {
		return err
	}

	if err := w.queue.Delete(ctx, msg.ReceiptHandle); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	log.Printf("job done: video_id=%s outputs=%d", transcodeJob.VideoID, len(outputs))
	return nil
}

func decodeTranscodeJob(body string) (job.TranscodeJob, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return job.TranscodeJob{}, fmt.Errorf("empty message body")
	}

	if isS3TestEvent(body) {
		return job.TranscodeJob{}, ErrIgnoredMessage
	}

	var direct job.TranscodeJob
	if err := json.Unmarshal([]byte(body), &direct); err == nil {
		if strings.TrimSpace(direct.SourceKey) != "" || strings.TrimSpace(direct.VideoID) != "" {
			return direct, nil
		}
	}

	type snsEnvelope struct {
		Message string `json:"Message"`
	}

	var envelope snsEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return job.TranscodeJob{}, err
	}

	if strings.TrimSpace(envelope.Message) == "" {
		if parsed, ok := parseS3EventBody(body); ok {
			return parsed, nil
		}
		if isS3TestEvent(body) {
			return job.TranscodeJob{}, ErrIgnoredMessage
		}
		return job.TranscodeJob{}, fmt.Errorf("sns envelope missing Message")
	}

	if err := json.Unmarshal([]byte(envelope.Message), &direct); err != nil {
		if parsed, ok := parseS3EventBody(envelope.Message); ok {
			return parsed, nil
		}
		return job.TranscodeJob{}, err
	}

	if strings.TrimSpace(direct.SourceKey) == "" && strings.TrimSpace(direct.VideoID) == "" {
		if parsed, ok := parseS3EventBody(envelope.Message); ok {
			return parsed, nil
		}
	}

	return direct, nil
}

func isS3TestEvent(body string) bool {
	type testEvent struct {
		Service string `json:"Service"`
		Event   string `json:"Event"`
		Bucket  string `json:"Bucket"`
	}

	var event testEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(event.Service), "Amazon S3") &&
		strings.EqualFold(strings.TrimSpace(event.Event), "s3:TestEvent") &&
		strings.TrimSpace(event.Bucket) != ""
}

func parseS3EventBody(body string) (job.TranscodeJob, bool) {
	type s3Event struct {
		Records []struct {
			S3 struct {
				Bucket struct {
					Name string `json:"name"`
				} `json:"bucket"`
				Object struct {
					Key string `json:"key"`
				} `json:"object"`
			} `json:"s3"`
		} `json:"Records"`
	}

	var event s3Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return job.TranscodeJob{}, false
	}
	if len(event.Records) == 0 {
		return job.TranscodeJob{}, false
	}

	bucket := strings.TrimSpace(event.Records[0].S3.Bucket.Name)
	key := strings.TrimSpace(event.Records[0].S3.Object.Key)
	if bucket == "" || key == "" {
		return job.TranscodeJob{}, false
	}

	if decodedKey, err := url.QueryUnescape(key); err == nil && strings.TrimSpace(decodedKey) != "" {
		key = decodedKey
	}

	return job.TranscodeJob{
		SourceBucket: bucket,
		SourceKey:    key,
		OutputPrefix: fmt.Sprintf("videos/%s", sourceKeyIdentifier(key)),
	}, true
}

func (w *Worker) processJob(ctx context.Context, j job.TranscodeJob) ([]job.OutputFile, error) {
	jobID := jobIdentifier(j)
	jobDir := filepath.Join(w.env.WorkDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return nil, fmt.Errorf("create job dir: %w", err)
	}
	defer os.RemoveAll(jobDir)

	inputPath := filepath.Join(jobDir, filepath.Base(j.SourceKey))
	if err := w.storage.Download(ctx, j.SourceBucket, j.SourceKey, inputPath); err != nil {
		return nil, fmt.Errorf("download source: %w", err)
	}

	resolutions := j.Resolutions
	if len(resolutions) == 0 {
		resolutions = []string{"360p", "480p", "720p", "1080p"}
	}

	outputPrefix := strings.TrimSpace(j.OutputPrefix)
	if outputPrefix == "" {
		outputPrefix = fmt.Sprintf("videos/%s", jobID)
	}

	files, err := w.transcoder.Transcode(inputPath, jobDir, resolutions)
	if err != nil {
		return nil, err
	}

	outputs := make([]job.OutputFile, 0, len(files))
	for _, file := range files {
		s3Key := strings.TrimPrefix(filepath.ToSlash(filepath.Join(outputPrefix, file.KeySuffix)), "/")
		if err := w.storage.Upload(ctx, w.env.TranscodedBucket, s3Key, file.Path, file.ContentType); err != nil {
			return nil, fmt.Errorf("upload file %s: %w", file.Path, err)
		}

		if !file.IsManifest {
			continue
		}

		outputs = append(outputs, job.OutputFile{
			Resolution: file.Resolution,
			Format:     "hls",
			Bucket:     w.env.TranscodedBucket,
			Key:        s3Key,
			SizeBytes:  file.SizeBytes,
		})
	}

	return outputs, nil
}

func (w *Worker) sendCompletion(ctx context.Context, j job.TranscodeJob, status string, outputs []job.OutputFile, processErr error) error {
	completion := job.CompletionMessage{
		VideoID:      j.VideoID,
		UserID:       j.UserID,
		SourceBucket: j.SourceBucket,
		SourceKey:    j.SourceKey,
		Status:       status,
		Outputs:      outputs,
		Processed:    time.Now().UTC().Format(time.RFC3339),
	}
	if processErr != nil {
		completion.Error = processErr.Error()
	}

	body, err := json.Marshal(completion)
	if err != nil {
		return fmt.Errorf("marshal completion message: %w", err)
	}

	if err := w.queue.SendCompletion(ctx, string(body)); err != nil {
		return fmt.Errorf("send completion message: %w", err)
	}

	return nil
}

func validateJob(j *job.TranscodeJob, fallbackBucket string) error {
	if strings.TrimSpace(j.SourceKey) == "" {
		return fmt.Errorf("source_key is required")
	}
	if strings.TrimSpace(j.SourceBucket) == "" {
		j.SourceBucket = fallbackBucket
	}
	if strings.TrimSpace(j.SourceBucket) == "" {
		return fmt.Errorf("source_bucket is required")
	}

	return nil
}

func jobIdentifier(j job.TranscodeJob) string {
	if strings.TrimSpace(j.VideoID) != "" {
		return strings.TrimSpace(j.VideoID)
	}

	return sourceKeyIdentifier(j.SourceKey)
}

func sourceKeyIdentifier(sourceKey string) string {
	base := strings.TrimSpace(filepath.Base(sourceKey))
	if base == "" {
		return fmt.Sprintf("job-%d", time.Now().UTC().Unix())
	}

	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return fmt.Sprintf("job-%d", time.Now().UTC().Unix())
	}

	return name
}
