#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CHART_PATH=""
IMAGE=""
IMAGE_REF=""
IMAGE_TAG=""
PUSH="false"
REPO=""
VERSION=""


parse_flags() {
  while test $# -gt 0; do
    case "$1" in
    --chart-path)
      shift; CHART_PATH="$1"
      ;;
    --image)
      shift; IMAGE="$1"
      ;;
    --image-ref)
      shift; IMAGE_REF="$1"
      ;;
    --image-tag)
      shift; IMAGE_TAG="$1"
      ;;
    --push)
      shift; PUSH="$1"
      ;;
    --repo)
      shift; REPO="$1"
      ;;
    --version)
      shift; VERSION="$1"
      ;;
    esac

    shift
  done

  if [ -z "$CHART_PATH" ]; then
    echo "error: --chart-path flag is required"
    exit 1
  fi

  if [ -z "$IMAGE" ]; then
    echo "error: --image flag is required"
    exit 1
  fi

  if [ -z "$IMAGE_REF" ]; then
    echo "error: --image-ref flag is required"
    exit 1
  fi

  if [ -z "$REPO" ]; then
    echo "error: --repo flag is required"
    exit 1
  fi

  if [ -z "$VERSION" ]; then
    echo "error: --version flag is required"
    exit 1
  fi
}

parse_flags "$@"

tmp="$(mktemp -d)"
trap 'rm -rf $tmp' EXIT

# There might be no git tag reachable when testing, so we use a dummy version instead.
if [ "$PUSH" != "true" ]; then
  VERSION="0.1.0-dev"
fi

echo ">> Copying chart $CHART_PATH to $tmp"
cp -rL "$CHART_PATH" "$tmp"
chart_dir="$tmp/$(basename "$CHART_PATH")"

if [ -n "$IMAGE_TAG" ]; then
  echo ">> Updating image tag $IMAGE_TAG to $VERSION"
  yq -e ."$IMAGE_TAG" "$chart_dir/values.yaml" > /dev/null
  yq -i ."$IMAGE_TAG |= \"$VERSION\"" "$chart_dir/values.yaml"
else
  IMAGE="$IMAGE:$VERSION"
fi

if [ -n "$IMAGE_REF" ]; then
  echo ">> Updating image reference $IMAGE_REF to $IMAGE"
  yq -e ."$IMAGE_REF" "$chart_dir/values.yaml" > /dev/null
  yq -i ."$IMAGE_REF |= \"$IMAGE\"" "$chart_dir/values.yaml"
fi

chart_name=$(yq -e .name "$chart_dir/Chart.yaml")

echo ">> Packaging helm chart $chart_name"
helm package "$chart_dir" -d "$tmp" --version "$VERSION"

if [ "$PUSH" != "true" ]; then
  exit 0
fi

echo ">> Pushing helm chart $chart_name"
helm push "$tmp/$chart_name-$VERSION.tgz" "oci://$REPO/charts"
