# Refine Extension

## Make extension deploy a EnvoyFilter instead of AuthorizationPolicy

With AuthorizationPolicies there is a rollout problem. 
After creation of the first AuthorizationPolicy all other traffic to other Shoots will be blocked.
When the extension way is used, only after a Shoot reconcile the AuthorizationPolicy will be created.
So at best, all AuthorizationPolicies need to be created after the extension startup.
One could implement a bootstrap function to create AuthorizationPolicies with allow for all Shoots, but that is error prone.

Also it is not nice that there must be a AuthorizationPolicy for every Shoot without any use.

So the idea is to switch from AuthorizationPolicy to an EnvoyFilter which does the same, but maybe only for the one Shoot.

## Change ProviderConfig of Extension

Make it possible to configure mutliple rules with differnt actions (allow/deny). Also add the type (remote or not) to configuration.

```yaml
kind: Shoot
spec:
  extensions:
  - type: acl
    providerConfig:
    - cidrs:
      - x.x.x.x/24
      - y.y.y.y/24
      action: ALLOW # default
      type: ip|remote # use IPBlocks or RemoteIPBlocks
    - cidrs:
      - ......
```

This format would make it possible to have a webhook which checks the provider configuration and then adds the worker node router IP and the type.

## Additional Ideas

- Maybe log blocks with EnvoyFilter
- Provider Extension Webhook to set NAT/router IP so that worker nodes are able to reach API

# Gardener Core vs Extension

All functionality could also be included in Gardener Core. There are some reasons to do it as extension:

- There are provider specific configuration needed (proxy protocol, service traffic policy)
- As there are only additional settings, it is a good use case for an extension