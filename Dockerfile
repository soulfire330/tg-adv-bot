FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Кеш сборки: без него каждый rebuild заново компилирует gotd/td/tg —
# 3126 сгенерированных файлов в одном пакете, пик ~5 ГБ RAM.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tg-adv-bot ./cmd/app

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/tg-adv-bot /app/tg-adv-bot
COPY --from=build /src/prompt.md /app/prompt.md
ENTRYPOINT ["/app/tg-adv-bot"]
CMD ["run"]
