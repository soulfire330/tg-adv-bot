FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tg-adv-bot ./cmd/app

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/tg-adv-bot /app/tg-adv-bot
COPY --from=build /src/prompt.md /app/prompt.md
ENTRYPOINT ["/app/tg-adv-bot"]
CMD ["run"]
