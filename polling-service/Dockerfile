FROM ubuntu:latest
LABEL authors="Kristian"

ENTRYPOINT ["top", "-b"]

FROM golang:1.23.1-bookworm AS builder

WORKDIR /app

COPY polling-service/go.mod polling-service/go.sum ./
RUN go mod download

COPY polling-service/ .


RUN go build -o polling-service-app


FROM debian:bookworm-slim
WORKDIR /root/
COPY --from=builder /app/polling-service-app .
EXPOSE 8080
ENTRYPOINT ["./polling-service-app"]