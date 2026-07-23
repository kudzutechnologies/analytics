TAG := $(shell git describe --exact-match --tags HEAD 2>/dev/null)
VERSION := $(patsubst v%,%,$(TAG))
RELEASE_DIR := releases/$(TAG)

help:
	@echo "To build the API protobuf files run 'make api'"
	@echo "To build a forwarder release run 'make release' (requires HEAD on an exact git tag)"

api:
	(cd api; protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    *.proto)
	(cd client-cc/src/protobuf; protoc --cpp_out=. \
		--grpc_out=. \
		--plugin=protoc-gen-grpc=`which grpc_cpp_plugin` \
		--proto_path=../../../api ../../../api/*.proto)

release:
ifndef TAG
	$(error make release requires HEAD to be on an exact git tag)
endif
	@echo "Building release $(TAG) (version $(VERSION))"
	@mkdir -p $(RELEASE_DIR)
	sed -i.bak 's/^const MajorVersion = ".*"/const MajorVersion = "$(VERSION)"/' forwarder/config.go && rm forwarder/config.go.bak
	sed -i.bak 's/^PKG_VERSION:=.*/PKG_VERSION:=$(VERSION)/' forwarder/pkg/kerlink/Makefile && rm forwarder/pkg/kerlink/Makefile.bak
	sed -i.bak 's/^VERSION=".*"/VERSION="$(VERSION)"/' forwarder/pkg/kerlink/ipk/build.sh && rm forwarder/pkg/kerlink/ipk/build.sh.bak
	sed -i.bak 's/^Version: .*/Version: $(VERSION)/' forwarder/pkg/kerlink/ipk/control && rm forwarder/pkg/kerlink/ipk/control.bak
	$(MAKE) -C forwarder all
	$(MAKE) -C forwarder/pkg/kerlink all
	cp forwarder/kudzu-forwarder-*.tgz $(RELEASE_DIR)/
	cp forwarder/pkg/kerlink/kudzu-forwarder_$(VERSION)_klkgw.ipk $(RELEASE_DIR)/
	cp forwarder/pkg/kerlink/kudzu-forwarder-remove.ipk $(RELEASE_DIR)/
	@echo "Release artifacts copied to $(RELEASE_DIR)/"

.PHONY: api help release
