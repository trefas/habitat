FROM golang:1.27 AS builder
WORKDIR /app

# Копируем go.mod и go.sum для кэширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь исходный код
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o habitat ./cmd/api

# Финальный образ
FROM gcr.io/distroless/static-debian12
WORKDIR /app

# Копируем бинарник
COPY --from=builder /app/habitat .

# ВАЖНО: копируем миграции
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080
CMD ["./habitat"]