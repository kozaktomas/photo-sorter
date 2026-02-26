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
    apk add --no-cache ca-certificates tzdata curl \
    texlive-luatex texmf-dist-latexrecommended texmf-dist-fontsrecommended texmf-dist-langczechslovak texmf-dist-pictures && \
    # Install EBGaramond fonts from CTAN (avoids pulling huge texmf-dist-fontsextra)
    mkdir -p /usr/share/fonts/opentype/ebgaramond && \
    for f in EBGaramond-Regular.otf EBGaramond-SemiBold.otf EBGaramond-Italic.otf EBGaramond-SemiBoldItalic.otf; do \
      curl -sL -o /usr/share/fonts/opentype/ebgaramond/$f \
        https://mirrors.ctan.org/fonts/ebgaramond/opentype/$f; \
    done && \
    # Install enumitem.sty from CTAN (avoids pulling huge texmf-dist-latexextra)
    curl -sL -o /usr/share/texmf-dist/tex/latex/enumitem/enumitem.sty \
      --create-dirs \
      https://mirrors.ctan.org/macros/latex/contrib/enumitem/enumitem.sty && \
    # Pre-generate font cache and make it writable for nobody user
    mkdir -p /var/cache/luatex-cache && \
    TEXMFCACHE=/var/cache/luatex-cache luaotfload-tool --update 2>/dev/null || true && \
    chmod -R 777 /var/cache/luatex-cache && \
    apk del curl && \
    rm -rf /var/cache/apk/* && \
    mkdir /app

ENV TEXMFCACHE=/var/cache/luatex-cache

WORKDIR /app

COPY --from=backend /app/photo-sorter /app/photo-sorter

RUN chown nobody /app/photo-sorter && \
    chmod 500 /app/photo-sorter

USER nobody

EXPOSE 8080

ENTRYPOINT ["/app/photo-sorter"]
CMD ["serve"]
