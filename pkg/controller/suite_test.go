package controller

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	//+kubebuilder:scaffold:imports
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var clientScheme *runtime.Scheme
var logger logr.Logger
var ctx = context.TODO()
var extensionCounter = 1
var namespaceCounter = 1

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Extension Test Suite")
}

var _ = BeforeSuite(func() {
	var err error

	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

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
