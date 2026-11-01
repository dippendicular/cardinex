FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/cardinex ./service/cmd/cardinex && go build -o /out/cardinex-mcp ./service/cmd/cardinex-mcp

FROM alpine:3.20
COPY --from=build /out/cardinex /out/cardinex-mcp /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["cardinex"]
