NAME := kudzu-forwarder-bs
VERSION ?= dev
OUTDIR ?= .
all:
	python3 ../tools/build-go-forwarders.py --component bs --version "$(VERSION)" --output-dir "$(OUTDIR)"
tarball:
	python3 ../tools/build-go-forwarders.py --component bs --version "$(VERSION)" --output-dir "$(OUTDIR)" --bundle
$(NAME)-%:
	python3 ../tools/build-go-forwarders.py --component bs --target "$*" --version "$(VERSION)" --output-dir "$(OUTDIR)"
.PHONY: all tarball
