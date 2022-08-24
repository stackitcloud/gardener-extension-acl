# Gardener Example Extension for Managed Resources

## Healthchecks

Gardener provides a [Health Check Library](https://gardener.cloud/docs/gardener/extensions/healthcheck-library/)
that we can use to monitor the health of resources that our extension is
responsible for. Example: If the extension controller deploys a Gardener
`ManagedResource`, we can define a health check on the extension that checks for
the health of this `ManagedResource`. This lets the extension reflect the state
of the resources it is responsible for. This is expressed by status conditions
in the extension resource itself (one per health check).

## Generating ControllerRegistration and ControllerDeployment

Extensions are installed on a Gardener cluster by deploying a
`ControllerRegistration` and a `ControllerDeployment` object to the garden
cluster. In this repository, you find an example for both of these resources in
the `example/controller-registration.yaml` file. 

The `ProviderConfig.chart` field contains the entire Helm chart for your
extension as a gzipped and then base64-encoded string. After you altered this
Helm chart in the `charts/gardener-extension` directory, run `make generate` to
re-create this value. The `providerConfig.values.image.tag` field is populated
with the contents of the `VERSION` file in the repository root.

**NOTE**: The contents of the `VERSION` file need to be a valid SemVer. So,
during development, you need to first run `make generate` and then manually
replace the `providerConfig.values.image.tag` field with the current ID of the
feature branch concourse build.

## Tests

To run the test suite, execute:

```bash
earthly +test
```

Place all needed Gardener CRDs in the `upstream-crds` directory, they get
installed automatically in the envtest cluster.

See the [actuator_test.go](./pkg/controller/actuator_test.go) for a minimal test
case example.
