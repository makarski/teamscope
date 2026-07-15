.PHONY: build test vet fmt check config snapshot serve clean

BINARY := teamscope
CONFIG := teamscope-config.toml

build:
	@go build -o $(BINARY) .

test:
	@go test ./...

vet:
	@go vet ./...

fmt:
	@gofmt -w .

# Pre-flight: format, vet, test. Run before committing.
check: fmt vet test

# Bootstrap a config from the template if one does not exist.
config:
	@test -f $(CONFIG) || cp $(CONFIG).template $(CONFIG)
	@echo "> edit $(CONFIG) with your credentials, teams and goals"

snapshot: build
	@./$(BINARY) --config $(CONFIG) snapshot

serve: build
	@./$(BINARY) --config $(CONFIG) serve

clean:
	@rm -f $(BINARY)
