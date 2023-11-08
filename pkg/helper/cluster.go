package helper

import (
	"context"
	"errors"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrClusterObjectNotComplete = errors.New("cluster.Seed or cluster.Shoot is nil")
)

func GetClusterForExtension(
	ctx context.Context,
	c client.Reader,
	extension *extensionsv1alpha1.Extension,
) (*controller.Cluster, error) {
	cluster, err := controller.GetCluster(ctx, c, extension.GetNamespace())
	if err != nil {
		return nil, err
	}
	if cluster.Seed == nil || cluster.Shoot == nil {
		return nil, ErrClusterObjectNotComplete
	}
	return cluster, nil
}
