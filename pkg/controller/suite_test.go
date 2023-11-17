package controller

import (
	"context"
	"maps"
	"path/filepath"
	"strconv"
	"testing"

	gardnercorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"istio.io/api/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	//+kubebuilder:scaffold:imports
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var clientScheme *runtime.Scheme
var logger logr.Logger
var ctx = context.TODO()
var shootNamespaceCounter = 1
var istioNamespaceCounter = 1

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	logger = testr.New(t)

	RunSpecs(t, "Extension Test Suite")
}

var _ = BeforeSuite(func() {
	var err error

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "upstream-crds")},
		ErrorIfCRDPathMissing: true,
	}

	clientScheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(clientScheme)).To(Succeed())
	Expect(extensionsv1alpha1.AddToScheme(clientScheme)).To(Succeed())
	Expect(resourcesv1alpha1.AddToScheme(clientScheme)).To(Succeed())
	Expect(apiextensions.AddToScheme(clientScheme)).To(Succeed())
	Expect(istionetworkingv1beta1.AddToScheme(clientScheme)).To(Succeed())
	Expect(istionetworkingv1alpha3.AddToScheme(clientScheme)).To(Succeed())

	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientScheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func createNewShootNamespace() string {
	generatedName := "shoot--project--test" + strconv.Itoa(shootNamespaceCounter)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: generatedName,
		},
	}
	shootNamespaceCounter++
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
	return generatedName
}

func createNewIstioNamespace() (string, map[string]string) { //nolint:gocritic // no named results needed
	generatedName := "istio-ingress-namespace" + strconv.Itoa(istioNamespaceCounter)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: generatedName,
			Labels: map[string]string{
				"istioNamespace": generatedName,
				"label":          "test",
			},
		},
	}
	istioNamespaceCounter++
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
	return generatedName, namespace.GetLabels()
}

func createNewGateway(shootNamespace string, labels map[string]string) {
	selectorLabels := maps.Clone(labels)
	selectorLabels["app"] = "test"

	gw := &istionetworkingv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-apiserver",
			Namespace: shootNamespace,
		},
		Spec: v1beta1.Gateway{
			Selector: selectorLabels,
		},
	}
	Expect(k8sClient.Create(ctx, gw)).ShouldNot(HaveOccurred())
}

func createNewExtension(shootNamespace string, providerConfig []byte) *extensionsv1alpha1.Extension {
	ext := &extensionsv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acl",
			Namespace: shootNamespace,
		},
		Spec: extensionsv1alpha1.ExtensionSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: "acl",
				ProviderConfig: &runtime.RawExtension{
					Raw: providerConfig,
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, ext)).ShouldNot(HaveOccurred())
	return ext
}

func createNewEnvoyFilter(shootNamespace, istioNamespace string) {
	envoyfilter := &istionetworkingv1alpha3.EnvoyFilter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shootNamespace,
			Namespace: istioNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, envoyfilter)).ShouldNot(HaveOccurred())
}

func createNewInfrastructure(shootNamespace string) {
	cluster := &extensionsv1alpha1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shootNamespace,
			Namespace: shootNamespace,
		},
		Spec: extensionsv1alpha1.InfrastructureSpec{},
	}

	Expect(k8sClient.Create(ctx, cluster)).ShouldNot(HaveOccurred())
}

func createNewCluster(shootNamespace string) {
	cluster := &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: shootNamespace,
		},
		Spec: extensionsv1alpha1.ClusterSpec{
			CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
			Seed: runtime.RawExtension{
				Object: &gardnercorev1beta1.Seed{
					Spec: gardnercorev1beta1.SeedSpec{
						Networks: gardnercorev1beta1.SeedNetworks{
							Nodes: ptr.To("10.250.0.0/24"),
							Pods:  "10.10.0.0/24",
						},
					},
				},
			},
			Shoot: runtime.RawExtension{
				Object: &gardnercorev1beta1.Shoot{
					ObjectMeta: metav1.ObjectMeta{
						Name: shootNamespace,
					},
					Spec: gardnercorev1beta1.ShootSpec{
						Networking: &gardnercorev1beta1.Networking{
							Nodes: nil,
							Pods:  nil,
						},
					},
					Status: gardnercorev1beta1.ShootStatus{ // needed to wait until k8s server is up and running
						AdvertisedAddresses: []gardnercorev1beta1.ShootAdvertisedAddress{{
							Name: "test",
							URL:  "https://test",
						}},
					},
				},
			},
		},
	}

	Expect(k8sClient.Create(ctx, cluster)).ShouldNot(HaveOccurred())
}

func deleteNamespace(name string) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, namespace)).ShouldNot(HaveOccurred())
}
