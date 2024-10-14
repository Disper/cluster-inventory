package fsm

import (
	"context"
	"fmt"
	"strconv"

	gardener "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	imv1 "github.com/kyma-project/infrastructure-manager/api/v1"
	"github.com/kyma-project/infrastructure-manager/internal/gardener/shoot/extender"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ErrReason string

func sFnSelectShootProcessing(_ context.Context, m *fsm, s *systemState) (stateFn, *ctrl.Result, error) {
	m.log.Info("Select shoot processing state")

	if s.shoot.Spec.DNS == nil || s.shoot.Spec.DNS.Domain == nil {
		msg := fmt.Sprintf("DNS Domain is not set yet for shoot: %s, scheduling for retry", s.shoot.Name)
		m.log.Info(msg)
		return updateStatusAndRequeueAfter(gardenerRequeueDuration)
	}

	lastOperation := s.shoot.Status.LastOperation
	if lastOperation == nil {
		msg := fmt.Sprintf("Last operation is nil for shoot: %s, scheduling for retry", s.shoot.Name)
		m.log.Info(msg)
		return updateStatusAndRequeueAfter(gardenerRequeueDuration)
	}

	patchShoot, err := shouldPatchShoot(s.instance, *s.shoot)
	if err != nil {
		msg := fmt.Sprintf("Failed to get applied generation for shoot: %s, scheduling for retry", s.shoot.Name)
		m.log.Error(err, msg)
		return updateStatusAndStop()
	}

	if patchShoot {
		// only allow to patch if full previous cycle was completed
		m.log.Info("Gardener shoot already exists, updating")
		return switchState(sFnPatchExistingShoot)
	}

	if lastOperation.Type == gardener.LastOperationTypeCreate {
		return switchState(sFnWaitForShootCreation)
	}

	if lastOperation.Type == gardener.LastOperationTypeReconcile {
		return switchState(sFnWaitForShootReconcile)
	}

	m.log.Info("Unknown shoot operation type, exiting with no retry")
	return stop()
}

func shouldPatchShoot(runtime imv1.Runtime, shoot gardener.Shoot) (bool, error) {
	runtimeGeneration := runtime.GetGeneration()
	appliedGenerationString, found := shoot.GetAnnotations()[extender.ShootRuntimeGenerationAnnotation]

	if !found {
		return true, nil
	}

	appliedGeneration, err := strconv.ParseInt(appliedGenerationString, 10, 64)
	if err != nil {
		return false, err
	}

	return appliedGeneration < runtimeGeneration, nil
}
