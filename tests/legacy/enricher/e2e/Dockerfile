FROM ghcr.io/twistingmercury/golang-tooling:go1.27.2

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache curl bash coreutils

WORKDIR /e2e

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY test-runner.sh /test-runner.sh

RUN chmod +x /test-runner.sh

ENTRYPOINT ["/test-runner.sh"]
