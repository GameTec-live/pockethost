# syntax=docker/dockerfile:1

FROM oven/bun:1.3.12 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web ./
RUN bun run build

FROM golang:1.26.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/site/dist ./internal/site/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pockethost .

FROM scratch
COPY --from=build /out/pockethost /pockethost
VOLUME ["/pb_data"]
EXPOSE 8090
ENV POCKETHOST_DATA_DIR=/pb_data
ENTRYPOINT ["/pockethost"]
CMD ["serve", "--http", "0.0.0.0:8090"]
