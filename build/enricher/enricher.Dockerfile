#------------------------------------------------------------
# Stage 1: Run linters, formatters, security scanners, 
# unit tests, then build binary
#------------------------------------------------------------
FROM ghcr.io/twistingmercury/golang-tooling:go1.27.2 AS build

ENV CGO_ENABLED=0 \
    GOARCH=amd64 \
    GOOS=linux

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Run linters, formatters, security scanners, etc
RUN goimports -w .
RUN golangci-lint run
RUN govulncheck ./cmd/... ./internal/...
RUN gosec -quiet -exclude-dir=tests ./...
RUN go test -v ./cmd/main/... ./internal/... -coverprofile=coverage.out

ARG LD_FLAGS="-s -w \
    -X 'github.com/twistingmercury/librarian/internal/enricher/version.version=${BUILD_VER}' \
    -X 'github.com/twistingmercury/librarian/internal/enricher/version.buildDate=${BUILD_DATE}' \
    -X 'github.com/twistingmercury/librarian/internal/enricher/version.commit=${BUILD_COMMIT}'"

RUN go build --ldflags "${LD_FLAGS}" -o /out/mnemonic-enricher cmd/main/main.go


#------------------------------------------------------------
# Stage 2: Runtime - minimal production image
#------------------------------------------------------------
FROM scratch AS final

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

LABEL org.opencontainers.image.title="mnemonic-enricher" \
    org.opencontainers.image.description="Backend server providing enrichment services for patterns within Mnemonic MCP server." \
    org.opencontainers.image.source="https://github.com/twistingmercury/librarian-enricher" \
    org.opencontainers.image.licenses="Apache-2.0" \
    org.opencontainers.image.authors="Jeremy K. Johnson" \
    org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.revision="${BUILD_COMMIT}" \
    org.opencontainers.image.version="${BUILD_VER}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mnemonic-enricher /mnemonic-enricher

EXPOSE 8080

ENTRYPOINT ["/mnemonic-enricher"]