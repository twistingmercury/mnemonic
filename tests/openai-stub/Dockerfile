FROM golang:1.27.3-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /openai-stub .

FROM alpine:3.24
COPY --from=builder /openai-stub /openai-stub
EXPOSE 8090
HEALTHCHECK --interval=1s --timeout=2s --retries=10 CMD wget -qO- http://localhost:8090/healthz || exit 1
CMD ["/openai-stub"]
