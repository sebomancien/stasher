FROM golang:1.26-alpine3.23 AS builder

WORKDIR /build

# Cache dependency downloads separately from source compilation.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o stasher .

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/stasher .

# Default backup destination; mount a volume or bind-mount here.
VOLUME ["/backups"]

ENV BACKUP_DEST=/backups
ENV CHECK_INTERVAL=60s
ENV LOG_LEVEL=info

ENTRYPOINT ["./stasher"]
