# Deploy to Render

This repo is configured for 3 Render services via `render.yaml`:
- `video-backend` (Web Service)
- `completion-worker` (Background Worker)
- `video-transcoder` (Background Worker)

## 1. Prerequisites

- Push this repository to GitHub.
- Ensure AWS resources exist:
  - S3 bucket for originals: `video-app-originals`
  - S3 bucket for transcoded outputs: `video-app-transcoded`
  - SQS transcode queue URL
  - SQS completion queue URL
- Ensure Neon/Postgres `DATABASE_URL` is ready.
- Ensure AWS IAM credentials have permissions for:
  - S3: read/write/list for both buckets
  - SQS: receive/delete/change visibility (worker queues), send message (completion)

## 2. Create Services from Blueprint

1. Open Render Dashboard.
2. Click `New +` -> `Blueprint`.
3. Connect your GitHub repo and select branch.
4. Render will detect `render.yaml` and create 3 services automatically.

## 3. Set Environment Variables

Set these in Render (Dashboard -> each service -> Environment).

### `video-backend`
- `PORT=8080`
- `DATABASE_URL=<your_neon_or_postgres_url>`
- `JWT_SECRET=<strong_secret>`
- `ACCESS_TOKEN_TTL=1h`
- `REFRESH_TOKEN_TTL=168h`
- `AWS_REGION=<aws_region>`
- `AWS_ACCESS_KEY_ID=<aws_access_key_id>`
- `AWS_SECRET_ACCESS_KEY=<aws_secret_access_key>`
- `ORIGINAL_VIDEO_BUCKET=video-app-originals`
- `COMPLETION_QUEUE_URL=<sqs_completion_queue_url>`
- `CDN_BASE_URL=https://d26jaa9z4vqbr2.cloudfront.net` (or your own CloudFront domain)

### `completion-worker`
- `DATABASE_URL=<your_neon_or_postgres_url>`
- `AWS_REGION=<aws_region>`
- `AWS_ACCESS_KEY_ID=<aws_access_key_id>`
- `AWS_SECRET_ACCESS_KEY=<aws_secret_access_key>`
- `COMPLETION_QUEUE_URL=<sqs_completion_queue_url>`

### `video-transcoder`
- `AWS_REGION=<aws_region>`
- `AWS_ACCESS_KEY_ID=<aws_access_key_id>`
- `AWS_SECRET_ACCESS_KEY=<aws_secret_access_key>`
- `TRANSCODE_QUEUE_URL=<sqs_transcode_queue_url>`
- `COMPLETION_QUEUE_URL=<sqs_completion_queue_url>`
- `ORIGINAL_VIDEO_BUCKET=video-app-originals`
- `TRANSCODED_BUCKET=video-app-transcoded`
- `FFMPEG_BINARY=ffmpeg`
- `WORK_DIR=/tmp/transcoder`
- `SQS_WAIT_TIME_SECONDS=20`
- `SQS_MAX_MESSAGES=1`
- `SQS_VISIBILITY_TIMEOUT_SECONDS=900`
- `POLL_INTERVAL=2s`

## 4. Verify Deployment

1. Wait for all 3 services to show `Live`.
2. Test backend health:
   - `GET https://<video-backend-domain>/health`
3. Trigger upload flow:
   - Call presign endpoint with auth token.
   - Upload file to returned S3 presigned URL using `PUT`.
4. Confirm worker processing:
   - Transcoder logs should show download/transcode/upload.
   - Completion worker logs should show DB update to `ready`.
5. Open player URL:
   - `https://<video-backend-domain>/watch/<video_id>`

## 5. Notes

- `video-transcoder` Docker image already includes `ffmpeg`.
- If playback fails through CDN, verify CloudFront origin/OAC and bucket policy.
- If using S3 events to feed transcode queue, ensure event notifications target the correct queue.
