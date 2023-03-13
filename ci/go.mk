GO = go
GOIMPORTS = goimports $(if $(GOPKGPATH),-local $(GOPKGPATH))
GOLANGCILINT = golangci-lint

goget:
	$(GO) get -t ./...

gobuild:
	$(GO) build $(GO_BUILD_FLAGS) $(V) ./...

gotest:
	$(GO) test $(GO_TEST_FLAGS) $(V) ./...

goimports gofmt:
	$(GOIMPORTS) -w .

golint:
	$(GOLANGCILINT) run
