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
      "shoot-1": {
      "permissions": [
        {
        "header": {
          "name": "reversed-vpn",
          "string_match": {
          "exact": "outbound|1194||vpn-seed-server.shoot--shoot-1.svc.cluster.local"
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
      "shoot-1-inverse": {
      "permissions": [
        {
        "not_rule": {
          "header": {
          "name": "reversed-vpn",
          "string_match": {
            "exact": "outbound|1194||vpn-seed-server.shoot--shoot-1.svc.cluster.local"
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
      "shoot-2": {
      "permissions": [
        {
        "header": {
          "name": "reversed-vpn",
          "string_match": {
          "exact": "outbound|1194||vpn-seed-server.shoot--shoot-2.svc.cluster.local"
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
      "shoot-2-inverse": {
      "permissions": [
        {
        "not_rule": {
          "header": {
          "name": "reversed-vpn",
          "string_match": {
            "exact": "outbound|1194||vpn-seed-server.shoot--shoot-2.svc.cluster.local"
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