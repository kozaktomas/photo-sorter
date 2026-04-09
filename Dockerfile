# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /app
COPY web/package*.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN mkdir -p internal/web/static/dist && cd web && npm run build

# Stage 2: Build Go backend
FROM golang:1.26-alpine AS backend
ENV CGO_ENABLED=0
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/internal/web/static/dist/ ./internal/web/static/dist/
ARG VERSION=dev
ARG COMMIT_SHA=unknown
RUN go build -ldflags "-s -w -X github.com/kozaktomas/photo-sorter/cmd.Version=${VERSION} -X github.com/kozaktomas/photo-sorter/cmd.CommitSHA=${COMMIT_SHA}" -o photo-sorter .

# Stage 3: Runtime
FROM alpine:3
# Font installation is delegated to scripts/install-fonts.sh — single source
# of truth shared with the `make install-fonts` dev target. Bookman Old Style
# is intentionally not installed (proprietary, not redistributable).
COPY scripts/install-fonts.sh /tmp/install-fonts.sh
RUN apk update && \
    apk add --no-cache ca-certificates tzdata curl unzip \
    texlive-luatex texmf-dist-latexrecommended texmf-dist-fontsrecommended texmf-dist-langczechslovak texmf-dist-pictures && \
    sh /tmp/install-fonts.sh /usr/share/fonts && \
    rm /tmp/install-fonts.sh && \
    # Install enumitem.sty from CTAN (avoids pulling huge texmf-dist-latexextra)
    mkdir -p /usr/share/texmf-dist/tex/latex/enumitem && \
    curl -fsSL -o /usr/share/texmf-dist/tex/latex/enumitem/enumitem.sty \
      https://mirrors.ctan.org/macros/latex/contrib/enumitem/enumitem.sty && \
    # Update TeX file database so lualatex can find manually-installed packages
    mktexlsr && \
    # Pre-generate font cache for luaotfload
    mkdir -p /var/cache/luatex-cache && \
    TEXMFCACHE=/var/cache/luatex-cache TEXMFVAR=/var/cache/luatex-cache luaotfload-tool --update && \
    chmod -R 777 /var/cache/luatex-cache && \
    apk del curl unzip && \
    rm -rf /var/cache/apk/* && \
    mkdir /app

ENV TEXMFCACHE=/var/cache/luatex-cache
ENV TEXMFVAR=/var/cache/luatex-cache

WORKDIR /app

COPY --from=backend /app/photo-sorter /app/photo-sorter

RUN chown nobody /app/photo-sorter && \
    chmod 500 /app/photo-sorter

USER nobody

EXPOSE 8080

# Ensure clean SIGTERM delivery for graceful shutdown (saves HNSW indexes).
# In docker-compose.yml, set stop_grace_period: 60s to allow time for index persistence on slow hardware.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/photo-sorter"]
CMD ["serve"]
