# SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_HACK_DIR           := $(shell go list -mod=mod -m -f "{{.Dir}}" github.com/gardener/gardener)/hack

EXTENSION_PREFIX            := gardener-extension
NAME                        := acl
ADMISSION_NAME              := admission-acl
export REPO                 := ghcr.io/stackitcloud
REPO_ROOT                   := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                    := $(REPO_ROOT)/hack
VERSION                     := $(shell git describe --tag --always --dirty)
export TAG                  := $(VERSION)
LEADER_ELECTION             := false
IGNORE_OPERATION_ANNOTATION := false

SHELL=/usr/bin/env bash -o pipefail

WEBHOOK_CONFIG_PORT	:= 10250
WEBHOOK_CONFIG_MODE	:= url
WEBHOOK_CONFIG_URL	:= host.docker.internal:$(WEBHOOK_CONFIG_PORT)
EXTENSION_NAMESPACE	:= garden

WEBHOOK_PARAM := --webhook-config-url=$(WEBHOOK_CONFIG_URL)
ifeq ($(WEBHOOK_CONFIG_MODE), service)
  WEBHOOK_PARAM := --webhook-config-namespace=$(EXTENSION_NAMESPACE)
endif

#########################################
# Tools                                 #
#########################################

# Manually update golangci-lint to the latest v1 version.
# This unblocks the go 1.24 upgrade, which will unblock the gardener/gardener update.
# TODO(timebertt): remove this version override when bumping the gardener/gardener dependency.
GOLANGCI_LINT_VERSION := v1.64.8

TOOLS_DIR := hack/tools
include $(GARDENER_HACK_DIR)/tools.mk
include $(HACK_DIR)/tools.mk

.PHONY: run
run:
	@LEADER_ELECTION_NAMESPACE=garden GO111MODULE=on go run \
		./cmd/$(EXTENSION_PREFIX)-$(NAME) \
		--kubeconfig=${KUBECONFIG} \
		--ignore-operation-annotation=$(IGNORE_OPERATION_ANNOTATION) \
		--leader-election=$(LEADER_ELECTION) \
		--webhook-config-mode=url \
		--webhook-config-url="host.docker.internal:9443" \
		--webhook-config-cert-dir=example/certs \
		--webhook-config-server-port=9443

.PHONY: debug
debug:
	@LEADER_ELECTION_NAMESPACE=garden GO111MODULE=on dlv debug\
		./cmd/$(EXTENSION_PREFIX)-$(NAME) -- \
		--kubeconfig=${KUBECONFIG} \
		--ignore-operation-annotation=$(IGNORE_OPERATION_ANNOTATION) \
		--leader-election=$(LEADER_ELECTION)

.PHONY: start-admission
start-admission:
	@LEADER_ELECTION_NAMESPACE=garden go run \
		./cmd/$(EXTENSION_PREFIX)-$(ADMISSION_NAME) \
		--maxAllowedCIDRs=50 \
		--webhook-config-server-host=0.0.0.0 \
		--webhook-config-server-port=$(WEBHOOK_CONFIG_PORT) \
		--webhook-config-mode=$(WEBHOOK_CONFIG_MODE) \
		$(WEBHOOK_PARAM)

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

export PUSH ?= false

.PHONY: images
images: $(KO)
	KO_DOCKER_REPO=$(REPO) $(KO) build --push=$(PUSH) \
		--image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-acl" \
		--sbom none -t $(TAG) --base-import-paths \
		--platform linux/amd64,linux/arm64 \
		./cmd/gardener-extension-acl ./cmd/gardener-extension-admission-acl \
		| tee images.txt

.PHONY: artifacts-only
artifacts-only: $(HELM) $(YQ)
	hack/push-artifacts.sh

.PHONY: artifacts
artifacts: images artifacts-only

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: clean
clean:
	@bash $(GARDENER_HACK_DIR)/clean.sh ./cmd/... ./pkg/...

.PHONY: check
check: $(GOIMPORTS) $(GOLANGCI_LINT) $(HELM)
	@bash $(GARDENER_HACK_DIR)/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/...
	@bash $(GARDENER_HACK_DIR)/check-charts.sh ./charts

.PHONY: generate
generate: $(VGOPATH) $(HELM) $(YQ)
	@REPO_ROOT=$(REPO_ROOT) VGOPATH=$(VGOPATH) bash $(GARDENER_HACK_DIR)/generate-controller-registration.sh acl charts/gardener-extension-acl latest deploy/extension/base/controller-registration.yaml Extension:acl

.PHONY: format
format: $(GOIMPORTS) $(GOIMPORTSREVISER)
	@bash $(GARDENER_HACK_DIR)/format.sh ./cmd ./pkg

.PHONY: test
test: $(REPORT_COLLECTOR) $(SETUP_ENVTEST)
	@./hack/test.sh ./cmd/... ./pkg/...

.PHONY: test-cov
test-cov:
	@bash $(GARDENER_HACK_DIR)/test-cover.sh ./cmd/... ./pkg/...

.PHONY: test-cov-clean
test-cov-clean:
	@bash $(GARDENER_HACK_DIR)/test-cover-clean.sh

.PHONY: verify
verify: check format test

.PHONY: verify-tidy
verify-tidy: tidy ## Verify go module files are up to date.
	@if !(git diff --quiet HEAD -- go.mod go.sum); then \
		echo "go module files are out of date, please run 'make tidy'"; exit 1; \
	fi

.PHONY: verify-generate
verify-generate: clean generate ## Verify generated files are up to date.
	@if !(git diff --quiet HEAD); then \
		echo "generated files are out of date, please run 'make generate'"; exit 1; \
	fi

.PHONY: verify-extended
verify-extended: verify-tidy verify-generate check format test artifacts

#####################################################################
# Rules for local environment                                       #
#####################################################################

# speed-up skaffold deployments by building all images concurrently
extension-%: export SKAFFOLD_BUILD_CONCURRENCY = 0
extension-%: export SKAFFOLD_DEFAULT_REPO = localhost:5001
extension-%: export SKAFFOLD_PUSH = true
# use static label for skaffold to prevent rolling all gardener components on every `skaffold` invocation
extension-%: export SKAFFOLD_LABEL = skaffold.dev/run-id=acl

extension-up: $(SKAFFOLD)
	$(SKAFFOLD) run
extension-dev: $(SKAFFOLD)
	$(SKAFFOLD) dev --cleanup=false --trigger=manual
extension-down: $(SKAFFOLD)
	$(SKAFFOLD) delete
