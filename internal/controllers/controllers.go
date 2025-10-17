package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	defaultRetryDelay = 24 * time.Hour
)

// SkipConfig declares which namespaces or object names should be ignored by the controllers.
type SkipConfig struct {
	Namespaces   []string
	Deployments  []string
	StatefulSets []string
	DaemonSets   []string
	Jobs         []string
	CronJobs     []string
	Pods         []string
}

// ResourceType describes a Kubernetes resource that can be watched by copycat.
type ResourceType string

const (
	ResourceDeployments  ResourceType = "deployments"
	ResourceStatefulSets ResourceType = "statefulsets"
	ResourceDaemonSets   ResourceType = "daemonsets"
	ResourceJobs         ResourceType = "jobs"
	ResourceCronJobs     ResourceType = "cronjobs"
	ResourcePods         ResourceType = "pods"
)

var supportedResourceTypes = map[string]ResourceType{
	"deployments":  ResourceDeployments,
	"statefulsets": ResourceStatefulSets,
	"daemonsets":   ResourceDaemonSets,
	"jobs":         ResourceJobs,
	"cronjobs":     ResourceCronJobs,
	"pods":         ResourcePods,
}

// AllResourceTypes returns every supported resource type in deterministic order.
func AllResourceTypes() []ResourceType {
	return []ResourceType{
		ResourceDeployments,
		ResourceStatefulSets,
		ResourceDaemonSets,
		ResourceJobs,
		ResourceCronJobs,
		ResourcePods,
	}
}

// ParseWatchResources converts raw resource strings to ResourceType values while reporting unsupported entries.
func ParseWatchResources(values []string) ([]ResourceType, []string) {
	seen := make(map[ResourceType]struct{}, len(values))
	parsed := make([]ResourceType, 0, len(values))
	invalid := make([]string, 0)
	for _, raw := range values {
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		if trimmed == "" {
			continue
		}
		typ, ok := supportedResourceTypes[trimmed]
		if !ok {
			invalid = append(invalid, raw)
			continue
		}
		if _, dup := seen[typ]; dup {
			continue
		}
		seen[typ] = struct{}{}
		parsed = append(parsed, typ)
	}
	return parsed, invalid
}

type nameMatcher struct {
	matchAll   bool
	any        map[string]struct{}
	namespaced map[string]map[string]struct{}
}

func newNameMatcher(values []string) nameMatcher {
	m := nameMatcher{}
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if name == "*" {
			m.matchAll = true
			continue
		}
		if strings.Contains(name, "/") {
			parts := strings.SplitN(name, "/", 2)
			ns := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if ns == "" || val == "" {
				continue
			}
			if m.namespaced == nil {
				m.namespaced = make(map[string]map[string]struct{})
			}
			nsSet := m.namespaced[ns]
			if nsSet == nil {
				nsSet = make(map[string]struct{})
				m.namespaced[ns] = nsSet
			}
			nsSet[val] = struct{}{}
			continue
		}
		if m.any == nil {
			m.any = make(map[string]struct{})
		}
		m.any[name] = struct{}{}
	}
	return m
}

func (m nameMatcher) matches(namespace, name string) bool {
	if m.matchAll {
		return true
	}
	if len(m.any) > 0 {
		if _, ok := m.any[name]; ok {
			return true
		}
	}
	if len(m.namespaced) > 0 {
		if nsSet, ok := m.namespaced[namespace]; ok {
			if _, ok := nsSet[name]; ok {
				return true
			}
		}
	}
	return false
}

type baseReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Pusher            mirror.Pusher
	AllowedNamespaces []string // "*" or explicit list
	SkippedNamespaces map[string]struct{}
	SkipDeployments   nameMatcher
	SkipStatefulSets  nameMatcher
	SkipDaemonSets    nameMatcher
	SkipJobs          nameMatcher
	SkipCronJobs      nameMatcher
	SkipPods          nameMatcher
}

type ForceReconciler struct {
	baseReconciler
	watch []ResourceType
}

func (r *ForceReconciler) ForceReconcile(ctx context.Context) (int, int, error) {
	watch := r.watch
	if len(watch) == 0 {
		watch = AllResourceTypes()
	}
	workloads := 0
	images := 0
	var errs []error
	for _, res := range watch {
		switch res {
		case ResourceDeployments:
			var list appsv1.DeploymentList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				d := &list.Items[i]
				if !r.nsAllowed(d.Namespace) || r.SkipDeployments.matches(d.Namespace, d.Name) {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, d.Namespace, d.Name, &d.Spec.Template.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("deployment %s/%s: %w", d.Namespace, d.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		case ResourceStatefulSets:
			var list appsv1.StatefulSetList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				s := &list.Items[i]
				if !r.nsAllowed(s.Namespace) || r.SkipStatefulSets.matches(s.Namespace, s.Name) {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, s.Namespace, s.Name, &s.Spec.Template.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("statefulset %s/%s: %w", s.Namespace, s.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		case ResourceDaemonSets:
			var list appsv1.DaemonSetList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				ds := &list.Items[i]
				if !r.nsAllowed(ds.Namespace) || r.SkipDaemonSets.matches(ds.Namespace, ds.Name) {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, ds.Namespace, ds.Name, &ds.Spec.Template.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("daemonset %s/%s: %w", ds.Namespace, ds.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		case ResourceJobs:
			var list batchv1.JobList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				j := &list.Items[i]
				if !r.nsAllowed(j.Namespace) || r.SkipJobs.matches(j.Namespace, j.Name) {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, j.Namespace, j.Name, &j.Spec.Template.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("job %s/%s: %w", j.Namespace, j.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		case ResourceCronJobs:
			var list batchv1.CronJobList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				cj := &list.Items[i]
				if !r.nsAllowed(cj.Namespace) || r.SkipCronJobs.matches(cj.Namespace, cj.Name) {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, cj.Namespace, cj.Name, &cj.Spec.JobTemplate.Spec.Template.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("cronjob %s/%s: %w", cj.Namespace, cj.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		case ResourcePods:
			var list corev1.PodList
			if err := r.Client.List(ctx, &list); err != nil {
				return workloads, images, err
			}
			for i := range list.Items {
				p := &list.Items[i]
				if !r.nsAllowed(p.Namespace) {
					continue
				}
				skip, err := r.shouldSkipPod(ctx, p)
				if err != nil {
					return workloads, images, err
				}
				if skip {
					continue
				}
				if p.Status.Phase != corev1.PodPending && p.Status.Phase != corev1.PodRunning {
					continue
				}
				mirrored, err := r.mirrorPodSpec(ctx, p.Namespace, p.Name, &p.Spec)
				images += mirrored
				if err != nil {
					errs = append(errs, fmt.Errorf("pod %s/%s: %w", p.Namespace, p.Name, err))
					workloads++
					continue
				}
				workloads++
			}
		}
	}
	if len(errs) > 0 {
		return workloads, images, errors.Join(errs...)
	}
	return workloads, images, nil
}

func (r *baseReconciler) nsAllowed(ns string) bool {
	if r.namespaceSkipped(ns) {
		return false
	}
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

func (r *baseReconciler) namespaceSkipped(ns string) bool {
	if len(r.SkippedNamespaces) == 0 {
		return false
	}
	_, ok := r.SkippedNamespaces[ns]
	return ok
}

func (r *baseReconciler) mirrorPodSpec(ctx context.Context, ns, podName string, spec *corev1.PodSpec) (int, error) {
	if !r.nsAllowed(ns) {
		return 0, nil
	}
	images := util.ImagesFromPodSpec(spec)
	if len(images) == 0 {
		return 0, nil
	}
	mirrored := 0
	for _, img := range images {
		meta := mirror.Metadata{
			Namespace:     ns,
			PodName:       podName,
			ContainerName: img.ContainerName,
		}
		if err := r.Pusher.Mirror(ctx, img.Image, meta); err != nil {
			return mirrored, err
		}
		mirrored++
	}
	return mirrored, nil
}

func (r *baseReconciler) processPodSpec(ctx context.Context, ns, podName string, spec *corev1.PodSpec) (ctrl.Result, error) {
	if !r.nsAllowed(ns) {
		return ctrl.Result{}, nil
	}
	_, err := r.mirrorPodSpec(ctx, ns, podName, spec)
	if err == nil {
		return ctrl.Result{}, nil
	}
	var retryErr *mirror.RetryError
	if errors.As(err, &retryErr) {
		delay := time.Until(retryErr.RetryAt)
		if delay <= 0 {
			delay = defaultRetryDelay
		}
		return ctrl.Result{RequeueAfter: delay}, nil
	}
	return ctrl.Result{RequeueAfter: defaultRetryDelay}, nil
}

type DeploymentReconciler struct{ baseReconciler }

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	if r.SkipDeployments.matches(req.Namespace, req.Name) {
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

func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
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
	if r.SkipStatefulSets.matches(req.Namespace, req.Name) {
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
func (r *StatefulSetReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type DaemonSetReconciler struct{ baseReconciler }

func (r *DaemonSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	if r.SkipDaemonSets.matches(req.Namespace, req.Name) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw DaemonSet", "name", req.Name, "namespace", req.Namespace)
	var ds appsv1.DaemonSet
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, &ds); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return r.processPodSpec(ctx, ds.Namespace, ds.Name, &ds.Spec.Template.Spec)
}

func (r *DaemonSetReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.DaemonSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

type JobReconciler struct{ baseReconciler }

func (r *JobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.nsAllowed(req.Namespace) {
		return ctrl.Result{}, nil
	}
	if r.SkipJobs.matches(req.Namespace, req.Name) {
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
func (r *JobReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
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
	if r.SkipCronJobs.matches(req.Namespace, req.Name) {
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
func (r *CronJobReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
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
	if r.SkipPods.matches(req.Namespace, req.Name) {
		return ctrl.Result{}, nil
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("saw Pod", "name", req.Name, "namespace", req.Namespace)
	var p corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if skip, err := r.shouldSkipPod(ctx, &p); err != nil {
		return ctrl.Result{}, err
	} else if skip {
		return ctrl.Result{}, nil
	}
	if p.Status.Phase == corev1.PodPending || p.Status.Phase == corev1.PodRunning {
		return r.processPodSpec(ctx, p.Namespace, p.Name, &p.Spec)
	}
	return ctrl.Result{}, nil
}
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrent int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		WithEventFilter(predicate.ResourceVersionChangedPredicate{}).
		Complete(r)
}

func (r *baseReconciler) shouldSkipPod(ctx context.Context, pod *corev1.Pod) (bool, error) {
	if r.SkipPods.matches(pod.Namespace, pod.Name) {
		return true, nil
	}
	for _, owner := range pod.OwnerReferences {
		switch owner.Kind {
		case "ReplicaSet":
			skip, err := r.shouldSkipReplicaSetOwner(ctx, pod.Namespace, owner.Name)
			if err != nil {
				return false, err
			}
			if skip {
				return true, nil
			}
		case "Deployment":
			if r.SkipDeployments.matches(pod.Namespace, owner.Name) {
				return true, nil
			}
		case "StatefulSet":
			if r.SkipStatefulSets.matches(pod.Namespace, owner.Name) {
				return true, nil
			}
		case "DaemonSet":
			if r.SkipDaemonSets.matches(pod.Namespace, owner.Name) {
				return true, nil
			}
		case "Job":
			skip, err := r.shouldSkipJobOwner(ctx, pod.Namespace, owner.Name)
			if err != nil {
				return false, err
			}
			if skip {
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *baseReconciler) shouldSkipReplicaSetOwner(ctx context.Context, namespace, name string) (bool, error) {
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &rs); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, owner := range rs.OwnerReferences {
		if owner.Kind == "Deployment" && r.SkipDeployments.matches(namespace, owner.Name) {
			return true, nil
		}
	}
	return false, nil
}

func (r *baseReconciler) shouldSkipJobOwner(ctx context.Context, namespace, name string) (bool, error) {
	if r.SkipJobs.matches(namespace, name) {
		return true, nil
	}
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, owner := range job.OwnerReferences {
		if owner.Kind == "CronJob" && r.SkipCronJobs.matches(namespace, owner.Name) {
			return true, nil
		}
	}
	return false, nil
}

func SetupAll(mgr ctrl.Manager, pusher mirror.Pusher, allowedNS []string, skipCfg SkipConfig, watch []ResourceType, maxConcurrent int) (*ForceReconciler, error) {
	base := baseReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Pusher:            pusher,
		AllowedNamespaces: allowedNS,
		SkippedNamespaces: make(map[string]struct{}, len(skipCfg.Namespaces)),
		SkipDeployments:   newNameMatcher(skipCfg.Deployments),
		SkipStatefulSets:  newNameMatcher(skipCfg.StatefulSets),
		SkipDaemonSets:    newNameMatcher(skipCfg.DaemonSets),
		SkipJobs:          newNameMatcher(skipCfg.Jobs),
		SkipCronJobs:      newNameMatcher(skipCfg.CronJobs),
		SkipPods:          newNameMatcher(skipCfg.Pods),
	}
	for _, ns := range skipCfg.Namespaces {
		if trimmed := strings.TrimSpace(ns); trimmed != "" {
			base.SkippedNamespaces[trimmed] = struct{}{}
		}
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if len(watch) == 0 {
		watch = AllResourceTypes()
	}
	force := &ForceReconciler{baseReconciler: base, watch: append([]ResourceType(nil), watch...)}
	logger := ctrl.Log.WithName("controllers")
	for _, res := range watch {
		switch res {
		case ResourceDeployments:
			if err := (&DeploymentReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		case ResourceStatefulSets:
			if err := (&StatefulSetReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		case ResourceDaemonSets:
			if err := (&DaemonSetReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		case ResourceJobs:
			if err := (&JobReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		case ResourceCronJobs:
			if err := (&CronJobReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		case ResourcePods:
			if err := (&PodReconciler{base}).SetupWithManager(mgr, maxConcurrent); err != nil {
				return nil, err
			}
		default:
			logger.Error(fmt.Errorf("unsupported resource type"), "ignoring watch resource", "resource", res)
		}
	}
	return force, nil
}
