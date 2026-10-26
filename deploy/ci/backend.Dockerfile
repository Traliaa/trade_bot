FROM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY configs/values_ci.yaml /src/values_ci.yaml
COPY deploy/ci/configcheck.go /src/deploy/ci/configcheck.go
RUN install -d -m 0755 /out/app/configs \
 && install -m 0644 /src/values_ci.yaml /out/app/configs/values_ci.yaml \
 && go build -trimpath -ldflags='-s -w' -o /out/app/bot ./cmd/bot \
 && go build -trimpath -o /out/configcheck ./deploy/ci/configcheck.go

FROM scratch AS checkbin
COPY --from=build /out/configcheck /configcheck

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99 AS runtime
WORKDIR /app
COPY --from=build /out/app /app
COPY --from=build /usr/share/zoneinfo/Europe/Moscow /usr/share/zoneinfo/Europe/Moscow
ENV TZ=Europe/Moscow CONFIG_FILE=values_ci.yaml
USER 65532:65532
EXPOSE 3000
ENTRYPOINT ["/app/bot"]
