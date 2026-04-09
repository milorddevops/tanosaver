FROM golang:alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
# go mod vendor
# Cкачать все зависимости себе под ноги
COPY vendor/ ./vendor/

RUN go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -tags "containers_image_openpgp exclude_graphdriver_btrfs exclude_graphdriver_devicemapper" -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o tanos-saver ./cmd/tanos-saver
# Если -> go mod vendor
#RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -tags "containers_image_openpgp exclude_graphdriver_btrfs exclude_graphdriver_devicemapper" -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o tanos-saver ./cmd/tanos-saver

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/tanos-saver /usr/local/bin/

RUN chmod +x /usr/local/bin/tanos-saver

ENTRYPOINT ["tanos-saver"]
CMD ["--help"]
