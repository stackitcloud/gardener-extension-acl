#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

charts=(
  gardener-extension-acl
  gardener-extension-admission-acl/charts/application
  gardener-extension-admission-acl/charts/runtime
)

helm_artifacts=artifacts/charts
rm -rf "$helm_artifacts"
mkdir -p "$helm_artifacts"
cp -r charts/gardener-extension-* "$helm_artifacts"

function image() {
  grep "$1" images.txt
}

function image_repo() {
  image "$1" | cut -d ':' -f 1
}

function image_tag() {
  image "$1" | cut -d ':' -f 2-
}

function update_chart_values() {
  for chart in charts/*; do
    name=$(basename "$chart")
    values_file="$helm_artifacts/$name/values.yaml"
    if yq -e '.image | has("repository")' "$values_file" >/dev/null 2>&1; then
      # update charts that have a ".image" map
      yq -i "\
        ( .image.repository = \"$(image_repo "$name")\" ) | \
        ( .image.tag = \"$(image_tag "$name")\" )\
      " "$values_file"
    elif yq -e '. | has("image")' "$values_file" >/dev/null 2>&1; then
      # update charts that have a ".image" field
      yq -i "\
        ( .image = \"$(image "$name")\" )\
      " "$values_file"
    fi
  done
}

# inject image references into chart values
update_chart_values

# push to registry
if [ "$PUSH" != "true" ] ; then
  echo "Skip pushing artifacts because PUSH is not set to 'true'"
  exit 0
fi

for chart in "${charts[@]}"; do
  chart_name=$(yq -e .name "$helm_artifacts/$chart/Chart.yaml")
  helm package "$helm_artifacts/$chart" --version "$TAG" -d "$helm_artifacts" >/dev/null 2>&1
  helm push "$helm_artifacts/$chart_name-"* "oci://$REPO/charts"
done
