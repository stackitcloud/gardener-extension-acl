# SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_HACK_DIR           := $(shell go list -mod=mod -m -f "{{.Dir}}" github.com/gardener/gardener)/hack

EXTENSION_PREFIX            := gardener-extension
NAME                        := acl
REPO                        := ghcr.io/stackitcloud/gardener-extension-acl
REPO_ROOT                   := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                    := $(REPO_ROOT)/hack
VERSION                     := $(shell git describe --tag --always --dirty)
TAG							:= $(VERSION)
LEADER_ELECTION             := false
IGNORE_OPERATION_ANNOTATION := false

SHELL=/usr/bin/env bash -o pipefail

#########################################
# Tools                                 #
#########################################

TOOLS_DIR := $(HACK_DIR)/tools
include $(GARDENER_HACK_DIR)/tools.mk
include $(HACK_DIR)/tools.mk

GOIMPORTSREVISER_VERSION := v3.5.6

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

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

PUSH ?= false
images: export KO_DOCKER_REPO = $(REPO)

.PHONY: images
images: $(KO)
	KO_DOCKER_REPO=$(REPO) $(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-acl" --sbom none -t $(TAG) --bare --platform linux/amd64,linux/arm64 --push=$(PUSH) ./cmd/gardener-extension-acl

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: tidy
tidy:
	@GO111MODULE=on go mod tidy

# run `make init` to perform an initial go mod cache sync which is required for other make targets
init: tidy
# needed so that check-generate.sh can call make revendor
revendor: tidy

.PHONY: clean
clean:
	@bash $(GARDENER_HACK_DIR)/clean.sh ./cmd/... ./pkg/...

.PHONY: check-generate
check-generate:
	@bash $(GARDENER_HACK_DIR)/check-generate.sh $(REPO_ROOT)

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

.PHONY: verify-extended
verify-extended: check-generate check format test

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
