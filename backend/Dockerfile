FROM golang:1.23 AS builder
WORKDIR /src
COPY . .
RUN go test ./... && go build -o /out/zxy-panel-api ./cmd/server

FROM debian:bookworm-slim
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=builder /out/zxy-panel-api /app/zxy-panel-api
EXPOSE 8088
CMD ["/app/zxy-panel-api"]
