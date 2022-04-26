go = go
goimports = go run golang.org/x/tools/cmd/goimports@latest
PKGPATH=$(shell go list -m)
T=$(PKGPATH)/...

.DEFAULT: release

.PHONY: release
release:: build test ec2-macos-init

ec2-macos-init ec2-macos-init_amd64 ec2-macos-init_arm64 &:
	./build.sh

.PHONY: build
build:
	$(go) build $(V) $(T)

.PHONY: test
test:
	$(go) test $(V) $(T)

.PHONY: imports goimports
imports goimports:
	$(goimports) -local $(PKGPATH) $(or $(goimports_flags),-w) .

.PHONY: clean
clean::
	$(go) clean -i
	git clean -fX
