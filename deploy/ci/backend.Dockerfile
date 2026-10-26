FROM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN go build -trimpath -ldflags='-s -w' -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /app
COPY --from=build --chmod=755 /out/bot /app/bot
COPY --chmod=644 configs/values_ci.yaml /app/configs/values_ci.yaml
COPY --from=build /usr/share/zoneinfo/Europe/Moscow /usr/share/zoneinfo/Europe/Moscow
ENV TZ=Europe/Moscow CONFIG_FILE=values_ci.yaml
USER 65532:65532
EXPOSE 3000
ENTRYPOINT ["/app/bot"]
