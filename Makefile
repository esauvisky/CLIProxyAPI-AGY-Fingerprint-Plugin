PLUGIN_SO := build/agy-fingerprint.so

.PHONY: all test clean

all: $(PLUGIN_SO)

$(PLUGIN_SO): main.go go.mod
	mkdir -p build
	CGO_ENABLED=1 go build -buildvcs=false -buildmode=c-shared -o $(PLUGIN_SO) .
	rm -f build/agy-fingerprint.h

test: $(PLUGIN_SO)
	go vet ./...

clean:
	rm -rf build
