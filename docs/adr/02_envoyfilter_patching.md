# `EnvoyFilter` Patching

Based on [ADR01](01_refine_extension.md), the implementation of the ACL
extension was shifted from `AuthorizationPolicies` to `EnvoyFilters`. (For the
reasoning behind this decision, see the ADR.)

Implementing access control with `EnvoyFilters` came with its own set of
challenges, and this ADR aims at recording the reasoning behind the solutions we
found for them.

## The Role of `EnvoyFilters` in Istio

While Istio comes with a bunch of "high-level" CRDs to define the networking
behavior of a cluster (like e.g. `Ingress`), `EnvoyFilters` are a way to
directly patch and manipulate the Envoy configuration that results from the
higher-level resources. `EnvoyFilters` are very powerful, but also complex. Usually, if
you can implement a desired networking behavior without `EnvoyFilters`, you
should definitely do it. As discussed in [ADR01](01_refine_extension.md), we
must chose `EnvoyFilters` for the task at hand, though.

The ingress of seed clusters is where the configuration of ACL rules takes
place, and consequently, where we must hook in with our extension.

Gardener already comes pre-equipped with a few `EnvoyFilter` objects (see any
seed cluster in your environment) that already influence the ingress behavior.

There are three areas where a user-defined ACL needs to be respected:

1. The API server networking config
2. The VPN networking config
3. The internal networking config

While API server and VPN networking configuration is easier to achieve, a
special workaround for internal networking is necessary.

## Simple Case: Deploying Additional `EnvoyFilters`

The general idea of most Gardener extension controllers is to deploy additional
resources (usually using the Gardener Resource Manager) which then have some
effect in the shoot namespace of the cluster that requested the extension. We
considered the same approach for the ACL extension.

The original Gardener networking configuration for API server networking (the
first item on the list) stems from an Istio `Gateway` resource. This makes it
easy for us to alter it after the fact by adding an `EnvoyFilter`, patching the
resulting config with user-defined rules.

The same goes for item two, the VPN config, which stems from an `EnvoyFilter`
itself. Istio is (in this case) capable of applying a chain of `EnvoyFilters`.

We could therefore solve two of our three requirements by creating a Helm
template for an additional `EnvoyFilter` resource. This template is filled with
the user-defined rules from the extensions `spec.providerConfig` field. This is
the main task of the [actuator](pkg/controller/actuator.go).

The internal networking config (item three), also comes from an `EnvoyFilter`
deployed by Gardener. Due to the fact that it hooks into the patching mechanism
of Istio differently, we were unable to patch this `EnvoyFilter` like we did for
VPN networking. In fact, we couldn't find any way to insert the additional ACL
rules into this `EnvoyFilter` patch whatsoever, which we consider a bug in
Istio. Istio even provides a `priority` field for `EnvoyFilters`, but it also
didn't have any effect. (See the
[issue we opened with Istio for our problem](https://github.com/istio/istio/issues/41536).)

Because of this perceived bug, even a change to the Gardener core codebase (e.g.
adding a name to the `filter_chain` or the `filters` fields in the original
`EnvoyFilter`) wouldn't solve the problem at the moment.

## Complicated Case: Mutating Webhook for Specific `EnvoyFilters`

Because of this challenge with Istio not merging several `EnvoyFilters` as we'd
expect, case three of our list was still unsolved. The only idea we had left was
to implement a mutating webhook that acts on specific `EnvoyFilters` and injects
the user-defined ACL rules _into the original `EnvoyFilter`_ produced by
Gardener upon creation or updates.

See the [webhook](pkg/webhook/webhook.go) code for the details of the
implementation. In short, we trigger the webhook only for `EnvoyFilters`
whose name starts with `shoot--`. This is the naming pattern for `EnvoyFilters`
containing the internal networking configuration. We then check if there is an
`Extension` of `type: acl` in the corresponding shoot namespace. If yes, and if
it contains rules, we patch the incoming `EnvoyFilter` with those rules and send the
result back to the API server.

A challenge with this approach is that the webhook (in contrast to the extension
controller) is only triggered when `EnvoyFilters` are created or updated.
However, the webhook also needs to act when a change is made to the `Extension`
resource (e.g. if the user changed their ACL rules). This was solved on the
actuator side in the controller. Additionally to creating the aforementioned
`EnvoyFilters` as managed resources, it also calculates a hash of all rules in
the `Extension` spec. This hash is inserted as an annotation in the appropriate
`EnvoyFilter` resource (`shoot--...`). This update triggers the webhook. It
fetches the changed `Extension` and patches the `EnvoyFilter` with the updated
rules.

In case of extension deletion (the user doesn't need ACL anymore), the
annotation is removed and the webhook removes all ACL-based rules, recreating
the original state of the shoots `EnvoyFilter` resource.


