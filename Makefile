.PHONY: fmt check

fmt:
	gofmt -w .

check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt: $$out"; exit 1; fi
	go vet ./...
	go test -race -count=1 ./...