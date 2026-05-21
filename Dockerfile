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
#
# External decoders the upload pipeline shells out to:
#   - libheif-tools : heif-convert for HEIC/HEIF originals
#   - exiftool      : XMP sidecar writer for the EXIF edit endpoint and the
#                     upload-time EXIF reader (with a pure-Go fallback)
#   - libraw-tools  : dcraw_emu used by the dcraw-shim.sh wrapper below.
#                     Alpine removed the upstream dcraw package (Dave Coffin
#                     stopped maintaining it years ago; LibRaw is the
#                     modern replacement), so the shim translates the
#                     `dcraw -c -w -h` invocation our Go code uses into a
#                     dcraw_emu call that emits PPM on stdout.
COPY scripts/install-fonts.sh /tmp/install-fonts.sh
COPY scripts/dcraw-shim.sh /usr/local/bin/dcraw
RUN apk update && \
    apk add --no-cache ca-certificates tzdata curl unzip postgresql-client \
    texlive-luatex texmf-dist-latexrecommended texmf-dist-fontsrecommended texmf-dist-langczechslovak texmf-dist-pictures \
    libheif-tools exiftool libraw-tools && \
    chmod 0755 /usr/local/bin/dcraw && \
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

# Ensure clean SIGTERM delivery so the HTTP server can drain in-flight
# requests and the DB pool can close cleanly.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/photo-sorter"]
CMD ["serve"]
