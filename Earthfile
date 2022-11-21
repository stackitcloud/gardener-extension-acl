VERSION 0.6
FROM golang:1.18-alpine
ARG NAME=acl
ARG DOCKER_REPO=ghcr.io/stackitcloud/gardener-extension-$NAME
ARG BINPATH=/usr/local/bin/
ARG GOCACHE=/go-cache

local-setup:
    LOCALLY
    RUN git config --local core.hooksPath .githooks/

deps:
    WORKDIR /src
    ENV GO111MODULE=on
    ENV CGO_ENABLED=0
    COPY go.mod go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum

build-extension-controller:
    FROM +deps
    COPY --dir pkg/ cmd/ charts/ .
    ARG GOOS=linux
    ARG GOARCH=amd64
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o app/gardener-extension ./cmd/gardener-extension/main.go
    SAVE ARTIFACT app/gardener-extension

build-webhook:
    FROM +deps
    COPY --dir pkg/ cmd/ charts/ .
    ARG GOOS=linux
    ARG GOARCH=amd64
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o app/webhook ./cmd/webhook/main.go
    SAVE ARTIFACT app/webhook

build-local:
    ARG USEROS
    ARG USERARCH
    COPY (+build-extension-controller/gardener-extension --GOOS=$USEROS --GOARCH=$USERARCH) /gardener-extension
    COPY (+build-webhook/webhook --GOOS=$USEROS --GOARCH=$USERARCH) /webhook
    SAVE ARTIFACT /gardener-extension AS LOCAL out/gardener-extension
    SAVE ARTIFACT /webhook AS LOCAL out/webhook

build-test:
    FROM +deps
    COPY --dir pkg/ cmd/ charts/ .
    RUN --mount=type=cache,target=$GOCACHE \
        go build -ldflags="-w -s" -o /dev/null ./...

set-version:
    FROM alpine/git
    COPY .git .git
    RUN git describe --tags --always > VERSION
    SAVE ARTIFACT VERSION

ci:
    BUILD +lint
    BUILD +test

version:
    # todo find lightweight image for cat
    FROM busybox
    COPY +set-version/VERSION .
    BUILD +docker --DOCKER_TAG=$(cat VERSION)
    
docker:
    BUILD +docker-extension
    BUILD +docker-webhook

docker-extension:
    ARG TARGETPLATFORM
    ARG TARGETOS
    ARG TARGETARCH
    ARG DOCKER_TAG
    FROM --platform=$TARGETPLATFORM \
        gcr.io/distroless/static:nonroot
    COPY --platform=$USERPLATFORM \
        (+build-extension-controller/gardener-extension --GOOS=$TARGETOS --GOARCH=$TARGETARCH) /gardener-extension
    COPY --dir charts/ /charts
    BUILD +set-version
    USER 65532:65532
    ENTRYPOINT ["/gardener-extension"]
    SAVE IMAGE --push $DOCKER_REPO-controller:$DOCKER_TAG

docker-webhook:
    ARG TARGETPLATFORM
    ARG TARGETOS
    ARG TARGETARCH
    ARG DOCKER_TAG
    FROM --platform=$TARGETPLATFORM \
        gcr.io/distroless/static:nonroot
    COPY --platform=$USERPLATFORM \
        (+build-webhook/webhook --GOOS=$TARGETOS --GOARCH=$TARGETARCH) /webhook
    COPY --dir charts/ /charts
    BUILD +set-version
    USER 65532:65532
    ENTRYPOINT ["/webhook"]
    SAVE IMAGE --push $DOCKER_REPO-webhook:$DOCKER_TAG

revendor:
    FROM +deps
    COPY --dir pkg/ cmd/ charts/ tools.go .
    RUN GO111MODULE=on go mod vendor
    RUN GO111MODULE=on go mod tidy
    RUN chmod +x vendor/github.com/gardener/gardener/hack/*
    RUN chmod +x vendor/github.com/gardener/gardener/hack/.ci/*

install-requirements:
    FROM +revendor
    RUN pwd
    RUN go install -mod=vendor ./vendor/github.com/ahmetb/gen-crd-api-reference-docs
    RUN go install -mod=vendor ./vendor/github.com/golang/mock/mockgen
    RUN go install -mod=vendor ./vendor/golang.org/x/tools/cmd/goimports
    SAVE ARTIFACT vendor

generate-deploy:
    FROM nixery.dev/shell/envsubst/gzip/gnutar
    COPY --dir charts/gardener-extension example/ .
    COPY +set-version/VERSION .
    ARG CHART_SOURCE=gardener-extension/
    ARG TEMPLATE=example/deployment.tpl.yaml
    ARG TARGET=controller-registration.yaml
    ENV ENCODED_CHART
    ENV TAG
    RUN ENCODED_CHART=$(cd $CHART_SOURCE && \
        tar --format=gnu --sort=name --owner=root:0 --group=root:0 --numeric-owner --mtime='UTC 2019-01-01' -zcf - . | base64 -w0) && \
        TAG=$(cat VERSION) && \
        envsubst < $TEMPLATE > $TARGET
    SAVE ARTIFACT $TARGET AS LOCAL deploy/$TARGET

deploy:
    FROM dtzar/helm-kubectl
    COPY +generate-deploy/controller-registration.yaml .
    RUN --push \
        --mount=type=secret,target=/root/.kube/config,id=KUBECONFIG \
        sed -i 's/v0.0.1-dev/12d8353d/g' controller-registration.yaml && \
        kubectl apply -f controller-registration.yaml --wait

lint:
    ARG GOLANGCI_LINT_CACHE=/golangci-cache
    FROM +deps
    COPY +golangci-lint/golangci-lint $BINPATH
    COPY --dir pkg/ cmd/ charts/ .golangci.yml .
    RUN --mount=type=cache,target=$GOCACHE \
        --mount=type=cache,target=$GOLANGCI_LINT_CACHE \
        golangci-lint run -v ./...

# todo only when writing custom controller
test:
    FROM +deps
    ARG KUBERNETES_VERSION=1.23.x
    COPY +gotools/bin/setup-envtest $BINPATH
    # install envtest in its own layer
    RUN setup-envtest use $KUBERNETES_VERSION
    COPY --dir pkg/ cmd/ upstream-crds/ charts/ .
    ARG GO_TEST="go test"
    RUN --mount=type=cache,target=$GOCACHE \
        if [ ! "$(ls -A $GOCACHE)" ]; then echo "WAITING FOR GO TEST TO BUILD TESTING BIN"; fi && \
        eval `setup-envtest use -p env $KUBERNETES_VERSION` && \
        eval "$GO_TEST ./pkg/..."

test-output:
    FROM +test --GO_TEST="go test -count 1 -coverprofile=cover.out"
    SAVE ARTIFACT cover.out

coverage:
    FROM +deps
    COPY --dir pkg/ cmd/ .
    COPY +test-output/cover.out .
    RUN go tool cover -func=cover.out

coverage-html:
    LOCALLY
    COPY +test-output/cover.out out/cover.out
    RUN go tool cover -html=out/cover.out

snyk-scan:
    FROM +deps
    COPY +snyk/snyk $BINPATH
    COPY --dir pkg/ cmd/ charts/ .
    COPY .snyk .
    RUN --secret SNYK_TOKEN snyk test

snyk-helm:
    FROM +deps
    COPY +snyk/snyk $BINPATH
    COPY --dir +helm2kube/result .
    COPY .snyk .
    RUN --secret SNYK_TOKEN \
        snyk iac test \
        --policy-path=.snyk \ # I don't know why the CLI won't pick this up by default...
        --severity-threshold=high \  # TODO remove this line if you want to fix a lot of issues in the helm charts
        result

# todo: semgrep
# semgrep:

all:
    BUILD +deps
    BUILD +generate-deploy
    BUILD +snyk-scan
    BUILD +snyk-helm
    #BUILD +semgrep # TODO semgrep
    BUILD +lint
    BUILD +coverage
    BUILD +ci

###########
# helper
###########

golangci-lint:
    FROM golangci/golangci-lint:v1.46.2
    SAVE ARTIFACT /usr/bin/golangci-lint

snyk:
    FROM snyk/snyk:alpine
    SAVE ARTIFACT /usr/local/bin/snyk

helm:
    FROM alpine/helm:3.8.1
    SAVE ARTIFACT /usr/bin/helm

bash:
    FROM bash
    SAVE ARTIFACT /usr/local/bin

helm2kube:
    COPY +helm/helm $BINPATH
    COPY --dir ./charts/gardener-extension .
    COPY --dir ./charts/seed .
    RUN mkdir result
    RUN helm template ./gardener-extension >> result/gardener-extension.yaml
    RUN helm template ./seed >> result/seed.yaml
    SAVE ARTIFACT result

gotools:
    FROM +deps
    # tool versions tracked in go.mod through tools.go
    RUN go install \
        sigs.k8s.io/controller-runtime/tools/setup-envtest
    SAVE ARTIFACT /go/bin

controller-gen:
    FROM +deps
    # tool versions tracked in go.mod through tools.go
    RUN go install \
        sigs.k8s.io/controller-tools/cmd/controller-gen
    SAVE ARTIFACT /go/bin
