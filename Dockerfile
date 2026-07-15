FROM golang:1.25.1-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY example/go.mod example/go.sum ./example/
RUN go mod download && cd example && go mod download

COPY . .
RUN cd example && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aggo-sse ./sse

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl fonts-noto-cjk tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 aggo \
    && useradd --uid 10001 --gid aggo --create-home --shell /usr/sbin/nologin aggo

WORKDIR /app
COPY --from=builder /out/aggo-sse /app/aggo-sse

ENV PORT=8080 \
    PDF_FONT_PATH=/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc

EXPOSE 8080
USER aggo

HEALTHCHECK --interval=15s --timeout=4s --start-period=45s --retries=4 \
    CMD curl --fail --silent --show-error http://127.0.0.1:8080/ready || exit 1

ENTRYPOINT ["/app/aggo-sse"]