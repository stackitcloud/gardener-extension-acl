# Changing How the Webhook is Triggered

In [ADR02](02_envoyfilter_patching.md), we defined how we create/mutate
`EnvoyFilters` in order to provide ACL functionality.

In the "Complicated Case 1: Mutating Webhook for Specific `EnvoyFilters`"
section, ADR02 described how a hashing mechanism is used to update an annotation
every time the contents of the `Extension.spec` changes, causing the
`MutatingWebhook` to execute.

This however neglected that there can be changes to the ACL configuration of a
`Shoot` even when the `Extension.spec` hasn't changed, namely in the form of the
`additionalAllowedCIDRs` configuration. This setting can be changed for the
entire extension using the Helm chart it is deployed with. Adding additional
CIDRs to this field would update all associated `EnvoyFilters`, except the ones
the Webhook is responsible for.

The new implementation of the trigger mechanism is both simpler and more
thorough: Every time an `Extension` is reconciled, the actuator sends an empty
patch for the associated `EnvoyFilter` that the Webhook is responsible for.
This removes the need for the hashing logic, and makes sure that the Webhook is
run every time the extension is reconciled. The problem with the neglected
`additionalAllowedCIDRs` setting is solved by this approach.
