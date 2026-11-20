help:
	@echo "make api: regenerate Go bindings and build-tree C++ bindings"
	@echo "make build-basicstation: native consumer and all Linux Go targets"
	@echo "make release-check: verify tagged release prerequisites"
	@echo "make release: build all artifacts without modifying source files"

api:
	cd api && protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative *.proto
	cmake -S client-cc -B client-cc/build-codegen
	cmake --build client-cc/build-codegen --target client-cc-proto

build-basicstation:
	$(MAKE) -C forwarder-bs all
	cmake -S basicstation-analytics -B basicstation-analytics/build-native -DCMAKE_BUILD_TYPE=MinSizeRel
	cmake --build basicstation-analytics/build-native --target basicstation-analytics

release-check:
	python3 tools/release-basicstation.py --check
release:
	python3 tools/release-basicstation.py

.PHONY: help api build-basicstation release-check release
