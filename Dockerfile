FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -o /out/edge-agent \
    ./cmd/edge-agent

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
USER 10001:10001

COPY --from=builder /out/edge-agent /edge-agent
COPY config.yml /etc/edge-agent/config.yml

ENTRYPOINT ["/edge-agent", "-config", "/etc/edge-agent/config.yml"]