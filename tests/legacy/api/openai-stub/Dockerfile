FROM golang:1.27.2-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /openai-stub .

FROM alpine:3.24
COPY --from=builder /openai-stub /openai-stub
EXPOSE 8090
CMD ["/openai-stub"]
