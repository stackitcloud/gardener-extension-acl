package controller

import (
	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("actuator unit test", func() {
	var (
		a         *actuator
		ext       *extensionsv1alpha1.Extension
		cluster   *extensionsv1alpha1.Cluster
		namespace string
	)

	BeforeEach(func() {
		namespace = createNewNamespace()
		ext = getNewExtension(namespace)
		cluster = getNewCluster(namespace)
		a = getNewActuator()

		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
		deleteNamespace(namespace)
	})

	Describe("createSeedResources", func() {
		It("//TODO should not return an error", func() {
			ctrlCluster := getClusterAsCtrlCluster(namespace)
			Expect(a.createSeedResources(ctx, &ExtensionSpec{}, ctrlCluster, namespace)).ToNot(HaveOccurred())
		})
	})
})

func getNewActuator() *actuator {
	return &actuator{
		client: k8sClient,
		config: cfg,
		logger: logger,
		extensionConfig: config.Config{
			ChartPath: "../../charts",
		},
	}
}

func getClusterAsCtrlCluster(namespace string) *controller.Cluster {
	// of course there are two different cluster types :)
	ctrlCluster, err := controller.GetCluster(ctx, k8sClient, namespace)
	Expect(err).ToNot(HaveOccurred())
	return ctrlCluster
}

func getNewCluster(namespace string) *extensionsv1alpha1.Cluster {
	return &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		Spec: extensionsv1alpha1.ClusterSpec{
			CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
			Seed:         runtime.RawExtension{Raw: []byte("{}")},
			Shoot:        runtime.RawExtension{Raw: []byte("{}")},
		},
	}
}
