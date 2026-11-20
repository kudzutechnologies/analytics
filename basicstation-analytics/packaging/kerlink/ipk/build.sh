#!/bin/sh
set -eu
: "${BINARY:?Supply a Kerlink SDK-built binary}"
: "${VERSION:?Supply the package version}"
: "${RUNTIME_DEPENDS:?Supply SDK runtime dependencies}"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$script_dir/../../../../tools/build_ipk.py" --binary "$BINARY" --assets "$script_dir/.." --version "$VERSION" --depends "$RUNTIME_DEPENDS" --output "${OUTPUT:-kudzu-bs-analytics_${VERSION}_klkgw.ipk}"
