#------------------------------------------------------------
# Stage 1: Run linters, formatters, security scanners, 
# unit tests, then build binary
#------------------------------------------------------------
FROM ghcr.io/twistingmercury/golang-tooling:go1.27.3 AS build

ENV GOARCH=amd64 \
    GOOS=linux

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

WORKDIR /src

COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/.golangci.yml ./
COPY src/cmd/mcp ./cmd/mcp
COPY src/internal/mcp ./internal/mcp

# Run linters, formatters, security scanners, etc
RUN goimports -w .
RUN CGO_ENABLED=0 golangci-lint run
RUN CGO_ENABLED=0 govulncheck ./cmd/... ./internal/...
RUN CGO_ENABLED=0 gosec -quiet -exclude-dir=tests ./...
RUN CGO_ENABLED=1 go test -race -v ./cmd/mcp/... ./internal/...

ARG LD_FLAGS="-s -w \
    -X 'github.com/twistingmercury/mnemonic/internal/mcp/version.version=${BUILD_VER}' \
    -X 'github.com/twistingmercury/mnemonic/internal/mcp/version.buildDate=${BUILD_DATE}' \
    -X 'github.com/twistingmercury/mnemonic/internal/mcp/version.commit=${BUILD_COMMIT}'"

RUN CGO_ENABLED=0 go build --ldflags "${LD_FLAGS}" -o /out/mnemonic ./cmd/mcp


#------------------------------------------------------------
# Stage 2: Runtime - minimal production image
#------------------------------------------------------------
FROM scratch AS final

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

LABEL org.opencontainers.image.title="mnemonic" \
    org.opencontainers.image.description="Mnemonic MCP service for semantic pattern search and retrieval" \
    org.opencontainers.image.source="https://github.com/twistingmercury/mnemonic" \
    org.opencontainers.image.licenses="FSL-1.1-ALv2" \
    org.opencontainers.image.authors="Jeremy K. Johnson" \
    org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.revision="${BUILD_COMMIT}" \
    org.opencontainers.image.version="${BUILD_VER}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mnemonic /mnemonic

EXPOSE 8080 8081 9090

ENTRYPOINT ["/mnemonic"]
