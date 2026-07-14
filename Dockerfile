FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /build/rule-engine ./cmd/main.go

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl

ENV TZ=Asia/Shanghai

COPY --from=builder /build/rule-engine .

ENV CONFIG_FILE=/etc/rule-engine/config.yaml

EXPOSE 9090

ENTRYPOINT ["/app/rule-engine"]
CMD ["--config-file=/etc/rule-engine/config.yaml"]
