package shoot

import (
	"fmt"
	"strings"

	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/kyma-project/infrastructure-manager/hack/shoot-comparator/pkg/runtime"
	"github.com/kyma-project/infrastructure-manager/hack/shoot-comparator/pkg/utilz"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

func NewProviderMatcher(v any, path string) types.GomegaMatcher {
	return &ProviderMatcher{
		toMatch:  v,
		rootPath: path,
	}
}

type ProviderMatcher struct {
	toMatch  interface{}
	fails    []string
	rootPath string
}

func (m *ProviderMatcher) getPath(p string) string {
	return fmt.Sprintf("%s/%s", m.rootPath, p)
}

func (m *ProviderMatcher) Match(actual interface{}) (success bool, err error) {
	aProvider, err := utilz.Get[v1beta1.Provider](actual)
	if err != nil {
		return false, err
	}

	eProvider, err := utilz.Get[v1beta1.Provider](m.toMatch)
	if err != nil {
		return false, err
	}

	for _, matcher := range []propertyMatcher{
		{
			path:          m.getPath("type"),
			GomegaMatcher: gomega.BeComparableTo(eProvider.Type),
			actual:      aProvider.Type,
		},
		{
			path:          m.getPath("workers"),
			GomegaMatcher: gstruct.MatchElements(idWorker, gstruct.IgnoreExtras, workers(aProvider.Workers)),
			actual:      eProvider.Workers,
		},
		{
			path:          m.getPath("controlPlaneConfig"),
			GomegaMatcher: runtime.NewRawExtensionMatcher(eProvider.ControlPlaneConfig),
			actual:      aProvider.ControlPlaneConfig,
		},
		{
			path:          m.getPath("infrastructureConfig"),
			GomegaMatcher: runtime.NewRawExtensionMatcher(eProvider.InfrastructureConfig),
			actual:      aProvider.InfrastructureConfig,
		},
		{
			path:          m.getPath("workerSettings"),
			GomegaMatcher: newWorkerSettingsMatcher(eProvider.WorkersSettings),
			actual:      aProvider.WorkersSettings,
		},
	} {
		ok, err := matcher.Match(matcher.actual)
		if err != nil {
			return false, err
		}

		if !ok {
			msg := matcher.FailureMessage(matcher.actual)
			if matcher.path != "" {
				msg = fmt.Sprintf("%s: %s", matcher.path, msg)
			}
			m.fails = append(m.fails, msg)
		}
	}

	return len(m.fails) == 0, nil
}

func (m *ProviderMatcher) NegatedFailureMessage(_ interface{}) string {
	return "expected should not equal actual"
}

func (m *ProviderMatcher) FailureMessage(_ interface{}) string {
	return strings.Join(m.fails, "\n")
}

type propertyMatcher = struct {
	types.GomegaMatcher
	path     string
	actual interface{}
}

func idWorker(v interface{}) string {
	if v == nil {
		panic("nil value")
	}

	w, ok := v.(v1beta1.Worker)
	if !ok {
		panic("invalid type")
	}

	return w.Name
}

func workers(ws []v1beta1.Worker) gstruct.Elements {
	out := map[string]types.GomegaMatcher{}
	for _, w := range ws {
		ID := idWorker(w)
		out[ID] = gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Annotations": gomega.SatisfyAll(mapMatchers(w.Annotations)...),
			"CABundle":    gomega.BeComparableTo(w.CABundle),
			"CRI":         newCRIMatcher(w.CRI),
			"Kubernetes":  gstruct.Ignore(),
			"Labels":      gomega.SatisfyAll(mapMatchers(w.Labels)...),
			"Name":        gomega.BeComparableTo(w.Name),
			"Machine": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Type":  gomega.BeComparableTo(w.Machine.Type),
				"Image": newShootMachineImageMatcher(w.Machine.Image),
			}),
			"Maximum":                          gomega.BeComparableTo(w.Maximum),
			"Minimum":                          gomega.BeComparableTo(w.Minimum),
			"MaxSurge":                         gomega.BeComparableTo(w.MaxSurge),
			"MaxUnavailable":                   gomega.BeComparableTo(w.MaxUnavailable),
			"ProviderConfig":                   runtime.NewRawExtensionMatcher(w.ProviderConfig),
			"Taints":                           gstruct.Ignore(),
			"Volume":                           gomega.BeComparableTo(w.Volume),
			"DataVolumes":                      gstruct.Ignore(),
			"KubeletDataVolumeName":            gomega.BeComparableTo(w.KubeletDataVolumeName),
			"Zones":                            gomega.ContainElements(w.Zones),
			"SystemComponents":                 gstruct.Ignore(),
			"MachineControllerManagerSettings": gstruct.Ignore(),
			"Sysctls":                          gomega.SatisfyAll(mapMatchers(w.Sysctls)...),
			"ClusterAutoscaler":                gstruct.Ignore(),
		})
	}
	return out
}

func newShootMachineImageMatcher(i *v1beta1.ShootMachineImage) types.GomegaMatcher {
	if i == nil {
		return gomega.BeNil()
	}

	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Name":    gomega.BeComparableTo(i.Name),
		"Version": gomega.BeComparableTo(i.Version),
	}))
}

func newWorkerSettingsMatcher(s *v1beta1.WorkersSettings) types.GomegaMatcher {
	if s == nil || s.SSHAccess == nil {
		return gomega.BeNil()
	}

	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"SSHAccess": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Enabled": gomega.BeComparableTo(s.SSHAccess.Enabled),
		})),
	}))
}

func idContainerRuntime(v interface{}) string {
	if v == nil {
		panic("nil value")
	}

	r, ok := v.(v1beta1.ContainerRuntime)
	if !ok {
		panic("invalid type")
	}

	return r.Type
}

func newCRIMatcher(cri *v1beta1.CRI) types.GomegaMatcher {
	if cri == nil {
		return gomega.BeNil()
	}

	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Name": gomega.BeComparableTo(cri.Name),
		"ContainerRuntimes": gstruct.MatchElements(idContainerRuntime, gstruct.IgnoreExtras,
			containerRuntimes(cri.ContainerRuntimes)),
	})
}

func containerRuntimes(rs []v1beta1.ContainerRuntime) gstruct.Elements {
	rsLen := len(rs)
	out := make(gstruct.Elements, rsLen)
	for _, crt := range rs {
		ID := idContainerRuntime(crt)
		out[ID] = gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Type":           gomega.BeComparableTo(crt.Type),
			"ProviderConfig": gstruct.PointTo(runtime.NewRawExtensionMatcher(crt.ProviderConfig)),
		})
	}
	return out
}
