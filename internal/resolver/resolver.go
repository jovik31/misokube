package resolver

import (
	"context"
	"log/slog"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ResolverImpl is the concrete resolver implementation.
type ResolverImpl struct {
	cfg  Config
	log  *slog.Logger
	pods coreinformers.PodInformer

	snap       snap
	podsSynced atomic.Bool
}

func New(pods coreinformers.PodInformer, cfg Config) *ResolverImpl {
	cfg.SetDefaults()
	r := &ResolverImpl{
		cfg:  cfg,
		log:  cfg.Logger,
		pods: pods,
	}

	if r.log == nil {
		r.log = slog.Default()
	}
	r.snap.store(Snapshot{
		ByUID:  map[string]string{},
		ByName: map[string]string{},
		Synced: false,
	})
	return r
}

func (r *ResolverImpl) Start(ctx context.Context) error {
	// Pod handlers
	r.pods.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.onPodAdd,
		UpdateFunc: func(_, newObj any) { r.onPodAdd(newObj) },
		DeleteFunc: r.onPodDel,
	})

	// Initial sync in background (non-blocking Start).
	go func() {
		sctx := ctx
		if d := r.cfg.InitialSyncTimeout; d > 0 {
			var cancel context.CancelFunc
			sctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		okPods := cache.WaitForCacheSync(sctx.Done(), r.pods.Informer().HasSynced)

		r.podsSynced.Store(okPods)

		r.updateSynced()
		if okPods {
			r.log.Info("resolver informers synced")
		} else {
			r.log.Warn("resolver informers not fully synced", "pods", okPods)
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
	return r.snap.load().Synced
}

func (r *ResolverImpl) Resolve(ns, pod, uid string) (string, bool, error) {
	s := r.snap.load()
	if !s.Synced {
		return "", true, ErrNotReady
	}
	// UID-first (globally unique)
	if uid != "" {
		if tid, ok := s.ByUID[uid]; ok && tid != "" {
			return tid, false, nil
		}
	}
	// Fallback to ns/pod
	if tid, ok := s.ByName[PodKey(ns, pod)]; ok && tid != "" {
		return tid, false, nil
	}

	key := PodKey(ns, pod)
	obj, exists, err := r.pods.Informer().GetIndexer().GetByKey(key)
	if err != nil || !exists {
		return "", true, NotIndexedError{Namespace: ns, Pod: pod, UID: uid}
	}

	p, _ := obj.(*corev1.Pod)
	if tenantFromLabels(p.Labels, r.cfg.TenantLabelKey) == "" {
		return "", true, NoTenantLabelError{Namespace: ns, Pod: pod, UID: uid}
	}

	return "", true, NotIndexedError{Namespace: ns, Pod: pod, UID: uid}
}

// --- event handlers (copy-on-write updates) ---

func (r *ResolverImpl) onPodAdd(obj any) {
	p, _ := obj.(*corev1.Pod)
	if p == nil {
		return
	}
	if r.cfg.NodeName != "" && p.Spec.NodeName != r.cfg.NodeName {
		return
	}

	tid := tenantFromLabels(p.Labels, r.cfg.TenantLabelKey)
	uid := string(p.UID)
	key := PodKey(p.Namespace, p.Name)

	s := r.snap.load()
	byName := cloneMap(s.ByName)
	byUID := cloneMap(s.ByUID)

	if tid != "" {
		byName[key] = tid
		if uid != "" {
			byUID[uid] = tid
		}
	} else {
		delete(byName, key)
		if uid != "" {
			delete(byUID, uid)
		}
	}

	s.ByName, s.ByUID = byName, byUID
	s.Synced = r.podsSynced.Load()
	r.snap.store(s)
}

func (r *ResolverImpl) onPodDel(obj any) {
	p, _ := obj.(*corev1.Pod)
	if p == nil {
		return
	}
	if r.cfg.NodeName != "" && p.Spec.NodeName != r.cfg.NodeName {
		return
	}

	uid := string(p.UID)
	key := PodKey(p.Namespace, p.Name)

	s := r.snap.load()
	byName := cloneMap(s.ByName)
	byUID := cloneMap(s.ByUID)

	delete(byName, key)
	if uid != "" {
		delete(byUID, uid)
	}

	s.ByName, s.ByUID = byName, byUID
	s.Synced = r.podsSynced.Load()
	r.snap.store(s)
}

func (r *ResolverImpl) updateSynced() {
	s := r.snap.load()
	s.Synced = r.podsSynced.Load()
	r.snap.store(s)
}
