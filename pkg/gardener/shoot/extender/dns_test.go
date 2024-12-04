package extender

import (
	"testing"

	gardener "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	imv1 "github.com/kyma-project/infrastructure-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDNSExtender(t *testing.T) {
	t.Run("Create DNS config for create scenario", func(t *testing.T) {
		// given
		secretName := "my-secret"
		domainPrefix := "dev.mydomain.com"
		dnsProviderType := "aws-route53"
		runtimeShoot := imv1.Runtime{
			Spec: imv1.RuntimeSpec{
				Shoot: imv1.RuntimeShoot{
					Name: "myshoot",
				},
			},
		}
		extender := NewDNSExtender(secretName, domainPrefix, dnsProviderType)
		shoot := fixEmptyGardenerShoot("test", "dev")

		// when
		err := extender(runtimeShoot, &shoot)

		// then
		require.NoError(t, err)
		assert.Equal(t, "myshoot.dev.mydomain.com", *shoot.Spec.DNS.Domain)
		assert.Equal(t, []string{"myshoot.dev.mydomain.com"}, shoot.Spec.DNS.Providers[0].Domains.Include)
		assert.Equal(t, dnsProviderType, *shoot.Spec.DNS.Providers[0].Type)
		assert.Equal(t, secretName, *shoot.Spec.DNS.Providers[0].SecretName)
		assert.Equal(t, true, *shoot.Spec.DNS.Providers[0].Primary)
	})
}

func fixEmptyGardenerShoot(name, namespace string) gardener.Shoot {
	return gardener.Shoot{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Spec: gardener.ShootSpec{},
	}
}
