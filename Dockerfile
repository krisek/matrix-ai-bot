FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /matrix-ai-bot .

FROM gcr.io/distroless/static
COPY --from=builder /matrix-ai-bot /matrix-ai-bot
USER 65534:65534
ENTRYPOINT ["/matrix-ai-bot"]
