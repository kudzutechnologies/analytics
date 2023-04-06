PKG_DIR:=${CURDIR}
BINFILE:=kudzu-forwarder-arm7
BIN_DIR:=$(shell dirname $(shell dirname $(PKG_DIR)))
PKG_VERSION:=0.1.8
IPKG_FILE:=kudzu-forwarder_$(PKG_VERSION)_klkgw.ipk
IPKG_DELETE_FILE:=kudzu-forwarder_$(PKG_VERSION)_klkgw.ipk-remove.ipk

all: $(IPKG_FILE) $(IPKG_DELETE_FILE)

$(IPKG_FILE): container
	docker run -it --rm \
		-v $(PKG_DIR):/assets/pkg \
		-v $(BIN_DIR)/$(BIN_FILE):/assets/bin/$(BIN_FILE) \
		ipkg_build:latest \
		/assets/pkg/ipk/build.sh

$(IPKG_DELETE_FILE): $(IPKG_FILE)
	docker run -it --rm \
		-v $(PKG_DIR):/assets/pkg \
		-v $(BIN_DIR)/$(BIN_FILE):/assets/bin/$(BIN_FILE) \
		-w /assets/pkg \
		ipkg_build:latest \
		/assets/pkg/ipk/gen_remove_ipk kudzu-forwarder

container:
	docker build -t ipkg_build:latest .

.PHONY: all container