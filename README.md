# Gardener ACL Extension

**TL;DR: The Gardener ACL extension allows you to limit the access to shoot
clusters using an allow-list mechanism. Basically, it looks like this:**

```yaml
type: acl
providerConfig:
  rule:
    action: ALLOW
    type: remote_ip
    cidrs:
      - "1.2.3.4/24"
      - "10.250.0.0/16"
      - ...
```

Please read on for more information.

## Background, Functionality & Limitations

Gardener introduced *Shoot API Server SNI* with [GEP08](https://github.com/gardener/gardener/blob/master/docs/proposals/08-shoot-apiserver-via-sni.md).

Using Istio, Gardener configures a single ingress gateway per seed to proxy
traffic to all API servers on this seed based on some criteria. At it's core,
Istio configures an envoy proxy using a set of
[Kubernetes CRDs](https://istio.io/latest/docs/reference/config/networking/).
We can hook into this mechanism and insert additional configuration, which
further limits the access to a specific cluster.

Broadly speaking, there are three different external traffic flows:

1. Kubernetes API Listener (via SNI name)
1. Kubernetes Service Listener (internal flow)
1. VPN Listener

These ways are described in more detail in the aforementioned GEP. Essentially,
these three ways are all represented by a specific Envoy listener with filters.
The extension needs to hook into each of these filters (and their filter chains)
to implement the desired behavior. Unfortunately, all three types of access
require a unique way of handling them, respectively.

![Listener Overview](./docs/listener-overview.svg)

1. **SNI Access** - The most straightforward approach. Wen can deploy one
   additional `EnvoyFilter` per shoot with enabled ACL extension. It contains a
   filter patch that matches on the shoot SNI name and specifies an `ALLOW` rule
   with the provided IPs.
1. **Internal Flow** - Gardener creates one `EnvoyFilter` per shoot that defines
   this listener. Unfortunately, it doesn't have any criteria we could use to
   match it with an additional `EvnoyFilter` spec on a per-shoot basis, and
   we've tried a lot of things to make it work. On top of that, a behavior that
   we see as [a bug in Istio](https://github.com/istio/istio/issues/41536)
   prevents us from working with priorities here, which was the closest we got
   to make it work. Now instead, the extension deploys a `MutatingWebhook` that
   intercepts creations and updates of `EnvoyFilter` resources starting with
   `shoot--` (which is their only common feature). We then insert our
   rules. To make this work with updates to `Extension` objects, the controller
   dealing with 1) also updates a hash annotation on these `EnvoyFilter`
   resources every time the respective ACL extension object is updated.
1. **VPN Access** - All VPN traffic moves through the same listener. This
   requires us to create only a single `EnvoyFilter` for VPN that contains
   **all** rules of all shoots that have the extension enabled. And, conversely,
   we need to make sure that traffic of all shoots that don't have the
   extension enabled is still able to pass through this filter unhindered. We
   achieve this by not only creating a policy for every shoot with ACL enabled,
   but also an "inverted" policy which matches all shoots that don't have ACL
   enabled. All these policies are then put in a single EnvoyFilter patch.

Because of the last point, we currently see no way of allowing the user to
define multiple rules of different action types (`ALLOW` or `DENY`). Instead, we
only support a single `ALLOW` rule per shoot, which is in our opinion the best
trade-off to efficiently secure Kubernetes API servers.

See [ADR02](./docs/adr/02_envoyfilter_patching.md) for a more in-depth
discussion of the challenges we had.

## Cloud specific settings

### Openstack

In order for the internal VPN traffic to work, the router IP adresses from the
shoot openstack projects have to get allowlisted in the ACL extension.

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
