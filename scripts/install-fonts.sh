#!/bin/sh
#
# install-fonts.sh — Install all free fonts referenced from
# internal/latex/fonts.go. This script is the single source of truth used
# by both the Dockerfile (production runtime) and the Makefile target
# `make install-fonts` (Raspberry Pi / dev environments).
#
# Usage:
#   scripts/install-fonts.sh <DEST_ROOT>
#
#   Docker (production):  scripts/install-fonts.sh /usr/share/fonts
#   Dev (Raspberry Pi):   scripts/install-fonts.sh ~/.local/share/fonts/photo-sorter
#
# Requires: curl, unzip, find. Caller is responsible for any post-install
# cache refresh (fc-cache, luaotfload-tool, mktexlsr).
#
# NOTE: Bookman Old Style is a proprietary Microsoft font and cannot be
# redistributed. It is registered in fonts.go but this script does NOT
# install it. To enable it, drop the licensed TTF files
# (BOOKOS.TTF / BOOKOSB.TTF / BOOKOSI.TTF / BOOKOSBI.TTF) into:
#
#     <DEST_ROOT>/truetype/bookman-old-style/
#
# and rerun "fc-cache -f" + "luaotfload-tool --update --force".

set -eu

DEST_ROOT="${1:-}"
if [ -z "$DEST_ROOT" ]; then
  echo "usage: $0 <DEST_ROOT>" >&2
  exit 1
fi

# Expand a leading literal "~" if the caller passed it as a string.
case "$DEST_ROOT" in
  "~"*) DEST_ROOT="${HOME}${DEST_ROOT#\~}" ;;
esac

command -v curl  >/dev/null 2>&1 || { echo "error: curl is required"  >&2; exit 1; }
command -v unzip >/dev/null 2>&1 || { echo "error: unzip is required" >&2; exit 1; }
command -v find  >/dev/null 2>&1 || { echo "error: find is required"  >&2; exit 1; }

mkdir -p "$DEST_ROOT/truetype" "$DEST_ROOT/opentype"
echo "Installing fonts to: $DEST_ROOT"

GFONTS_BASE="https://github.com/google/fonts/raw/main/ofl"

# fetch_to <dir> <filename> <url> — idempotent download with retries.
# CTAN mirrors.ctan.org redirects to regional mirrors; some occasionally
# serve bad TLS certs, so we retry on all failures including SSL errors.
fetch_to() {
  _dir="$1"; _name="$2"; _url="$3"
  mkdir -p "$_dir"
  if [ -s "$_dir/$_name" ]; then
    echo "  skip: $_name"
    return 0
  fi
  echo "  get:  $_name"
  curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors -o "$_dir/$_name" "$_url"
}

# === CTAN: ParaType (PT Serif + PT Sans, 8 static TTFs in a zip) ============
echo "PT Serif + PT Sans..."
PARATYPE_DIR="$DEST_ROOT/truetype/paratype"
mkdir -p "$PARATYPE_DIR"
PARATYPE_FILES="PTF55F.ttf PTF75F.ttf PTF56F.ttf PTF76F.ttf PTS55F.ttf PTS75F.ttf PTS56F.ttf PTS76F.ttf"
NEED_PARATYPE=0
for f in $PARATYPE_FILES; do
  [ -s "$PARATYPE_DIR/$f" ] || NEED_PARATYPE=1
done
if [ "$NEED_PARATYPE" = "1" ]; then
  echo "  get:  paratype.zip (8 TTFs)"
  PARATYPE_TMP=$(mktemp -d)
  curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
    -o "$PARATYPE_TMP/paratype.zip" https://mirrors.ctan.org/fonts/paratype.zip
  unzip -q -o "$PARATYPE_TMP/paratype.zip" -d "$PARATYPE_TMP"
  for f in $PARATYPE_FILES; do
    find "$PARATYPE_TMP" -name "$f" -exec cp {} "$PARATYPE_DIR/" \;
  done
  rm -rf "$PARATYPE_TMP"
else
  echo "  skip: ParaType (all 8 files present)"
fi

# === CTAN: EB Garamond (4 OTFs) =============================================
echo "EB Garamond..."
for f in EBGaramond-Regular.otf EBGaramond-SemiBold.otf EBGaramond-Italic.otf EBGaramond-SemiBoldItalic.otf; do
  fetch_to "$DEST_ROOT/opentype/ebgaramond" "$f" \
    "https://mirrors.ctan.org/fonts/ebgaramond/opentype/$f"
done

# === CTAN: Source Sans 3 (4 OTFs) ===========================================
echo "Source Sans 3..."
for f in SourceSans3-Regular.otf SourceSans3-Semibold.otf SourceSans3-RegularIt.otf SourceSans3-SemiboldIt.otf; do
  fetch_to "$DEST_ROOT/opentype/sourcesans3" "$f" \
    "https://mirrors.ctan.org/fonts/sourcesans/fonts/$f"
done

# === GitHub: Libertinus Serif (6 OTFs from release zip) =====================
echo "Libertinus Serif..."
LIBERTINUS_DIR="$DEST_ROOT/opentype/libertinus"
mkdir -p "$LIBERTINUS_DIR"
LIBERTINUS_FILES="LibertinusSerif-Regular.otf LibertinusSerif-Bold.otf LibertinusSerif-Italic.otf LibertinusSerif-BoldItalic.otf LibertinusSerif-Semibold.otf LibertinusSerif-SemiboldItalic.otf"
NEED_LIBERTINUS=0
for f in $LIBERTINUS_FILES; do
  [ -s "$LIBERTINUS_DIR/$f" ] || NEED_LIBERTINUS=1
done
if [ "$NEED_LIBERTINUS" = "1" ]; then
  echo "  get:  Libertinus-7.051.zip (6 OTFs)"
  LIBERTINUS_TMP=$(mktemp -d)
  curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors -o "$LIBERTINUS_TMP/libertinus.zip" \
    "https://github.com/alerque/libertinus/releases/download/v7.051/Libertinus-7.051.zip"
  unzip -q -o "$LIBERTINUS_TMP/libertinus.zip" -d "$LIBERTINUS_TMP"
  for f in $LIBERTINUS_FILES; do
    cp "$LIBERTINUS_TMP/Libertinus-7.051/static/OTF/$f" "$LIBERTINUS_DIR/"
  done
  rm -rf "$LIBERTINUS_TMP"
else
  echo "  skip: Libertinus Serif (all 6 files present)"
fi

# === Google Fonts: variable fonts (upright + italic) ========================
echo "Lora..."
fetch_to "$DEST_ROOT/truetype/lora" "Lora-wght.ttf"        "$GFONTS_BASE/lora/Lora%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/lora" "Lora-Italic-wght.ttf" "$GFONTS_BASE/lora/Lora-Italic%5Bwght%5D.ttf"

echo "Merriweather..."
fetch_to "$DEST_ROOT/truetype/merriweather" "Merriweather-opsz-wdth-wght.ttf"        "$GFONTS_BASE/merriweather/Merriweather%5Bopsz%2Cwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/merriweather" "Merriweather-Italic-opsz-wdth-wght.ttf" "$GFONTS_BASE/merriweather/Merriweather-Italic%5Bopsz%2Cwdth%2Cwght%5D.ttf"

echo "Noto Serif..."
fetch_to "$DEST_ROOT/truetype/notoserif" "NotoSerif-wdth-wght.ttf"        "$GFONTS_BASE/notoserif/NotoSerif%5Bwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/notoserif" "NotoSerif-Italic-wdth-wght.ttf" "$GFONTS_BASE/notoserif/NotoSerif-Italic%5Bwdth%2Cwght%5D.ttf"

echo "Crimson Pro..."
fetch_to "$DEST_ROOT/truetype/crimsonpro" "CrimsonPro-wght.ttf"        "$GFONTS_BASE/crimsonpro/CrimsonPro%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/crimsonpro" "CrimsonPro-Italic-wght.ttf" "$GFONTS_BASE/crimsonpro/CrimsonPro-Italic%5Bwght%5D.ttf"

echo "Source Serif 4..."
fetch_to "$DEST_ROOT/truetype/sourceserif4" "SourceSerif4-opsz-wght.ttf"        "$GFONTS_BASE/sourceserif4/SourceSerif4%5Bopsz%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/sourceserif4" "SourceSerif4-Italic-opsz-wght.ttf" "$GFONTS_BASE/sourceserif4/SourceSerif4-Italic%5Bopsz%2Cwght%5D.ttf"

echo "Cormorant Garamond..."
fetch_to "$DEST_ROOT/truetype/cormorantgaramond" "CormorantGaramond-wght.ttf"        "$GFONTS_BASE/cormorantgaramond/CormorantGaramond%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/cormorantgaramond" "CormorantGaramond-Italic-wght.ttf" "$GFONTS_BASE/cormorantgaramond/CormorantGaramond-Italic%5Bwght%5D.ttf"

echo "Bitter..."
fetch_to "$DEST_ROOT/truetype/bitter" "Bitter-wght.ttf"        "$GFONTS_BASE/bitter/Bitter%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/bitter" "Bitter-Italic-wght.ttf" "$GFONTS_BASE/bitter/Bitter-Italic%5Bwght%5D.ttf"

echo "Gelasio..."
fetch_to "$DEST_ROOT/truetype/gelasio" "Gelasio-wght.ttf"        "$GFONTS_BASE/gelasio/Gelasio%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/gelasio" "Gelasio-Italic-wght.ttf" "$GFONTS_BASE/gelasio/Gelasio-Italic%5Bwght%5D.ttf"

echo "Noto Sans..."
fetch_to "$DEST_ROOT/truetype/notosans" "NotoSans-wdth-wght.ttf"        "$GFONTS_BASE/notosans/NotoSans%5Bwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/notosans" "NotoSans-Italic-wdth-wght.ttf" "$GFONTS_BASE/notosans/NotoSans-Italic%5Bwdth%2Cwght%5D.ttf"

echo "Open Sans..."
fetch_to "$DEST_ROOT/truetype/opensans" "OpenSans-wdth-wght.ttf"        "$GFONTS_BASE/opensans/OpenSans%5Bwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/opensans" "OpenSans-Italic-wdth-wght.ttf" "$GFONTS_BASE/opensans/OpenSans-Italic%5Bwdth%2Cwght%5D.ttf"

echo "Roboto..."
fetch_to "$DEST_ROOT/truetype/roboto" "Roboto-wdth-wght.ttf"        "$GFONTS_BASE/roboto/Roboto%5Bwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/roboto" "Roboto-Italic-wdth-wght.ttf" "$GFONTS_BASE/roboto/Roboto-Italic%5Bwdth%2Cwght%5D.ttf"

echo "Inter..."
fetch_to "$DEST_ROOT/truetype/inter" "Inter-opsz-wght.ttf"        "$GFONTS_BASE/inter/Inter%5Bopsz%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/inter" "Inter-Italic-opsz-wght.ttf" "$GFONTS_BASE/inter/Inter-Italic%5Bopsz%2Cwght%5D.ttf"

echo "IBM Plex Sans..."
fetch_to "$DEST_ROOT/truetype/ibmplexsans" "IBMPlexSans-wdth-wght.ttf"        "$GFONTS_BASE/ibmplexsans/IBMPlexSans%5Bwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/ibmplexsans" "IBMPlexSans-Italic-wdth-wght.ttf" "$GFONTS_BASE/ibmplexsans/IBMPlexSans-Italic%5Bwdth%2Cwght%5D.ttf"

echo "Nunito Sans..."
fetch_to "$DEST_ROOT/truetype/nunitosans" "NunitoSans-YTLC-opsz-wdth-wght.ttf"        "$GFONTS_BASE/nunitosans/NunitoSans%5BYTLC%2Copsz%2Cwdth%2Cwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/nunitosans" "NunitoSans-Italic-YTLC-opsz-wdth-wght.ttf" "$GFONTS_BASE/nunitosans/NunitoSans-Italic%5BYTLC%2Copsz%2Cwdth%2Cwght%5D.ttf"

echo "Raleway..."
fetch_to "$DEST_ROOT/truetype/raleway" "Raleway-wght.ttf"        "$GFONTS_BASE/raleway/Raleway%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/raleway" "Raleway-Italic-wght.ttf" "$GFONTS_BASE/raleway/Raleway-Italic%5Bwght%5D.ttf"

echo "Montserrat..."
fetch_to "$DEST_ROOT/truetype/montserrat" "Montserrat-wght.ttf"        "$GFONTS_BASE/montserrat/Montserrat%5Bwght%5D.ttf"
fetch_to "$DEST_ROOT/truetype/montserrat" "Montserrat-Italic-wght.ttf" "$GFONTS_BASE/montserrat/Montserrat-Italic%5Bwght%5D.ttf"

# === Google Fonts: static fonts (4 files: Regular, Bold, Italic, BoldItalic) ===
echo "Lato..."
for f in Lato-Regular Lato-Bold Lato-Italic Lato-BoldItalic; do
  fetch_to "$DEST_ROOT/truetype/lato" "$f.ttf" "$GFONTS_BASE/lato/$f.ttf"
done

echo "Fira Sans..."
for f in FiraSans-Regular FiraSans-Bold FiraSans-Italic FiraSans-BoldItalic; do
  fetch_to "$DEST_ROOT/truetype/firasans" "$f.ttf" "$GFONTS_BASE/firasans/$f.ttf"
done

# === Artifex urw-base35: URW Bookman (free Bookman clone) ===================
# Family name reported by fontconfig: "URW Bookman" (Light/Demi weights).
echo "URW Bookman..."
for f in URWBookman-Light URWBookman-LightItalic URWBookman-Demi URWBookman-DemiItalic; do
  fetch_to "$DEST_ROOT/opentype/urw-bookman" "$f.otf" \
    "https://raw.githubusercontent.com/ArtifexSoftware/urw-base35-fonts/master/fonts/$f.otf"
done

# === Bookman Old Style: proprietary, manual install only ====================
BOOKMAN_DIR="$DEST_ROOT/truetype/bookman-old-style"
if [ -d "$BOOKMAN_DIR" ] && [ -n "$(find "$BOOKMAN_DIR" -maxdepth 1 -type f \( -iname '*.ttf' -o -iname '*.otf' \) 2>/dev/null)" ]; then
  echo "Bookman Old Style: present in $BOOKMAN_DIR"
else
  echo
  echo "Note: Bookman Old Style is proprietary and was not installed."
  echo "      To enable it, place licensed TTF files (BOOKOS.TTF, BOOKOSB.TTF,"
  echo "      BOOKOSI.TTF, BOOKOSBI.TTF) in:"
  echo "          $BOOKMAN_DIR"
fi

echo
echo "Done. Installed to: $DEST_ROOT"
