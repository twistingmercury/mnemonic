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
COPY src/cmd/enricher ./cmd/enricher
COPY src/internal/enricher ./internal/enricher

# Run linters, formatters, security scanners, etc
RUN goimports -w .
RUN CGO_ENABLED=0 golangci-lint run
RUN CGO_ENABLED=0 govulncheck ./cmd/... ./internal/...
RUN CGO_ENABLED=0 gosec -quiet -exclude-dir=tests ./...
RUN CGO_ENABLED=1 go test -race -v ./cmd/enricher/... ./internal/... -coverprofile=coverage.out

ARG LD_FLAGS="-s -w \
    -X 'github.com/twistingmercury/mnemonic/internal/enricher/version.version=${BUILD_VER}' \
    -X 'github.com/twistingmercury/mnemonic/internal/enricher/version.buildDate=${BUILD_DATE}' \
    -X 'github.com/twistingmercury/mnemonic/internal/enricher/version.commit=${BUILD_COMMIT}'"

RUN CGO_ENABLED=0 go build --ldflags "${LD_FLAGS}" -o /out/mnemonic-enricher ./cmd/enricher


#------------------------------------------------------------
# Stage 2: Runtime - minimal production image
#------------------------------------------------------------
FROM scratch AS final

ARG BUILD_VER=dev
ARG BUILD_DATE=unknown
ARG BUILD_COMMIT=unknown

LABEL org.opencontainers.image.title="mnemonic-enricher" \
    org.opencontainers.image.description="Backend server providing enrichment services for patterns within Mnemonic MCP server." \
    org.opencontainers.image.source="https://github.com/twistingmercury/mnemonic" \
    org.opencontainers.image.licenses="FSL-1.1-ALv2" \
    org.opencontainers.image.authors="Jeremy K. Johnson" \
    org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.revision="${BUILD_COMMIT}" \
    org.opencontainers.image.version="${BUILD_VER}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mnemonic-enricher /mnemonic-enricher

EXPOSE 8080

ENTRYPOINT ["/mnemonic-enricher"]