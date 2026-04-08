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
RUN apk update && \
    apk add --no-cache ca-certificates tzdata curl unzip \
    texlive-luatex texmf-dist-latexrecommended texmf-dist-fontsrecommended texmf-dist-langczechslovak texmf-dist-pictures && \
    # Install PT Serif + PT Sans from CTAN (paratype package)
    # Zip layout: paratype/truetype/*.ttf — use find to avoid path fragility
    mkdir -p /usr/share/fonts/truetype/paratype && \
    curl -fsSL -o /tmp/paratype.zip \
      https://mirrors.ctan.org/fonts/paratype.zip && \
    unzip -q -o /tmp/paratype.zip -d /tmp/paratype && \
    find /tmp/paratype -name 'PTF55F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTF75F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTF56F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTF76F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTS55F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTS75F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTS56F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    find /tmp/paratype -name 'PTS76F.ttf' -exec cp {} /usr/share/fonts/truetype/paratype/ \; && \
    rm -rf /tmp/paratype /tmp/paratype.zip && \
    # Install EBGaramond fonts from CTAN (avoids pulling huge texmf-dist-fontsextra)
    mkdir -p /usr/share/fonts/opentype/ebgaramond && \
    for f in EBGaramond-Regular.otf EBGaramond-SemiBold.otf EBGaramond-Italic.otf EBGaramond-SemiBoldItalic.otf; do \
      curl -fsSL -o /usr/share/fonts/opentype/ebgaramond/$f \
        https://mirrors.ctan.org/fonts/ebgaramond/opentype/$f || exit 1; \
    done && \
    # Install SourceSans3 fonts from CTAN (package: sourcesans)
    mkdir -p /usr/share/fonts/opentype/sourcesans3 && \
    for f in SourceSans3-Regular.otf SourceSans3-Semibold.otf SourceSans3-RegularIt.otf SourceSans3-SemiboldIt.otf; do \
      curl -fsSL -o /usr/share/fonts/opentype/sourcesans3/$f \
        https://mirrors.ctan.org/fonts/sourcesans/fonts/$f || exit 1; \
    done && \
    # Install Libertinus Serif from GitHub release
    mkdir -p /usr/share/fonts/opentype/libertinus && \
    curl -fsSL -L -o /tmp/libertinus.zip \
      "https://github.com/alerque/libertinus/releases/download/v7.051/Libertinus-7.051.zip" && \
    unzip -q -o /tmp/libertinus.zip -d /tmp/libertinus && \
    cp /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-Regular.otf \
       /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-Bold.otf \
       /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-Italic.otf \
       /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-BoldItalic.otf \
       /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-Semibold.otf \
       /tmp/libertinus/Libertinus-7.051/static/OTF/LibertinusSerif-SemiboldItalic.otf \
       /usr/share/fonts/opentype/libertinus/ && \
    rm -rf /tmp/libertinus /tmp/libertinus.zip && \
    # ---------------------------------------------------------------
    # Google Fonts - Variable fonts (2 files each: upright + italic)
    # Variable .ttf files contain all weights; fontspec handles them.
    # ---------------------------------------------------------------
    # Lora (serif)
    mkdir -p /usr/share/fonts/truetype/lora && \
    curl -fsSL -o "/usr/share/fonts/truetype/lora/Lora[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/lora/Lora%5Bwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/lora/Lora-Italic[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/lora/Lora-Italic%5Bwght%5D.ttf" && \
    # Merriweather (serif)
    mkdir -p /usr/share/fonts/truetype/merriweather && \
    curl -fsSL -o "/usr/share/fonts/truetype/merriweather/Merriweather[opsz,wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/merriweather/Merriweather%5Bopsz%2Cwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/merriweather/Merriweather-Italic[opsz,wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/merriweather/Merriweather-Italic%5Bopsz%2Cwdth%2Cwght%5D.ttf" && \
    # Noto Serif - Latin subset (serif)
    mkdir -p /usr/share/fonts/truetype/notoserif && \
    curl -fsSL -o "/usr/share/fonts/truetype/notoserif/NotoSerif[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/notoserif/NotoSerif%5Bwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/notoserif/NotoSerif-Italic[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/notoserif/NotoSerif-Italic%5Bwdth%2Cwght%5D.ttf" && \
    # Crimson Pro (serif)
    mkdir -p /usr/share/fonts/truetype/crimsonpro && \
    curl -fsSL -o "/usr/share/fonts/truetype/crimsonpro/CrimsonPro[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/crimsonpro/CrimsonPro%5Bwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/crimsonpro/CrimsonPro-Italic[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/crimsonpro/CrimsonPro-Italic%5Bwght%5D.ttf" && \
    # Source Serif 4 (serif)
    mkdir -p /usr/share/fonts/truetype/sourceserif4 && \
    curl -fsSL -o "/usr/share/fonts/truetype/sourceserif4/SourceSerif4[opsz,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/sourceserif4/SourceSerif4%5Bopsz%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/sourceserif4/SourceSerif4-Italic[opsz,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/sourceserif4/SourceSerif4-Italic%5Bopsz%2Cwght%5D.ttf" && \
    # Cormorant Garamond (serif)
    mkdir -p /usr/share/fonts/truetype/cormorantgaramond && \
    curl -fsSL -o "/usr/share/fonts/truetype/cormorantgaramond/CormorantGaramond-Italic[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/cormorantgaramond/CormorantGaramond-Italic%5Bwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/cormorantgaramond/CormorantGaramond[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/cormorantgaramond/CormorantGaramond%5Bwght%5D.ttf" && \
    # Bitter (serif)
    mkdir -p /usr/share/fonts/truetype/bitter && \
    curl -fsSL -o "/usr/share/fonts/truetype/bitter/Bitter[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/bitter/Bitter%5Bwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/bitter/Bitter-Italic[wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/bitter/Bitter-Italic%5Bwght%5D.ttf" && \
    # Noto Sans - Latin subset (sans-serif)
    mkdir -p /usr/share/fonts/truetype/notosans && \
    curl -fsSL -o "/usr/share/fonts/truetype/notosans/NotoSans[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/notosans/NotoSans%5Bwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/notosans/NotoSans-Italic[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/notosans/NotoSans-Italic%5Bwdth%2Cwght%5D.ttf" && \
    # Open Sans (sans-serif)
    mkdir -p /usr/share/fonts/truetype/opensans && \
    curl -fsSL -o "/usr/share/fonts/truetype/opensans/OpenSans[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/opensans/OpenSans%5Bwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/opensans/OpenSans-Italic[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/opensans/OpenSans-Italic%5Bwdth%2Cwght%5D.ttf" && \
    # Roboto (sans-serif)
    mkdir -p /usr/share/fonts/truetype/roboto && \
    curl -fsSL -o "/usr/share/fonts/truetype/roboto/Roboto-Italic[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/roboto/Roboto-Italic%5Bwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/roboto/Roboto[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/roboto/Roboto%5Bwdth%2Cwght%5D.ttf" && \
    # Inter (sans-serif)
    mkdir -p /usr/share/fonts/truetype/inter && \
    curl -fsSL -o "/usr/share/fonts/truetype/inter/Inter[opsz,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/inter/Inter%5Bopsz%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/inter/Inter-Italic[opsz,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/inter/Inter-Italic%5Bopsz%2Cwght%5D.ttf" && \
    # IBM Plex Sans (sans-serif)
    mkdir -p /usr/share/fonts/truetype/ibmplexsans && \
    curl -fsSL -o "/usr/share/fonts/truetype/ibmplexsans/IBMPlexSans-Italic[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/ibmplexsans/IBMPlexSans-Italic%5Bwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/ibmplexsans/IBMPlexSans[wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/ibmplexsans/IBMPlexSans%5Bwdth%2Cwght%5D.ttf" && \
    # Nunito Sans (sans-serif)
    mkdir -p /usr/share/fonts/truetype/nunitosans && \
    curl -fsSL -o "/usr/share/fonts/truetype/nunitosans/NunitoSans-Italic[YTLC,opsz,wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/nunitosans/NunitoSans-Italic%5BYTLC%2Copsz%2Cwdth%2Cwght%5D.ttf" && \
    curl -fsSL -o "/usr/share/fonts/truetype/nunitosans/NunitoSans[YTLC,opsz,wdth,wght].ttf" \
      "https://github.com/google/fonts/raw/main/ofl/nunitosans/NunitoSans%5BYTLC%2Copsz%2Cwdth%2Cwght%5D.ttf" && \
    # ---------------------------------------------------------------
    # Google Fonts - Static fonts (4 files each: Regular, Bold, Italic, BoldItalic)
    # ---------------------------------------------------------------
    # Lato (sans-serif)
    mkdir -p /usr/share/fonts/truetype/lato && \
    for f in Lato-Regular Lato-Bold Lato-Italic Lato-BoldItalic; do \
      curl -fsSL -o /usr/share/fonts/truetype/lato/$f.ttf \
        "https://github.com/google/fonts/raw/main/ofl/lato/$f.ttf" || exit 1; \
    done && \
    # Fira Sans (sans-serif)
    mkdir -p /usr/share/fonts/truetype/firasans && \
    for f in FiraSans-Regular FiraSans-Bold FiraSans-Italic FiraSans-BoldItalic; do \
      curl -fsSL -o /usr/share/fonts/truetype/firasans/$f.ttf \
        "https://github.com/google/fonts/raw/main/ofl/firasans/$f.ttf" || exit 1; \
    done && \
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
