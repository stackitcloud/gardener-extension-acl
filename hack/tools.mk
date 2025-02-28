# This make file is supposed to be included in the top-level make file.
# It can be reused by repos vendoring g/g to have some common make recipes for building and installing development
# tools as needed.
# Recipes in the top-level make file should declare dependencies on the respective tool recipes (e.g. $(CONTROLLER_GEN))
# as needed. If the required tool (version) is not built/installed yet, make will make sure to build/install it.
# The *_VERSION variables in this file contain the "default" values, but can be overwritten in the top level make file.

TOOLS_BIN_DIR ?= $(TOOLS_DIR)/bin

#########################################
# Tools                                 #
#########################################

KO := $(TOOLS_BIN_DIR)/ko
# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.15.4
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

HELM := $(TOOLS_BIN_DIR)/helm
# renovate: datasource=github-releases depName=helm/helm
HELM_VERSION ?= v3.17.1
$(HELM): $(call tool_version_file,$(HELM),$(HELM_VERSION))
	curl -sSfL https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | HELM_INSTALL_DIR=$(TOOLS_BIN_DIR) USE_SUDO=false bash -s -- --version $(HELM_VERSION)

YQ := $(TOOLS_BIN_DIR)/yq
# renovate: datasource=github-releases depName=mikefarah/yq
YQ_VERSION ?= v4.45.1
$(YQ): $(call tool_version_file,$(YQ),$(YQ_VERSION))
	curl -L -o $(YQ) https://github.com/mikefarah/yq/releases/download/$(YQ_VERSION)/yq_$(SYSTEM_NAME)_$(SYSTEM_ARCH)
	chmod +x $(YQ)
