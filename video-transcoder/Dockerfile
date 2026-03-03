FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /transcoder ./cmd/transcode

FROM alpine:3.20

RUN apk add --no-cache ffmpeg ca-certificates

WORKDIR /app
COPY --from=builder /transcoder /usr/local/bin/transcoder

ENV FFMPEG_BINARY=ffmpeg
ENV WORK_DIR=/tmp/transcoder

ENTRYPOINT ["/usr/local/bin/transcoder"]

