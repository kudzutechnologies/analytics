NAME := kudzu-forwarder
VERSION ?= dev
OUTDIR ?= .
all:
	python3 ../tools/build-go-forwarders.py --component udp --version "$(VERSION)" --output-dir "$(OUTDIR)"
tarball:
	python3 ../tools/build-go-forwarders.py --component udp --version "$(VERSION)" --output-dir "$(OUTDIR)" --bundle
$(NAME)-%:
	python3 ../tools/build-go-forwarders.py --component udp --target "$*" --version "$(VERSION)" --output-dir "$(OUTDIR)"
.PHONY: all tarball
