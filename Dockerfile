# Этап 1: Сборка бинарника
FROM golang:1.27 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o habitat main.go

# Этап 2: Финальный минимальный образ
FROM alpine:latest
WORKDIR /root/
# Копируем только скомпилированный бинарник из этапа сборки
COPY --from=builder /app/habitat .
COPY --from=builder /app/migrations ./migrations
EXPOSE 8080
CMD ["./habitat"]
