# SPDX-License-Identifier: MIT
# Copyright 2026 Kudzu Technologies
PKG_DIR:=${CURDIR}
BIN_FILE:=basicstation-analytics
PKG_VERSION:=0.1.0
IPKG_FILE:=kudzu-bs-analytics_$(PKG_VERSION)_klkgw.ipk

all: $(IPKG_FILE)

$(IPKG_FILE):
	@echo "Build binary for Kerlink first, then:"
	@echo "  docker run --rm -v $(PKG_DIR):/assets/pkg -v <binary-dir>/$(BIN_FILE):/assets/bin/$(BIN_FILE) ipkg_build:latest /assets/pkg/ipk/build.sh"

.PHONY: all
