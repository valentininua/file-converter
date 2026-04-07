FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yt-encoder ./cmd

FROM alpine:3.20

RUN apk add --no-cache ca-certificates ffmpeg && \
    adduser -D -h /data appuser

WORKDIR /data

COPY --from=builder /out/yt-encoder /usr/local/bin/yt-encoder

USER appuser

ENTRYPOINT ["yt-encoder"]
CMD ["help"]