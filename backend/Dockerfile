FROM golang:1.23 AS builder
WORKDIR /src
COPY . .
RUN go test ./... && go build -o /out/zxy-panel-api ./cmd/server

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /app/data
COPY --from=builder /out/zxy-panel-api /app/zxy-panel-api
EXPOSE 8088
CMD ["/app/zxy-panel-api"]
