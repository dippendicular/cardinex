FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/cardinex ./cmd/cardinex

FROM alpine:3.20
COPY --from=build /out/cardinex /usr/local/bin/cardinex
EXPOSE 8080
ENTRYPOINT ["cardinex"]
