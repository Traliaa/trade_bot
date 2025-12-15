# Stage 1: builder
FROM golang:1.25 AS builder
WORKDIR /build

# Скачиваем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь проект
COPY . .

# Собираем Go-бота
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

# Ставим goose (в builder)
RUN go install github.com/pressly/goose/v3/cmd/goose@latest


# Stage 2: runtime
FROM golang:1.25 AS runtime
WORKDIR /app

# Копируем бота
COPY --from=builder /build/bot .
# Копируем goose
COPY --from=builder /go/bin/goose /usr/local/bin/goose

# Копируем миграции и конфиги
COPY --from=builder /build/migrations ./migrations
COPY --from=builder /build/configs ./configs

EXPOSE 3000

# При запуске контейнера:
# 1. прогоняем миграции
# 2. стартуем бота
ENTRYPOINT sh -c "goose -dir ./migrations postgres \"$DATABASE_DSN\" up &&./bot"
#&& river migrate-up --database-url \"$DATABASE_DSN\" &&./bot"
