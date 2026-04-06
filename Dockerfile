FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /atFormatterAPI main.go

FROM alpine:3.20

WORKDIR /app

COPY --from=builder /atFormatterAPI /atFormatterAPI
COPY config.json /app/config.json

EXPOSE 8080

CMD ["/atFormatterAPI"]