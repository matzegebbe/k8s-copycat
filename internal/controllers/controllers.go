package controllers

import (
	"context"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/matzegebbe/doppler/internal/mirror"
	"github.com/matzegebbe/doppler/pkg/util"
)

const (
	maxConcurrent = 2
	retryDelay    = 30 * time.Second
)

type baseReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Pusher            mirror.Pusher
	AllowedNamespaces []string // "*" or explicit list
}

func (r *baseReconciler) nsAllowed(ns string) bool {
	if len(r.AllowedNamespaces) == 0 {
		return true
	}
	if len(r.AllowedNamespaces) == 1 && strings.TrimSpace(r.AllowedNamespaces[0]) == "*" {
		return true
	}
	for _, n := range r.AllowedNamespaces {
		if strings.TrimSpace(n) == ns {
			return true
		}
	}
	return false
}

func (r *baseReconciler) processPodSpec(ctx context.Context, ns string, spec *corev1.PodSpec) (ctrl.Result, error) {
	if !r.nsAllowed(ns) {
		return ctrl.Result{}, nil
	}
	images := util.ImagesFromPodSpec(spec)
	if len(images) == 0 {
		return ctrl.Result{}, nil
	}
	for _, img := range images {
		if err := r.Pusher.Mirror(ctx, img); err != nil {
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
	}
	return ctrl.Result{}, nil
}

type DeploymentReconciler struct{ baseReconciler }

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	var d appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &d); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, d.Namespace, &d.Spec.Template.Spec)
}
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type PodReconciler struct{ baseReconciler }

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	var p corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if p.Status.Phase == corev1.PodPending || p.Status.Phase == corev1.PodRunning {
		return r.processPodSpec(ctx, p.Namespace, &p.Spec)
	}
	return ctrl.Result{}, nil
}
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.ResourceVersionChangedPredicate{}).
		Complete(r)
}

// SetupAll wires the two controllers.
func SetupAll(mgr ctrl.Manager, pusher mirror.Pusher, allowedNS []string) error {
	if err := (&DeploymentReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&PodReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	return nil
}
