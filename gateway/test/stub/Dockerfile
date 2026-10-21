FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /stub .

FROM scratch
COPY --from=build /stub /stub
ENTRYPOINT ["/stub"]
