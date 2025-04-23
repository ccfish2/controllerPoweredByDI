package controllerruntime

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

func Success() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func Fail(err error) (ctrl.Result, error) {
	return ctrl.Result{}, err
}
