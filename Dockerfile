FROM golang:1.25 AS builder-deps
LABEL maintainer="Pico Maintainers <hello@pico.sh>"

WORKDIR /app

RUN apt-get update
RUN apt-get install -y git ca-certificates

COPY go.* ./

RUN go mod download

FROM builder-deps AS builder

COPY . .

ENV CGO_ENABLED=1
ENV LDFLAGS="-s -w"

RUN go build -ldflags "$LDFLAGS" -o /go/bin/patchbin ./cmd/patchbin

FROM scratch AS release

WORKDIR /app
ENV TERM="xterm-256color"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/patchbin ./patchbin

CMD ["/app/patchbin"]
