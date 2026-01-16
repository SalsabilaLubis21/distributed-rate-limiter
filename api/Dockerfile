# Stage 1: Build the Go application
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY vendor ./vendor

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -mod=vendor -o /main .

# Stage 2: Create the final image
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /main .

EXPOSE 8080

CMD ["./main"]