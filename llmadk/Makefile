# Targets for the nested llmadk module. Run from this directory or with make -C llmadk.

MODULE := github.com/nocturnium/llm-go-sdk/llmadk
export GOWORK := off

.PHONY: test lint apidiff apidiff-baseline integration

test:
	go test -race ./...

lint:
	golangci-lint run --config=../.golangci.yml ./...

# apidiff fails when the exported API of the llmadk package differs from
# api.txt; re-baseline on an intended change and review the diff. Only the
# package is compared, so example programs never count as API.
apidiff:
	@out="$$(apidiff api.txt $(MODULE) 2>&1 || true)"; \
	if [ -n "$$out" ]; then echo "$$out"; exit 1; fi; echo "Public API matches api.txt."

apidiff-baseline:
	apidiff -w api.txt $(MODULE)

# integration runs the live gate; it needs provider API keys.
integration:
	go test -tags=integration -race -count=1 -v ./...
