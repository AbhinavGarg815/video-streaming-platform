FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /transcoder ./cmd/transcode

FROM alpine:3.20

RUN apk add --no-cache ffmpeg ca-certificates

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /transcoder /usr/local/bin/transcoder

ENV FFMPEG_BINARY=ffmpeg
ENV WORK_DIR=/tmp/transcoder

RUN mkdir -p /tmp/transcoder && chown -R app:app /tmp/transcoder

USER app

ENTRYPOINT ["/usr/local/bin/transcoder"]

