# Copyright (c) Bas van Beek 2024.
# Copyright (c) Tetrate, Inc 2023.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

MODULE_PATH ?= $(shell sed -ne 's/^module //gp' go.mod)

# Tools
LINTER    := github.com/golangci/golangci-lint/cmd/golangci-lint@v1.43.0
GOIMPORTS := golang.org/x/tools/cmd/goimports@v0.1.5

# List of available module subdirs.
SUBDIRS := . group

.PHONY: build
build:
	$(call run,go build ./...)

TEST_OPTS ?= -race
.PHONY: test
test:
	$(call run,go test $(TEST_OPTS) ./...)

BENCH_OPTS ?=
.PHONY: bench
bench:
	$(call run,go test -bench=. $(BENCH_OPTS) ./...)

.PHONY: coverage
coverage:
	mkdir -p build
	go test -coverprofile build/coverage.out -covermode atomic -coverpkg '$(MODULE_PATH)/...' ./...
	go tool cover -o build/coverage.html -html build/coverage.out

LINT_CONFIG := $(dir $(abspath $(lastword $(MAKEFILE_LIST)))).golangci.yml
LINT_OPTS ?= --timeout 5m
.PHONY: lint
lint:
	$(call run,go run $(LINTER) run $(LINT_OPTS) --config $(LINT_CONFIG))

GO_SOURCES = $(shell git ls-files | grep '.go$$')
.PHONY: format
format:
	@for f in $(GO_SOURCES); do \
		awk '/^import \($$/,/^\)$$/{if($$0=="")next}{print}' "$$f" > /tmp/fmt; \
		mv /tmp/fmt "$$f"; \
	done
	go run $(GOIMPORTS) -w -local $(MODULE_PATH) $(GO_SOURCES)

.PHONY: check
check:
	@$(MAKE) format
	$(call run,go mod tidy)
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git diff; \
		exit 1; \
	fi

# Run command defined in the first arg of this function in each defined subdir.
define run
	for DIR in $(SUBDIRS); do \
		cd $$DIR && $1; \
	done
endef
