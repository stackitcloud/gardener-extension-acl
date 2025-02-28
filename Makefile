# SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_HACK_DIR           := $(shell go list -mod=mod -m -f "{{.Dir}}" github.com/gardener/gardener)/hack

EXTENSION_PREFIX            := gardener-extension
NAME                        := acl
ADMISSION_NAME              := admission-acl
REPO                        := ghcr.io/stackitcloud
REPO_ROOT                   := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
HACK_DIR                    := $(REPO_ROOT)/hack
VERSION                     := $(shell git describe --tag --always --dirty)
TAG                         := $(VERSION)
LEADER_ELECTION             := false
IGNORE_OPERATION_ANNOTATION := false

EXTENSION_IMAGE           := $(REPO)/$(EXTENSION_PREFIX)-$(NAME)
EXTENSION_ADMISSION_IMAGE := $(REPO)/$(EXTENSION_PREFIX)-$(ADMISSION_NAME)

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

TOOLS_DIR := hack/tools
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

PUSH ?= false

.PHONY: images
images: $(KO)
	KO_DOCKER_REPO=$(EXTENSION_IMAGE) $(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-acl" --sbom none -t $(TAG) --bare --platform linux/amd64,linux/arm64 --push=$(PUSH) ./cmd/gardener-extension-acl
	KO_DOCKER_REPO=$(EXTENSION_ADMISSION_IMAGE) $(KO) build --image-label org.opencontainers.image.source="https://github.com/stackitcloud/gardener-extension-acl" --sbom none -t $(TAG) --bare --platform linux/amd64,linux/arm64 --push=$(PUSH) ./cmd/gardener-extension-admission-acl

.PHONY: helm-charts
helm-charts: $(HELM) $(YQ)
	@bash $(HACK_DIR)/package-helm-chart.sh --repo $(REPO) --image $(EXTENSION_IMAGE) --version $(VERSION) --image-ref image --chart-path $(REPO_ROOT)/charts/gardener-extension-acl --push $(PUSH)
	@bash $(HACK_DIR)/package-helm-chart.sh --repo $(REPO) --image $(EXTENSION_ADMISSION_IMAGE) --version $(VERSION) --image-ref global.image.repository --image-tag global.image.tag --chart-path $(REPO_ROOT)/charts/gardener-extension-admission-acl/charts/application --push $(PUSH)
	@bash $(HACK_DIR)/package-helm-chart.sh --repo $(REPO) --image $(EXTENSION_ADMISSION_IMAGE) --version $(VERSION) --image-ref global.image.repository --image-tag global.image.tag --chart-path $(REPO_ROOT)/charts/gardener-extension-admission-acl/charts/runtime --push $(PUSH)

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
verify-extended: verify-tidy verify-generate check format test helm-charts

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
