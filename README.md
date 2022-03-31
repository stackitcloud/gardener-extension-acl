# Gardener Example Extension for Managed Resources

## Make It Your Own

1. Define the name of your extension controller.
   * Rename the `cmd/gardener-extension-example` directory. The naming
     convention is to keep the `gardener-extension-` part.
   * Do a search and replace in all files to update imports, the `go.mod`, etc.
     Use `gardener-extension-example` as the search term.
   * Update the `NAME` variable in the Makefile.
2. Specify the Type of extension you want to reconcile.
   * In the [controller options: Type constant](pkg/controller/add.go).
   * Update the [extension example spec.type](example/30-extension.yaml)
3. Add any configuration options for your extension to the `ExtensionOptions`
   struct in the [command's options.go](pkg/cmd/options.go). They are CLI flags
   for you to pass to the binary. Any  of these configuration options can be
   passed to the `ControllerOptions` in the
   [pkg/controller/config/config.go](pkg/controller/config/config.go) file.
   (Using the `Apply()` method in [options.go](pkg/cmd/options.go))

Now you can start to implement the [actuator](pkg/controller/actuator.go)
methods (Reconcile, Delete, Migrate, Restore). Most likely, your extension
controller will introduce additional Kubernetes resources, either to the seed or
the shoot clusters. Consider Gardener's
[ManagedResource](https://github.com/gardener/gardener/blob/master/docs/concepts/resource-manager.md)
approach to implement this, and see the `charts` directory for sample
implementations.

You can also add health checks to reflect the extension's state in its `Status`
field. Read more about health checks below.

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
make test
```

Place all needed Gardener CRDs in the `upstream-crds` directory, they get
installed automatically in the envtest cluster.

See the [actuator_test.go](./pkg/controller/actuator_test.go) for a minimal test
case example.
