FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o strava-sync .

FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/strava-sync .

CMD ["./strava-sync"]

# docker build -t strava-sync .

# docker run -d --name strava-sync \
#  -v /pad/naar/strava/.env:/app/.env:ro \
#  -v /pad/naar/strava/strava_tokens.json:/app/strava_tokens.json strava-sync
