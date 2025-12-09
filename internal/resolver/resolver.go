package resolver

import (
	"context"
	"log"
	"log/slog"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var _ Resolver = (*ResolverImpl)(nil)

// ResolverImpl is the concrete resolver implementation.
type ResolverImpl struct {
	cfg        Config
	log        *slog.Logger
	podInf     coreinformers.PodInformer
	podsSynced atomic.Bool
}

func NewResolver(pods coreinformers.PodInformer, cfg Config) (*ResolverImpl, error) {
	cfg.SetDefaults()
	r := &ResolverImpl{
		cfg:    cfg,
		log:    cfg.Logger,
		podInf: pods,
	}

	if r.log == nil {
		r.log = slog.Default()
	}

	// Add an indexer by Pod UID for fast lookups.
	// Safe to call multiple times; will return an error if duplicate.
	err := r.podInf.Informer().AddIndexers(cache.Indexers{
		"byUID": func(obj any) ([]string, error) {
			p, _ := obj.(*corev1.Pod)
			if p == nil {
				return nil, nil
			}
			uid := string(p.UID)
			if uid == "" {
				return nil, nil
			}
			return []string{uid}, nil
		},
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

// NewAndStartWithClient creates a resolver, owns and starts a SharedInformerFactory
// using the provided client, and begins syncing in the background. Returns immediately.
func NewAndStartWithClient(ctx context.Context, client kubernetes.Interface, cfg Config) (*ResolverImpl, error) {
	// Build factory and pod informer
	factory := informers.NewSharedInformerFactory(client, 0)
	podInf := factory.Core().V1().Pods()

	// Construct resolver bound to this informer
	r, err := NewResolver(podInf, cfg)
	if err != nil {
		return nil, err

	}

	// Start factory and resolver syncing in background
	go factory.Start(ctx.Done())
	_ = r.StartAsync(ctx)
	return r, nil
}

// NewAndStartWithRestConfig creates a client from rest config and delegates to NewAndStartWithClient.
func NewAndStartWithRestConfig(ctx context.Context, restCfg *rest.Config, cfg Config) (*ResolverImpl, error) {
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	r, err := NewAndStartWithClient(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ResolverImpl) Start(ctx context.Context) error {
	// Block until informers are synced (no timeout). Respect context cancellation.
	okPods := cache.WaitForCacheSync(ctx.Done(), r.podInf.Informer().HasSynced)
	if !okPods {
		log.Print("[RESOLVER] failed to wait for informer sync")
	}
	r.podsSynced.Store(okPods)
	if okPods {
		r.log.Info("resolver informers synced")
		return nil
	}
	// Context canceled before sync completed.
	r.log.Warn("resolver informers stopped before syncing", "pods", okPods)
	return context.Canceled
}

// StartAsync registers handlers and waits for informer sync in a separate goroutine.
// It assumes the underlying SharedInformerFactory has already been started elsewhere.
// Returns immediately; readiness flips once caches are synced.
func (r *ResolverImpl) StartAsync(ctx context.Context) error {
	go func() {
		okPods := cache.WaitForCacheSync(ctx.Done(), r.podInf.Informer().HasSynced)
		if !okPods {
			log.Print("[RESOLVER] async: failed to wait for informer sync")
		}
		r.podsSynced.Store(okPods)
		if okPods {
			r.log.Info("resolver informers synced (async)")
		} else {
			r.log.Warn("resolver informers stopped before syncing (async)", "pods", okPods)
		}
	}()

	return nil
}

func (r *ResolverImpl) Shutdown(ctx context.Context) error {
	// Informers typically owned by a shared factory; nothing to stop here.
	_ = ctx
	return nil
}

func (r *ResolverImpl) Ready() bool {
	return r.podsSynced.Load()
}

func (r *ResolverImpl) Resolve(puid string) (string, bool, error) {
	if !r.podsSynced.Load() {
		return "", true, ErrNotReady
	}
	// Fallback: use informer indexer by UID
	idx := r.podInf.Informer().GetIndexer()
	pods, err := idx.ByIndex("byUID", puid)
	if err != nil || len(pods) == 0 {
		return "", true, ErrNotReady
	}
	p, _ := pods[0].(*corev1.Pod)
	if p == nil {
		return "", true, ErrNotReady
	}
	// Resolve tenant from annotations; default if missing
	tenantID := p.GetAnnotations()[r.cfg.TenantLabelKey]
	if tenantID == "" {
		tenantID = r.cfg.DefaultTenant
	}
	return tenantID, false, nil
}
