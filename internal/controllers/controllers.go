package controllers

import (
	"context"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/matzegebbe/k8s-copycat/internal/mirror"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
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

func (r *baseReconciler) processPodSpec(ctx context.Context, ns, podName string, spec *corev1.PodSpec) (ctrl.Result, error) {
	if !r.nsAllowed(ns) {
		return ctrl.Result{}, nil
	}
	images := util.ImagesFromPodSpec(spec)
	if len(images) == 0 {
		return ctrl.Result{}, nil
	}
	for _, img := range images {
		meta := mirror.Metadata{
			Namespace:     ns,
			PodName:       podName,
			ContainerName: img.ContainerName,
		}
		if err := r.Pusher.Mirror(ctx, img.Image, meta); err != nil {
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
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw Deployment", "name", req.Name, "namespace", req.Namespace)
	var d appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &d); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, d.Namespace, d.Name, &d.Spec.Template.Spec)
}

func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type StatefulSetReconciler struct{ baseReconciler }

func (r *StatefulSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw StatefulSet", "name", req.Name, "namespace", req.Namespace)
	var s appsv1.StatefulSet
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &s); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, s.Namespace, s.Name, &s.Spec.Template.Spec)
}
func (r *StatefulSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type JobReconciler struct{ baseReconciler }

func (r *JobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw Job", "name", req.Name, "namespace", req.Namespace)
	var j batchv1.Job
	if err := r.Get(ctx, req.NamespacedName, &j); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, j.Namespace, j.Name, &j.Spec.Template.Spec)
}
func (r *JobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Job{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type CronJobReconciler struct{ baseReconciler }

func (r *CronJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw CronJob", "name", req.Name, "namespace", req.Namespace)
	var cj batchv1.CronJob
	if err := r.Get(ctx, req.NamespacedName, &cj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, cj.Namespace, cj.Name, &cj.Spec.JobTemplate.Spec.Template.Spec)
}
func (r *CronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.CronJob{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type PodReconciler struct{ baseReconciler }

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw Pod", "name", req.Name, "namespace", req.Namespace)
	var p corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if p.Status.Phase == corev1.PodPending || p.Status.Phase == corev1.PodRunning {
		return r.processPodSpec(ctx, p.Namespace, p.Name, &p.Spec)
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

func SetupAll(mgr ctrl.Manager, pusher mirror.Pusher, allowedNS []string) error {
	if err := (&DeploymentReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&StatefulSetReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&JobReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&CronJobReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&PodReconciler{baseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Pusher: pusher, AllowedNamespaces: allowedNS}}).SetupWithManager(mgr); err != nil {
		return err
	}
	return nil
}
