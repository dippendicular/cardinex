.PHONY: build test lib golden conformance python cpp csharp java node rust docker cross

build:
	go build ./...

test:
	go test ./...

# Native library for the host platform -> dist/
lib:
	bash scripts/build-lib.sh dist

# Regenerate golden files (only before v1.0 is released).
golden:
	go run ./conformance/gen

conformance: lib
	go test ./conformance/
	cd sdk/python && CARDINEX_LIB=$(CURDIR)/dist/$(shell uname -s | grep -q Darwin && echo libcardinex.dylib || echo libcardinex.so) python -m pytest tests

python: lib
	cp dist/libcardinex.* sdk/python/cardinex/ 2>/dev/null || cp dist/cardinex.dll sdk/python/cardinex/
	cd sdk/python && python -m pip wheel . -w ../../dist/wheels

cpp: lib
	cmake -S sdk/cpp -B build/cpp && cmake --build build/cpp && ./build/cpp/conformance conformance

csharp: lib
	dotnet run --project sdk/csharp/Cardinex.Conformance -- conformance

java: lib
	cd sdk/java && mvn -q test -Dcardinex.lib=$(CURDIR)/dist/libcardinex.so

node: lib
	cd sdk/node && npm install && npm test

rust: lib
	cd sdk/rust && CARDINEX_LIB_DIR=$(CURDIR)/dist cargo test

docker:
	docker compose up --build

cross:
	bash scripts/cross-build.sh
