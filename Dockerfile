FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /ical-formatter-api main.go

FROM alpine:3.20

LABEL org.opencontainers.image.source=https://github.com/krootjes/ical-formatter-api

RUN apk add --no-cache tzdata

WORKDIR /app

COPY --from=builder /ical-formatter-api /ical-formatter-api

EXPOSE 8080

CMD ["/ical-formatter-api"]