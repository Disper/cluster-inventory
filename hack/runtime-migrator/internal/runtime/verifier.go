package runtime

import (
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1 "github.com/kyma-project/infrastructure-manager/api/v1"
	"github.com/kyma-project/infrastructure-manager/hack/shoot-comparator/pkg/shoot"
	"github.com/kyma-project/infrastructure-manager/pkg/config"
	gardener_shoot "github.com/kyma-project/infrastructure-manager/pkg/gardener/shoot"
	"github.com/kyma-project/infrastructure-manager/pkg/gardener/shoot/extender/auditlogs"
	"k8s.io/utils/ptr"
	"slices"
)

type Verifier struct {
	converterConfig config.ConverterConfig
	auditLogConfig  auditlogs.Configuration
}

func NewVerifier(converterConfig config.ConverterConfig, auditLogConfig auditlogs.Configuration) Verifier {
	return Verifier{
		converterConfig: converterConfig,
		auditLogConfig:  auditLogConfig,
	}
}

type ShootComparisonResult struct {
	RuntimeID      string
	OriginalShoot  v1beta1.Shoot
	ConvertedShoot v1beta1.Shoot
	Diff           *Difference
}

type Difference string

func (v Verifier) Do(runtimeToVerify v1.Runtime, shootToMatch v1beta1.Shoot) (ShootComparisonResult, error) {
	converter, err := v.newConverter(shootToMatch)
	if err != nil {
		return ShootComparisonResult{}, err
	}

	shootFromConverter, err := converter.ToShoot(runtimeToVerify)
	if err != nil {
		return ShootComparisonResult{}, err
	}

	diff, err := compare(shootToMatch, shootFromConverter)
	if err != nil {
		return ShootComparisonResult{}, err
	}

	return ShootComparisonResult{
		RuntimeID:      runtimeToVerify.Name,
		OriginalShoot:  shootToMatch,
		ConvertedShoot: shootFromConverter,
		Diff:           diff,
	}, nil
}

func (v Verifier) newConverter(shootToMatch v1beta1.Shoot) (gardener_shoot.Converter, error) {
	auditLogData, err := v.auditLogConfig.GetAuditLogData(
		shootToMatch.Spec.Provider.Type,
		shootToMatch.Spec.Region)

	if err != nil {
		return gardener_shoot.Converter{}, err
	}

	return gardener_shoot.NewConverterPatch(gardener_shoot.PatchOpts{
		ConverterConfig: v.converterConfig,
		AuditLogData:    auditLogData,
		Zones:           getZones(shootToMatch.Spec.Provider.Workers),
	}), nil
}

func getZones(workers []v1beta1.Worker) []string {
	var zones []string

	for _, worker := range workers {
		for _, zone := range worker.Zones {
			if !slices.Contains(zones, zone) {
				zones = append(zones, zone)
			}
		}
	}

	return zones
}

func compare(originalShoot, convertedShoot v1beta1.Shoot) (*Difference, error) {
	matcher := shoot.NewMatcher(originalShoot)
	equal, err := matcher.Match(convertedShoot)
	if err != nil {
		return nil, err
	}

	if !equal {
		diff := Difference(matcher.FailureMessage(nil))
		return ptr.To(diff), nil
	}

	return nil, nil
}

func (cr ShootComparisonResult) IsEqual() bool {
	return cr.Diff == nil
}
