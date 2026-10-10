FROM golang:1.26.6-trixie AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY app ./app
COPY lib ./lib

ENV CGO_ENABLED=1
RUN go build -mod=vendor -trimpath -buildvcs=false \
    -tags 'netgo osusergo' \
    -ldflags "-extldflags '-static'" \
    -o /out/victoria-traces ./app/victoria-traces

FROM debian:13-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl openssl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/victoria-traces /victoria-traces

ENTRYPOINT ["/victoria-traces"]