VERSION ?= 0.1.0
BINARY ?=
RUNTIME_DEPENDS ?=
OUTPUT ?= kudzu-bs-analytics_$(VERSION)_klkgw.ipk

all:
	@test -n "$(BINARY)" || { echo "BINARY must name a Kerlink SDK-built binary" >&2; exit 2; }
	@test -n "$(RUNTIME_DEPENDS)" || { echo "RUNTIME_DEPENDS must name packages from the target SDK" >&2; exit 2; }
	python3 ../../../tools/build_ipk.py --binary "$(BINARY)" --assets . --version "$(VERSION)" --depends "$(RUNTIME_DEPENDS)" --output "$(OUTPUT)"

.PHONY: all
