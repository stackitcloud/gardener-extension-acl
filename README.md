# Gardener ACL Extension

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
Helm chart in the `charts/gardener-extension` directory, run `earthly +generate-deploy` to
re-create this value. The `providerConfig.values.image.tag` field is populated
with the contents of the `VERSION` file in the repository root.

**NOTE**: The contents of the `VERSION` file need to be a valid SemVer. So,
during development, you need to first run `earthly +generate-deploy` and then manually
replace the `providerConfig.values.image.tag` field with the current ID of the
feature branch concourse build.

## Tests

To run the test suite, execute:

```bash
earthly +test
```

Place all needed Gardener CRDs in the `upstream-crds` directory, they get
installed automatically in the envtest cluster.

See the [actuator_test.go](pkg/controller/actuator_test.go) for a minimal test
case example.

## Webhook Development

The `controller-runtime` package always creates a Webhook Server that relies on
TLS, and therefore requires a certificate. As this complicates local
development, you can use the following approach to tunnel the server running
locally with a self-signed certificate through `localtunnel`, exposing it via
public TLS which the Kubernetes API server accepts.

Generate selfsigned *certificates*:

```bash
bash hack/gen-certs.sh
```

Start the local webhook server with the certs configured:

```bash
go run cmd/webhook/main.go --cert-dir certs --key-name server.key --cert-name server.crt
```

Create a tunnel that exposes your local server:

```bash
npx localtunnel --port 9443 --local-https --local-ca certs/ca.crt --local-cert certs/server.crt --local-key certs/server.key --subdomain webhook-dev
```

Finally, apply the `MutatingWebhookConfiguration`:

```bash
kubectl apply -f example/40-webhook.yaml
```
