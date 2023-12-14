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

While the API server networking configuration is easier to achieve, a
special workaround is necessary for the other two areas.

## Simple Case: Deploying Additional `EnvoyFilters`

The general idea of most Gardener extension controllers is to deploy additional
resources (usually using the Gardener Resource Manager) which then have some
effect in the shoot namespace of the cluster that requested the extension. We
considered the same approach for the ACL extension.

The original Gardener networking configuration for API server networking (the
first item on the list) stems from an Istio `Gateway` resource. This makes it
easy for us to alter it after the fact by adding an `EnvoyFilter`, patching the
resulting config with user-defined rules.

We could therefore solve one of our three requirements by creating a Helm
template for an additional `EnvoyFilter` resource. This template is filled with
the user-defined rules from the extensions `spec.providerConfig` field. This is
the main task of the [actuator](pkg/controller/actuator.go).

The internal networking config (item three) also comes from an `EnvoyFilter`
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

## Complicated Case 1: Mutating Webhook for Specific `EnvoyFilters`

Because of this challenge with Istio not merging several `EnvoyFilters` as we'd
expect, case three needs special treatment. The idea we had was to implement a
mutating webhook that acts on specific `EnvoyFilters` and injects the
user-defined ACL rules _into the original `EnvoyFilter`_ produced by Gardener
upon creation or updates.

See the [webhook](pkg/webhook/webhook.go) code for the details of the
implementation. In short, we trigger the webhook only for `EnvoyFilters`
whose name starts with `shoot--`. This is the naming pattern for `EnvoyFilters`
containing the internal networking configuration. We then check if there is an
`Extension` of `type: acl` in the corresponding shoot namespace. If yes, and if
it contains rules, we patch the incoming `EnvoyFilter` with those rules and send
the result back to the API server.

> The following description of the hashing procedure to trigger the webhook has
> been superseded by [ADR04](04_trigger_webhook.md).

A challenge with this approach is that the webhook (in contrast to the extension
controller) is only triggered when `EnvoyFilters` are created or updated.
However, the webhook also needs to act when a change is made to the `Extension`
resource (e.g. if the user changed their ACL rules). This was solved on the
actuator side in the controller. Additionally to creating the aforementioned
`EnvoyFilters` as managed resources, it also calculates a hash of the rule in
the `Extension` spec. This hash is inserted as an annotation in the appropriate
`EnvoyFilter` resource (`shoot--...`). This update triggers the webhook. It
fetches the changed `Extension` and patches the `EnvoyFilter` with the updated
rules.

In case of extension deletion (the user doesn't need ACL anymore), the
annotation is removed and the webhook removes all ACL-based rules, recreating
the original state of the shoots `EnvoyFilter` resource.

## Complicated Case 2: A Single `EnvoyFilter` for All VPN Rules

The VPN listener is also special, in that it bundles the entire traffic for all
shoots. (The other two flows have one listener per shoot.) The reason for this
is that all VPN traffic goes to the VPN auth server first and from there to the
respective API server.

This means, to adequately support ACLs in the case of VPN, we need to create a
combined `EnvoyFilter` (exactly one per seed). It roughly looks like this
(abbreviated example):

```yaml
...
configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: GATEWAY
      listener:
        name: 0.0.0.0_8132
    patch:
      operation: INSERT_FIRST
      value:
        name: acl-vpn
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC
          rules:
            action: ALLOW
            policies:
              # this is the policy that allows all other shoots to pass through
              acl-vpn-inverse:
                  permissions:
                  - any: true
                  principals:
                    - or_ids:
                        ids:
                          - not_id:
                              header:
                                name: reversed-vpn
                                string_match:
                                  contains: shoot--projectname--shootname1
                          - not_id:
                              header:
                                name: reversed-vpn
                                string_match:
                                  contains: shoot--projectname--shootname2
              # this is the shoot-specific policy, matching on both the header
              # AND the provided list of allowed IP ranges
              shoot--projectname--shootname1:
                permissions:
                - any: true
                principals:
                - and_ids:
                    ids:
                    - or_ids:
                        ids:
                        - remote_ip:
                            address_prefix: 0.0.0.0
                            prefix_len: 0
                        - remote_ip:
                            address_prefix: 10.250.0.0
                            prefix_len: 16
                        - remote_ip:
                            address_prefix: 10.96.0.0
                            prefix_len: 11
                    - header:
                        name: reversed-vpn
                        string_match:
                          contains: shoot--projectname--shootname
              # one more policy for each shoot that has the extension enabled
              shoot-projectname-shootname2:
                ...
```

You can see that we construct one policy for each shoot with enabled ACL
extension, and another rule matching all shoots **without** ACL extension
(called `acl-vpn-inverse`) that prevents those shoots from being denied VPN
access.

This case of combining all rules into a single `EnvoyFilter` object means that
we can only support either a `DENY` or an `ALLOW` rule, but not both. We decided
to go with `ALLOW`, because it makes the most sense for Kubernetes API servers
in our opinion.

A possible idea we had to solve this problem and allow the user to define
multiple rules would be to not deal with VPN traffic access control in the Envoy
configuration. Instead, Envoy would pass all the VPN traffic to the VPN auth
server unhindered.

The auth server currently only checks for a regex match on
the `reversed-vpn` header. It could be extended to read in a `ConfigMap` written
by the ACL extension (containing the shoot access rules). Based on data from
this `ConfigMap`, it could do more sophisticated authorization by also checking
for those rules.
