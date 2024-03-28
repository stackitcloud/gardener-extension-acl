# Shoot observability components

With https://github.com/gardener/gardener/pull/9038 `nginx-ingress` moved behind `istio`. This allows us to filter access to observability components `alertmanager`, `plutono`, `prometheus` and `vali`. When SNI matches the wildcard ingress domain, traffic is send to the nginx ingress service. 

![Ingress Chain](./docs/adr/ingress-chain.svg)

There is a single `listener` for all shoots. Filters for all shoots ard inserted into one filter chain. The filter of a shoot allows traffic to other shoots and checks the ip ranges for the actual shoot. If one filter denys the access, the rest of the filter chain is not tested. Only if all filters allow access, the traffic is forwarded to the nginx service.

The resulting filter chain in envoy looks like this:

```
{
     "listener": "0.0.0.0_9443",
       },
       "filter_chains": [
        {
         "filter_chain_match": {
          "server_names": [
           "*.ingress.ingress.seed-name.seed.example.com"
          ]
         },
         "filters": [
          {
           "name": "shoot-a",
          ...
            "rules": {
             "policies": {
              "shoot-a-inverse": {
               "permissions": [
                {
                 "not_rule": {
                  "requested_server_name": {
                   "suffix": "-shoot-a.ingress.seed-name.seed.example.com"
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
              "shoot-a": {
               "permissions": [
                {
                 "requested_server_name": {
                  "suffix": "-shoot-a.ingress.seed-name.seed.example.com"
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
              ....
               ]
              },
              }
             }
          },
          {
          "name": "shoot-b",
          ...
            "rules": {
             "policies": {
              "shoot-a-inverse": {
               "permissions": [
                {
                 "not_rule": {
                  "requested_server_name": {
                   "suffix": "-shoot-b.ingress.seed-name.seed.example.com"
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
              "shoot-b": {
               "permissions": [
                {
                 "requested_server_name": {
                  "suffix": "-shoot-b.ingress.seed-name.seed.example.com"
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
              ....
               ]
              },
              }
             }
            },
         ]
        }
```
