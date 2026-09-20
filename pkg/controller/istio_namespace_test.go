package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deploymentIn(ns string) appsv1.Deployment {
	return appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "istio-ingressgateway", Namespace: ns}}
}

func TestFilterSeedIstioIngressDeployments(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []string
		want       []string
	}{
		{"seed default + operator virtual garden", []string{"istio-ingress", "virtual-garden-istio-ingress"}, []string{"istio-ingress"}},
		{"zonal gateway", []string{"istio-ingress--eu-west-1a", "virtual-garden-istio-ingress"}, []string{"istio-ingress--eu-west-1a"}},
		{"exposure class handler", []string{"istio-ingress-handler-internal", "virtual-garden-istio-ingress"}, []string{"istio-ingress-handler-internal"}},
		{"still ambiguous", []string{"istio-ingress", "istio-ingress--eu-west-1a"}, []string{"istio-ingress", "istio-ingress--eu-west-1a"}},
		{"nothing seed-like", []string{"virtual-garden-istio-ingress", "foo"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var in []appsv1.Deployment
			for _, ns := range tc.namespaces {
				in = append(in, deploymentIn(ns))
			}
			got := filterSeedIstioIngressDeployments(in)
			var gotNS []string
			for _, d := range got {
				gotNS = append(gotNS, d.Namespace)
			}
			if len(gotNS) != len(tc.want) {
				t.Fatalf("got %v, want %v", gotNS, tc.want)
			}
			for i := range tc.want {
				if gotNS[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", gotNS, tc.want)
				}
			}
		})
	}
}
