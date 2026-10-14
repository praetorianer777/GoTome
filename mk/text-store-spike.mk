# The text store spike (#244). Nothing here is part of the gate: it reads a
# library of EPUBs, which only the owner has. spikes/text-store/README.md says
# what each step does.

TEXT_STORE_SPIKE_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

.PHONY: text-store-spike
text-store-spike: ## Measure where chunk text could live: LIBRARY=<folder of EPUBs> [STEPS=...]
	@test -n "$(LIBRARY)" || { echo "LIBRARY=<folder of EPUBs> is required"; exit 1; }
	$(TEXT_STORE_SPIKE_ROOT)/spikes/text-store/bench.sh $(LIBRARY) $(STEPS)
