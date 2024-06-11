package gardener

import (
	"context"
	"fmt"
	"os"

	gardener_api "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

//go:generate mockery --name=ShootClient
type ShootClient interface {
	Create(ctx context.Context, shoot *gardener_api.Shoot, opts v1.CreateOptions) (*gardener_api.Shoot, error)
	Get(ctx context.Context, name string, opts v1.GetOptions) (*gardener_api.Shoot, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	//List(ctx context.Context, opts v1.ListOptions) (*gardener.ShootList, error)
}

func NewRestConfigFromFile(kubeconfigFilePath string) (*restclient.Config, error) {
	rawKubeconfig, err := os.ReadFile(kubeconfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Gardener Kubeconfig from path %s: %s", kubeconfigFilePath, err.Error())
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(rawKubeconfig)
	if err != nil {
		return nil, err
	}

	return restConfig, err
}
