# Fuzzing the parsers of untrusted files. The gate runs every fuzz target for a
# short budget, which catches a parser that panics on nearby input; a longer
# hunt is `make fuzz-go FUZZ_TIME=10m`.

FUZZ_TIME ?= 8s

# go test takes one fuzz target per run, so the targets are listed from the
# sources and run in turn. A failing input is written to the package's
# testdata/fuzz directory; commit it with the fix so it stays a regression test.
.PHONY: fuzz-go
fuzz-go: | $(GO_CACHE) go-toolchain ## Fuzz every Fuzz* target for FUZZ_TIME each
	@$(DOCKER_GO) bash -c 'set -eo pipefail; \
		for file in $$(grep -rl "^func Fuzz" --include="*_test.go" .); do \
			pkg=$$(dirname $$file); \
			for target in $$(sed -n "s/^func \(Fuzz[A-Za-z0-9_]*\)(.*/\1/p" $$file); do \
				echo "fuzz $$pkg $$target ($(FUZZ_TIME))"; \
				go test -run="^$$" -fuzz="^$$target$$" -fuzztime=$(FUZZ_TIME) $$pkg | grep -v "^fuzz: elapsed"; \
			done; \
		done'
