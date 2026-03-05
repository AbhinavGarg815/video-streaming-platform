#!/usr/bin/env sh
set -eu

if [ ! -f ./deploy/aws/.env.aws ]; then
  echo "Missing ./deploy/aws/.env.aws"
  echo "Copy ./deploy/aws/.env.aws.example to ./deploy/aws/.env.aws and fill values."
  exit 1
fi

docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml pull || true
docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml up -d --build
docker compose --env-file ./deploy/aws/.env.aws -f docker-compose.aws.yml ps
