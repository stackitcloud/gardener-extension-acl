# Separate EnvoyFilter for each Shoot for VPN

Having a single EnvoyFilter for all Shoots presents several drawbacks, and the current implementation has some issues.

Based on our experience with the implementation of EnvoyFilters for Shoot observability components, we created a new implementation for the VPN Envoy filters.
In a similar approach, we insert filters for each Shoot into one filter chain. Each Shoot has its own EnvoyFilter resource.
The filter of a Shoot allows traffic to other Shoots and checks the IP ranges for the actual Shoot.
If one filter denies access, the rest of the filter chain is not tested.
Only if all filters allow access, the traffic is forwarded to the VPN auth server.

The resulting filter chain in envoy looks like this:
```
"http_filters": [
  {
  "name": "acl-vpn",
  "typed_config": {
    "@type": "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
    "rules": {
    "policies": {
      "shoot--1": {
      "permissions": [
        {
        "header": {
          "name": "reversed-vpn",
          "string_match": {
          "contains": ".shoot--shoot--1."
          }
        }
        }
      ],
      "principals": [
        {
        "remote_ip": {
          "address_prefix": "1.2.3.4",
          "prefix_len": 32
        }
        },
...
      ]
      },
      "shoot--1-inverse": {
      "permissions": [
        {
        "not_rule": {
          "header": {
          "name": "reversed-vpn",
          "string_match": {
            "contains": ".shoot--shoot--1."
          }
          }
        }
        }
      ],
      "principals": [
        {
        "remote_ip": {
          "address_prefix": "0.0.0.0",
          "prefix_len": 0
        }
        }
      ]
      }
    }
    }
  }
  },
  {
  "name": "acl-vpn",
  "typed_config": {
    "@type": "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
    "rules": {
    "policies": {
      "shoot--2": {
      "permissions": [
        {
        "header": {
          "name": "reversed-vpn",
          "string_match": {
          "contains": ".shoot--shoot--2."
          }
        }
        }
      ],
      "principals": [
        {
        "remote_ip": {
          "address_prefix": "4.3.2.1",
          "prefix_len": 32
        }
        },
...
      ]
      },
      "shoot--2-inverse": {
      "permissions": [
        {
        "not_rule": {
          "header": {
          "name": "reversed-vpn",
          "string_match": {
            "contains": ".shoot--shoot--2."
          }
          }
        }
        }
      ],
      "principals": [
        {
        "remote_ip": {
          "address_prefix": "0.0.0.0",
          "prefix_len": 0
        }
        }
      ]
      }
    }
    }
  }
  },
...
]
```

During the migration from the legacy shared EnvoyFilter to individual EnvoyFilters, we check during a reconciliation of a shoot if the new VPN EnvoyFilter is present.
In such instances, the shoot-specific component of this shoot is removed from the legacy filter.
Once the new VPN EnvoyFilter is created for all shoots, the legacy VPN EnvoyFilter will be deleted.
As the creation of the new EnvoyFilter by the GardenerResourceManager takes some time, two shoot reconciliations are needed before the shoot-specific component is fully removed from the legacy filter.
Throughout this process, the VPN endpoint remains protected by envoy filter rules.
Between the first and second reconciliation, the rules are duplicated in the envoy configuration, but this should not pose any issues.
