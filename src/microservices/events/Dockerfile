# Этап сборки (builder)
FROM golang:1.25-alpine AS builder

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем go.mod и go.sum для кэширования зависимостей
COPY go.mod go.sum ./

# Скачиваем зависимости
RUN go mod download

# Копируем исходный код
COPY main.go ./

# Собираем бинарник (статическая линковка для Alpine)
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o events .

# Финальный этап (runtime)
FROM alpine:latest

# Устанавливаем ca-certificates (если нужно для HTTPS-запросов, хотя в этом случае не обязательно)
RUN apk --no-cache add ca-certificates

# Устанавливаем рабочую директорию
WORKDIR /root/

# Копируем бинарник из этапа сборки
COPY --from=builder /app/events .

# Экспонируем порт
EXPOSE 8082

# Устанавливаем переменную окружения по умолчанию (можно переопределить в docker-compose)
ENV KAFKA_BROKER=localhost:9092

# Запускаем приложение
CMD ["./events"]