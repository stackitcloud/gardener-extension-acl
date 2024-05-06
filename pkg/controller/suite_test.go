package controller

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"istio.io/api/networking/v1beta1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
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
	createGardenNamespace()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func createGardenNamespace() {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "garden",
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
}

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

func createNewIstioNamespace() string { //nolint:gocritic // no named results needed
	generatedName := "istio-ingress-namespace" + strconv.Itoa(istioNamespaceCounter)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: generatedName,
			Labels: map[string]string{
				"istioNamespace": generatedName,
			},
		},
	}
	istioNamespaceCounter++
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
	return generatedName
}

func createNewIstioDeployment(namespace string, labels map[string]string) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "istio-ingressgateway-",
			Namespace:    namespace,
			Labels:       labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "fake",
							Image: "pause",
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, deployment)).ShouldNot(HaveOccurred())
}

func createNewGateway(name, shootNamespace string, labels map[string]string) *istionetworkingv1beta1.Gateway {
	gw := &istionetworkingv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shootNamespace,
		},
		Spec: v1beta1.Gateway{
			Selector: labels,
		},
	}
	Expect(k8sClient.Create(ctx, gw)).ShouldNot(HaveOccurred())
	return gw
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
				Object: &gardencorev1beta1.Seed{
					Spec: gardencorev1beta1.SeedSpec{
						Networks: gardencorev1beta1.SeedNetworks{
							Nodes: ptr.To("10.250.0.0/24"),
							Pods:  "10.10.0.0/24",
						},
						Ingress: &gardencorev1beta1.Ingress{
							Domain: "ingress.test",
						},
					},
				},
			},
			Shoot: runtime.RawExtension{
				Object: &gardencorev1beta1.Shoot{
					ObjectMeta: metav1.ObjectMeta{
						Name: shootNamespace,
					},
					Spec: gardencorev1beta1.ShootSpec{
						Networking: &gardencorev1beta1.Networking{
							Nodes: nil,
							Pods:  nil,
						},
					},
					Status: gardencorev1beta1.ShootStatus{
						TechnicalID: shootNamespace,
						// needed to wait until k8s server is up and running
						AdvertisedAddresses: []gardencorev1beta1.ShootAdvertisedAddress{{
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
