# Production Deployment on AWS (EC2 + Docker Compose)

This setup runs all 3 services on one EC2 host with HTTPS:
- `backend` (Gin API)
- `completion-worker` (SQS completion consumer)
- `video-transcoder` (SQS transcode consumer + ffmpeg)
- `caddy` (automatic TLS reverse proxy)

## 1. Why this setup

- Lowest cost while using your AWS credits.
- Fastest path to production for your current architecture.
- Keeps S3/SQS in AWS and avoids cross-cloud complexity.

## 2. AWS resources checklist

Create/verify:
- S3 bucket: `video-app-originals`
- S3 bucket: `video-app-transcoded`
- SQS queue: transcode queue URL
- SQS queue: completion queue URL
- CloudFront distribution (optional but recommended)
- Postgres (`DATABASE_URL`) (Neon or RDS)

IAM user policy must allow:
- `s3:GetObject`, `s3:PutObject`, `s3:ListBucket` on both buckets
- `sqs:ReceiveMessage`, `sqs:DeleteMessage`, `sqs:ChangeMessageVisibility`, `sqs:GetQueueAttributes` on worker queues
- `sqs:SendMessage` to completion queue

## 3. Provision EC2

Recommended instance:
- Start: `t4g.small` (ARM) or `t3.small` (x86)
- Storage: at least 30 GB gp3

Security group inbound:
- `22/tcp` from your IP
- `80/tcp` from `0.0.0.0/0`
- `443/tcp` from `0.0.0.0/0`

Point DNS:
- Create `A` record `api.yourdomain.com` -> EC2 public IP.

## 4. Install Docker + Compose on EC2

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo $VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker $USER
newgrp docker
```

## 5. Deploy app

```bash
git clone <your-repo-url>
cd video-streaming-platform
cp deploy/aws/.env.aws.example deploy/aws/.env.aws
```

Edit `deploy/aws/.env.aws` with real values.

Then deploy:

```bash
chmod +x deploy/aws/deploy.sh
./deploy/aws/deploy.sh
```

## 6. Verify

```bash
docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml ps
docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml logs -f backend
```

Checks:
- `https://api.yourdomain.com/health` returns OK.
- Upload flow returns presigned URL and `video_id`.
- After upload, transcoder logs show ffmpeg processing.
- Completion worker marks video as ready.
- Player loads: `https://api.yourdomain.com/watch/<video_id>`.

## 7. Zero-downtime update flow

```bash
git pull
./deploy/aws/deploy.sh
```

This rebuilds and restarts containers with `restart: unless-stopped`.

## 8. Backups and operations

- If using Neon, enable backups there.
- If using RDS, enable automated snapshots.
- Rotate IAM keys periodically.
- Monitor logs:
  - `docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml logs -f`

## 9. Notes

- This is production-ready for MVP/small traffic.
- For higher scale: move to ECS/Fargate + ECR + ASG/ECS autoscaling.
