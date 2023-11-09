package controller

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var clientScheme *runtime.Scheme
var logger logr.Logger
var ctx = context.TODO()
var extensionCounter = 1
var namespaceCounter = 1
var filterObjectCounter = 1

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
	Expect(istionetworkingClientGo.AddToScheme(clientScheme)).To(Succeed())

	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientScheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	createIngressNamespace()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func createNewNamespace() string {
	generatedName := "extension-test-" + strconv.Itoa(namespaceCounter)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: generatedName,
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
	namespaceCounter++
	return generatedName
}

func createIngressNamespace() {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: IngressNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).ShouldNot(HaveOccurred())
}

func deleteNamespace(name string) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, namespace)).ShouldNot(HaveOccurred())
}

func getNewExtension(namespace string) *extensionsv1alpha1.Extension {
	extensionCounter++
	return &extensionsv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "extension-" + strconv.Itoa(extensionCounter),
			Namespace: namespace,
			Annotations: map[string]string{
				"key": "value",
			},
		},
		Spec: extensionsv1alpha1.ExtensionSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: "acl",
			},
		},
		Status: extensionsv1alpha1.ExtensionStatus{},
	}
}

func GetUniqueShootName() string {
	filterObjectCounter++
	return "shoot" + strconv.Itoa(filterObjectCounter)
}
