package helper

import (
	"context"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetInfrastructureForExtension(
	ctx context.Context,
	c client.Reader,
	extension *extensionsv1alpha1.Extension,
	shootName string,
) (*extensionsv1alpha1.Infrastructure, error) {
	infra := &extensionsv1alpha1.Infrastructure{}
	namespacedName := types.NamespacedName{
		Namespace: extension.GetNamespace(),
		Name:      shootName,
	}

	if err := c.Get(ctx, namespacedName, infra); err != nil {
		return nil, err
	}
	return infra, nil
}
