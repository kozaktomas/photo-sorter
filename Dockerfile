# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /app
COPY web/package*.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN mkdir -p internal/web/static/dist && cd web && npm run build

# Stage 2: Build Go backend
FROM golang:1.25-alpine AS backend
ENV CGO_ENABLED=0
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/internal/web/static/dist/ ./internal/web/static/dist/
RUN go build -ldflags "-s -w" -o photo-sorter .

# Stage 3: Runtime
FROM alpine:3
RUN apk update && \
    apk add --no-cache ca-certificates tzdata && \
    rm -rf /var/cache/apk/* && \
    mkdir /app

WORKDIR /app

COPY --from=backend /app/photo-sorter /app/photo-sorter

RUN chown nobody /app/photo-sorter && \
    chmod 500 /app/photo-sorter

USER nobody

EXPOSE 8080

ENTRYPOINT ["/app/photo-sorter"]
CMD ["serve"]
