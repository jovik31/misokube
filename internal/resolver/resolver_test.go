package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	informers "k8s.io/client-go/informers"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

const tenantLabel = "setera.io/tenant"

// -------------------- helpers --------------------

func waitTrue(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	dead := time.Now().Add(timeout)
	for time.Now().Before(dead) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func newDeps() (*kfake.Clientset, informers.SharedInformerFactory) {
	client := kfake.NewSimpleClientset()
	f := informers.NewSharedInformerFactory(client, 0)
	return client, f
}

// -------------------- SIMPLE / BASELINE --------------------

// 1) Pod informer sync + Ready gate (no namespace defaults in this resolver)
func TestResolver_InformersSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, factory := newDeps()
	_ = client
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{
		TenantLabelKey: tenantLabel,
	})
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		t.Fatal("pod informer failed to sync")
	}
	waitTrue(t, time.Second, func() bool { return r.Ready() })
}

// 2) Snapshot-style behavior: Pod add/delete reflected in Resolve
func TestResolver_Snapshot_PodAddDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		t.Fatal("pod informer failed to sync")
	}
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	// Create a labeled pod
	_, err := client.CoreV1().Pods("team-a").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frontend",
			Namespace: "team-a",
			UID:       "uid-123",
			Labels:    map[string]string{tenantLabel: "tenant-X"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pod: %v", err)
	}

	// Eventually resolves by UID and by name
	waitTrue(t, 2*time.Second, func() bool {
		tenant, retry, err := r.Resolve("team-a", "frontend", "uid-123")
		return err == nil && !retry && tenant == "tenant-X"
	})
	tenant, retry, err := r.Resolve("team-a", "frontend", "")
	if err != nil || retry || tenant != "tenant-X" {
		t.Fatalf("resolve by name failed: tenant=%q retry=%v err=%v", tenant, retry, err)
	}

	// Delete pod -> should become a retryable miss
	if err := client.CoreV1().Pods("team-a").Delete(ctx, "frontend", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete pod: %v", err)
	}
	waitTrue(t, 2*time.Second, func() bool {
		_, retry, err := r.Resolve("team-a", "frontend", "uid-123")
		return retry && err != nil
	})
}

// -------------------- ERROR CLASSIFICATION --------------------

// 3) Not ready -> ErrNotReady
func TestResolver_NotReady_ReturnsErrNotReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	// Start but do NOT start informers -> not synced
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	_, retry, err := r.Resolve("ns", "pod", "uid-1")
	if !retry || !errors.Is(err, ErrNotReady) {
		t.Fatalf("expected ErrNotReady (retryable), got retry=%v err=%v", retry, err)
	}
}

// 4) Not indexed -> ErrNotIndexed (pod not in store)
func TestResolver_ErrNotIndexed_WhenPodUnknown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)

	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		t.Fatal("pod informer failed to sync")
	}
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	_, retry, err := r.Resolve("ns1", "p-1", "uid-1")
	if !retry || !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("expected ErrNotIndexed, got retry=%v err=%v", retry, err)
	}
}

// 5) No tenant label -> ErrNoTenantLabel (pod in cache but unlabeled)
func TestResolver_ErrNoTenantLabel_WhenPodSeenButNoLabel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)

	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	// Create pod without the tenant label
	_, err := client.CoreV1().Pods("ns1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nolabel",
			Namespace: "ns1",
			UID:       "uid-nolabel",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Eventually classify as NoTenantLabel (once watch delivered and store sees it)
	waitTrue(t, 2*time.Second, func() bool {
		_, retry, err := r.Resolve("ns1", "nolabel", "uid-nolabel")
		return retry && errors.Is(err, ErrNoTenantLabel)
	})
}

// 6) Race: create AFTER sync -> transient ErrNotIndexed allowed, then success
func TestResolver_Race_CreateAfterSync_CanReturnTransientNotIndexed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)
	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	// Create labeled pod AFTER sync (arrives via watch)
	_, err := client.CoreV1().Pods("ns1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p-080",
			Namespace: "ns1",
			UID:       "uid-080",
			Labels:    map[string]string{tenantLabel: "tenant-Z"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Allow a short window where ErrNotIndexed is acceptable, then expect success
	var ok bool
	dead := time.Now().Add(2 * time.Second)
	for time.Now().Before(dead) {
		tenant, retry, err := r.Resolve("ns1", "p-080", "uid-080")
		if err == nil && !retry && tenant == "tenant-Z" {
			ok = true
			break
		}
		if !errors.Is(err, ErrNotIndexed) {
			t.Fatalf("unexpected error during warm-up: retry=%v err=%v", retry, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("never resolved successfully within warm-up window")
	}
}

// -------------------- HAPPY PATH (FUNCTIONAL) --------------------

// 7) Resolve by UID (preseed before starting informers)
func TestResolve_ByUID_Preseed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	// Preseed: included in initial LIST
	_, err := client.CoreV1().Pods("team-a").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frontend",
			Namespace: "team-a",
			UID:       types.UID("uid-123"),
			Labels:    map[string]string{tenantLabel: "tenant-X"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("preseed pod: %v", err)
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		t.Fatal("pod informer failed to sync")
	}
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	tenant, retry, err := r.Resolve("team-a", "frontend", "uid-123")
	if err != nil || retry || tenant != "tenant-X" {
		t.Fatalf("resolve by UID failed: tenant=%q retry=%v err=%v", tenant, retry, err)
	}
}

// 8) Resolve by name (no UID), preseeded
func TestResolve_ByName_Preseed_NoUID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	_, err := client.CoreV1().Pods("ns1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "ns1",
			UID:       types.UID("uid-api"),
			Labels:    map[string]string{tenantLabel: "tenant-Z"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("preseed pod: %v", err)
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)
	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	tenant, retry, err := r.Resolve("ns1", "api", "")
	if err != nil || retry || tenant != "tenant-Z" {
		t.Fatalf("resolve by name failed: tenant=%q retry=%v err=%v", tenant, retry, err)
	}
}

// 9) Label appears (update) → resolves after update
func TestResolve_LabelAdded_ThenResolves(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	// Preseed pod without label
	seed, err := client.CoreV1().Pods("ns2").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker",
			Namespace: "ns2",
			UID:       types.UID("uid-worker"),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("preseed pod: %v", err)
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)
	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	// Initially: expect retryable (either NotIndexed until event processed, then NoTenantLabel)
	waitTrue(t, time.Second, func() bool {
		_, retry, err := r.Resolve("ns2", "worker", "uid-worker")
		return retry && err != nil
	})

	// Update: add label
	seed.Labels = map[string]string{tenantLabel: "tenant-W"}
	if _, err := client.CoreV1().Pods("ns2").Update(ctx, seed, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update pod label: %v", err)
	}

	// Eventually resolves
	waitTrue(t, 3*time.Second, func() bool {
		tenant, retry, err := r.Resolve("ns2", "worker", "uid-worker")
		return err == nil && !retry && tenant == "tenant-W"
	})
}

// 10) Node scoping — only pods on configured node are considered
func TestResolve_NodeScoped_IgnoresOtherNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	_, _ = client.CoreV1().Pods("nsA").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "nsA",
			UID:       types.UID("uid-A"),
			Labels:    map[string]string{tenantLabel: "tenant-A"},
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
	}, metav1.CreateOptions{})

	_, _ = client.CoreV1().Pods("nsB").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "nsB",
			UID:       types.UID("uid-B"),
			Labels:    map[string]string{tenantLabel: "tenant-B"},
		},
		Spec: corev1.PodSpec{NodeName: "other-node"},
	}, metav1.CreateOptions{})

	r := New(pods, Config{
		TenantLabelKey: tenantLabel,
		NodeName:       "node-1",
	})
	_ = r.Start(ctx)
	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	tenant, retry, err := r.Resolve("nsA", "agent", "uid-A")
	if err != nil || retry || tenant != "tenant-A" {
		t.Fatalf("resolve (nsA) failed: tenant=%q retry=%v err=%v", tenant, retry, err)
	}

	tenant, retry, err = r.Resolve("nsB", "agent", "uid-B")
	if err == nil || !retry {
		t.Fatalf("expected retryable miss for nsB: tenant=%q retry=%v err=%v", tenant, retry, err)
	}
}

// 11) Concurrency: many resolves succeed when pre-seeded BEFORE start
func TestResolver_ConcurrentResolves_Preseed_NoRaces(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	// PRESEED pods BEFORE starting informers -> arrive in the initial LIST
	const N = 100
	for i := 0; i < N; i++ {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("p-%03d", i),
				Namespace: "ns1",
				UID:       types.UID(fmt.Sprintf("uid-%03d", i)),
				Labels:    map[string]string{tenantLabel: "tenant-Z"},
			},
		}
		if _, err := client.CoreV1().Pods("ns1").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatalf("preseed pod: %v", err)
		}
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	_ = r.Start(ctx)
	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced)
	waitTrue(t, time.Second, func() bool { return r.Ready() })

	var wg sync.WaitGroup
	errCh := make(chan error, 1000)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ix := i % N
			uid := fmt.Sprintf("uid-%03d", ix)
			tenant, retry, err := r.Resolve("ns1", fmt.Sprintf("p-%03d", ix), uid)
			if err != nil || retry || tenant != "tenant-Z" {
				errCh <- fmt.Errorf("resolve mismatch %d: t=%q retry=%v err=%v", i, tenant, retry, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}
}
