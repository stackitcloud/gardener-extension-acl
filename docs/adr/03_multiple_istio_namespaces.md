# Supporting Multiple Istio Namespaces

Before this change, the `Namespace` used by the extension was hardcoded to
`istio-ingress`. This is the default name for the Istio namespace, where the
Ingress is deployed. In case of High Availability Shoots (see
[Gardener HA Control Plane Best Practices](https://gardener.cloud/docs/gardener/shoot_high_availability_best_practices/)),
or when using
[ExposureClasses](https://gardener.cloud/docs/gardener/exposureclasses/), the
name of this Ingress namespace can change dynamically.

Therefore, the extension is rewritten to dynamically determine the correct
namespace in which to create/mutate `EnvoyFilter` objects. This is achieved
using the following logic:

1. The extension gets the `Gateway` object called `kube-apiserver` in the
   current shoot namespace. This object is created by Gardener for every Shoot.
   The
   [Gateway](https://istio.io/latest/docs/reference/config/networking/gateway/)
   resource contains a `spec.selector` field, which contains label selectors for
   the `Deployment` of the gateway controller that is  responsible for the
   defined `Gateway`.
2. From these label selectors, we can obtain the `Namespace` where the Istio
   Ingress Gateway is deployed. This is achieved by a `LIST` operation
   on `Deployments`, filtered by the selector labels.

The namespace where this Deployment runs is the target namespace for all
`EnvoyFilters` deployed/mutated by the extension. It is also recorded in the
`Status` of the ACL `Extension` object. This enables the extension to support
changing the istio namespace for a  running shoot. This will happen for example
if a shoot is updated to High Availability or when using an `ExposureClass`.
Also, when the ingress namespaces changes, the old `acl-vpn` `EnvoyFilter` will
be cleaned up or get deleted (to "free" the previous ingress namespace from
restrictive filter policies.)
