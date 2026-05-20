# Usage:
#   ./build.sh                 # build everything into ./dist/*.zip
#   ./build.sh windows-x64     # build a single target

set -euo pipefail

if ! command -v zip >/dev/null 2>&1; then
    echo "error: 'zip' not found on PATH. Install it and re-run." >&2
    exit 1
fi

TARGETS=(
    "windows-x64    windows   amd64   .exe"
    "windows-x86    windows   386     .exe"
    "linux-x64      linux     amd64   "
    "linux-arm64    linux     arm64   "
    "darwin-x64     darwin    amd64   "
    "darwin-arm64   darwin    arm64   "
)

OUT_DIR="${OUT_DIR:-dist}"
mkdir -p "$OUT_DIR"

# Default release version. Override with `VERSION=1.2.3 ./build.sh` when
# cutting a different release.
VERSION="${VERSION:-1.2.0}"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildDate=${DATE}"

build_one() {
    local label="$1" goos="$2" goarch="$3" ext="$4"
    local bin="bpm${ext}"
    local stage="$OUT_DIR/.stage-${label}"
    local zip_name="bpm-${label}.zip"

    rm -rf "$stage"
    mkdir -p "$stage"

    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$stage/$bin" .

    rm -f "$OUT_DIR/$zip_name"
    ( cd "$stage" && zip -q "../$zip_name" "$bin" )
    rm -rf "$stage"

    local size
    size="$(stat -c%s "$OUT_DIR/$zip_name" 2>/dev/null || stat -f%z "$OUT_DIR/$zip_name")"
    printf "==> %-14s -> %s (%s bytes)\n" "$label" "$OUT_DIR/$zip_name" "$size"
}

want="${1:-}"

for row in "${TARGETS[@]}"; do
    read -r label goos goarch ext <<<"$row"
    if [[ -n "$want" && "$want" != "$label" ]]; then
        continue
    fi
    build_one "$label" "$goos" "$goarch" "$ext"
done

echo
echo "Done. Artifacts in: $OUT_DIR/"
ls -lh "$OUT_DIR/"*.zip
