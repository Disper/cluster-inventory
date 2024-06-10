package controller

import (
	"context"
	gardener "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	imv1 "github.com/kyma-project/infrastructure-manager/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func sFnInitialize(ctx context.Context, m *fsm, s *systemState) (stateFn, *ctrl.Result, error) {
	instanceIsBeingDeleted := !s.instance.GetDeletionTimestamp().IsZero()
	instanceHasFinalizer := controllerutil.ContainsFinalizer(&s.instance, m.Finalizer)

	// in case instance does not have finalizer - add it and update instance
	if !instanceIsBeingDeleted && !instanceHasFinalizer {
		m.log.Info("adding finalizer")
		controllerutil.AddFinalizer(&s.instance, m.Finalizer)

		err := m.Update(ctx, &s.instance)
		if err != nil {
			return stopWithErrorAndRequeue(err)
		}

		s.instance.UpdateStateProcessing(
			imv1.ConditionTypeRuntimeProvisioning,
			imv1.ConditionReasonInitialized,
			"initialized",
		)
		return stopWithRequeue()
	}
	// in case instance has no finalizer and instance is being deleted - end reconciliation
	if instanceIsBeingDeleted && !controllerutil.ContainsFinalizer(&s.instance, m.Finalizer) {
		m.log.Info("Instance is being deleted")
		// stop state machine
		return nil, nil, nil
	}

	// in case instance is being deleted and does not have finalizer - delete shoot
	if instanceIsBeingDeleted {
		return switchState(sFnDeleteShoot)
	}

	shoot, err := m.shootClient.Get(ctx, s.instance.Name, v1.GetOptions{})

	if err != nil {
		if apierrors.IsNotFound(err) {
			m.log.Info("Gardener shoot does not exist, creating new one")
			return switchState(sFnCreateShoot)
		}

		m.log.Info("Failed to get shoot", "error", err)
		return stopWithRequeue()
	}

	if isShootReady(shoot) {
		return switchState(sFnProcessShoot)
	} else {
		// wait for shoot creation
		return switchState(sFnPrepareCluster)
	}
}

//func isShootReady(s *systemState) bool {
//	condition := meta.FindStatusCondition(s.instance.Status.Conditions, string(imv1.ConditionTypeRuntimeProvisioning))
//	if condition == nil {
//		return false
//	}
//
//	// or check for shoot creation completed
//	if condition.Reason != string(imv1.ConditionReasonShootCreationCompleted) {
//		return false
//	}
//
//	return true
//}

func isShootReady(s *gardener.Shoot) bool {
	return true
}
