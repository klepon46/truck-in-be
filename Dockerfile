FROM golang:1.25-alpine AS source

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

FROM source AS test
RUN go test ./...

FROM source AS compile
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && addgroup -S unitlog \
    && adduser -S -G unitlog unitlog

WORKDIR /app
COPY --from=compile /out/server ./server
COPY --from=compile /out/migrate ./migrate
COPY --from=compile /src/migrations ./migrations

USER unitlog
EXPOSE 8105

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=12 \
    CMD wget -q -O /dev/null http://127.0.0.1:8105/health || exit 1

ENTRYPOINT ["/app/server"]
