package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ---------- shared helpers (bench-only) ----------

func waitTrueB(b *testing.B, timeout time.Duration, fn func() bool) {
	dead := time.Now().Add(timeout)
	for time.Now().Before(dead) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.Fatal("condition not met before timeout")
}

func waitStoreHasPodB(b *testing.B, pods coreinformers.PodInformer, ns, name string, timeout time.Duration) {
	key := ns + "/" + name
	dead := time.Now().Add(timeout)
	for time.Now().Before(dead) {
		if obj, exists, _ := pods.Informer().GetStore().GetByKey(key); exists && obj != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.Fatalf("pod %s not present in informer store before timeout", key)
}

// warmUpAllPods ensures every preseeded pod resolves at least once
// so the resolver snapshot is fully populated before timing.
func warmUpAllPods(b *testing.B, r *ResolverImpl, ns string, N int, withUID bool) {
	const (
		perPodTimeout = 2 * time.Second
		sleepStep     = 5 * time.Millisecond
	)
	for i := 0; i < N; i++ {
		dead := time.Now().Add(perPodTimeout)
		name := fmt.Sprintf("p-%06d", i)
		uid := fmt.Sprintf("uid-%06d", i)
		for {
			var u string
			if withUID {
				u = uid
			}
			tenant, retry, err := r.Resolve(ns, name, u)
			if err == nil && !retry && tenant == "tenant-Z" {
				break // this pod is warmed up
			}
			// Only tolerate transient, retryable states during warmup
			if err != nil && !retry {
				b.Fatalf("unexpected non-retryable warmup error for %s/%s: %v", ns, name, err)
			}
			if time.Now().After(dead) {
				b.Fatalf("warmup timeout for %s/%s (uid=%s): last err=%v retry=%v", ns, name, uid, err, retry)
			}
			time.Sleep(sleepStep)
		}
	}
}

// Preseed N labeled pods into "bench" BEFORE starting informers; returns ready resolver.
// Also performs warm-up so snapshot contains all mappings before the benchmark timer starts.
func makeReadyResolverWithPods(b *testing.B, N int) *ResolverImpl {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	b.Cleanup(cancel)

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	// Preseed (goes via initial LIST)
	for i := 0; i < N; i++ {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("p-%06d", i),
				Namespace: "bench",
				UID:       types.UID(fmt.Sprintf("uid-%06d", i)),
				Labels:    map[string]string{tenantLabel: "tenant-Z"},
			},
		}
		if _, err := client.CoreV1().Pods("bench").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			b.Fatalf("seed pod: %v", err)
		}
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		b.Fatalf("resolver start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		b.Fatal("pod informer failed to sync")
	}
	waitTrueB(b, time.Second, func() bool { return r.Ready() })

	// Warm-up: ensure every preseeded pod resolves at least once (UID path is the strictest)
	warmUpAllPods(b, r, "bench", N, true)

	return r
}

// Starts informers with NO objects; for NotIndexed miss benches.
func makeReadyResolverEmpty(b *testing.B) *ResolverImpl {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	b.Cleanup(cancel)

	_, factory := newDeps()
	pods := factory.Core().V1().Pods()

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		b.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		b.Fatal("pod informer failed to sync")
	}
	waitTrueB(b, time.Second, func() bool { return r.Ready() })
	return r
}

// ---------- Benchmarks ----------

// Hot path: resolve by UID with snapshot hit (sequential).
func BenchmarkResolve_Snapshot_UID(b *testing.B) {
	const N = 5000
	r := makeReadyResolverWithPods(b, N)

	var ctr uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := int(atomic.AddUint64(&ctr, 1)) % N
		uid := fmt.Sprintf("uid-%06d", j)
		tenant, retry, err := r.Resolve("bench", fmt.Sprintf("p-%06d", j), uid)
		if err != nil || retry || tenant != "tenant-Z" {
			b.Fatalf("resolve(uid) failed: t=%q retry=%v err=%v", tenant, retry, err)
		}
	}
}

// Hot path: resolve by UID with snapshot hit (parallel).
func BenchmarkResolve_Snapshot_UID_Parallel(b *testing.B) {
	const N = 5000
	r := makeReadyResolverWithPods(b, N)

	var ctr uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			j := int(atomic.AddUint64(&ctr, 1)) % N
			uid := fmt.Sprintf("uid-%06d", j)
			tenant, retry, err := r.Resolve("bench", fmt.Sprintf("p-%06d", j), uid)
			if err != nil || retry || tenant != "tenant-Z" {
				b.Fatalf("resolve(uid) failed: t=%q retry=%v err=%v", tenant, retry, err)
			}
		}
	})
}

// Hot path: resolve by name with snapshot hit (sequential).
func BenchmarkResolve_Snapshot_Name(b *testing.B) {
	const N = 5000
	r := makeReadyResolverWithPods(b, N)

	var ctr uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := int(atomic.AddUint64(&ctr, 1)) % N
		pod := fmt.Sprintf("p-%06d", j)
		tenant, retry, err := r.Resolve("bench", pod, "")
		if err != nil || retry || tenant != "tenant-Z" {
			b.Fatalf("resolve(name) failed: t=%q retry=%v err=%v", tenant, retry, err)
		}
	}
}

// Hot path: resolve by name with snapshot hit (parallel).
func BenchmarkResolve_Snapshot_Name_Parallel(b *testing.B) {
	const N = 5000
	r := makeReadyResolverWithPods(b, N)

	var ctr uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			j := int(atomic.AddUint64(&ctr, 1)) % N
			pod := fmt.Sprintf("p-%06d", j)
			tenant, retry, err := r.Resolve("bench", pod, "")
			if err != nil || retry || tenant != "tenant-Z" {
				b.Fatalf("resolve(name) failed: t=%q retry=%v err=%v", tenant, retry, err)
			}
		}
	})
}

// Miss classification: NotIndexed (store miss). Deterministic since no objects exist.
func BenchmarkResolve_Miss_NotIndexed(b *testing.B) {
	r := makeReadyResolverEmpty(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, retry, err := r.Resolve("nsX", "nope", "uid-nope")
		if !retry || err == nil || !errors.Is(err, ErrNotIndexed) {
			b.Fatalf("expected ErrNotIndexed, got retry=%v err=%v", retry, err)
		}
	}
}

func BenchmarkResolve_Miss_NotIndexed_Parallel(b *testing.B) {
	r := makeReadyResolverEmpty(b)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, retry, err := r.Resolve("nsX", "nope", "uid-nope")
			if !retry || err == nil || !errors.Is(err, ErrNotIndexed) {
				b.Fatalf("expected ErrNotIndexed, got retry=%v err=%v", retry, err)
			}
		}
	})
}

// Miss classification: NoTenantLabel (store hit without label). Preseed unlabeled BEFORE start,
// and verify presence in the store before timing to avoid race with watch delivery.
func BenchmarkResolve_Miss_NoTenantLabel(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	b.Cleanup(cancel)

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	// Preseed unlabeled pod (initial LIST)
	_, err := client.CoreV1().Pods("bench").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nolabel",
			Namespace: "bench",
			UID:       types.UID("uid-nolabel"),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		b.Fatalf("preseed unlabeled pod: %v", err)
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		b.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		b.Fatal("pod informer failed to sync")
	}
	waitTrueB(b, time.Second, func() bool { return r.Ready() })
	waitStoreHasPodB(b, pods, "bench", "nolabel", 2*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, retry, err := r.Resolve("bench", "nolabel", "uid-nolabel")
		if !retry || err == nil || !errors.Is(err, ErrNoTenantLabel) {
			b.Fatalf("expected ErrNoTenantLabel, got retry=%v err=%v", retry, err)
		}
	}
}

func BenchmarkResolve_Miss_NoTenantLabel_Parallel(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	b.Cleanup(cancel)

	client, factory := newDeps()
	pods := factory.Core().V1().Pods()

	_, err := client.CoreV1().Pods("bench").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nolabel",
			Namespace: "bench",
			UID:       types.UID("uid-nolabel"),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		b.Fatalf("preseed unlabeled pod: %v", err)
	}

	r := New(pods, Config{TenantLabelKey: tenantLabel})
	if err := r.Start(ctx); err != nil {
		b.Fatalf("start: %v", err)
	}
	factory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), pods.Informer().HasSynced); !ok {
		b.Fatal("pod informer failed to sync")
	}
	waitTrueB(b, time.Second, func() bool { return r.Ready() })
	waitStoreHasPodB(b, pods, "bench", "nolabel", 2*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, retry, err := r.Resolve("bench", "nolabel", "uid-nolabel")
			if !retry || err == nil || !errors.Is(err, ErrNoTenantLabel) {
				b.Fatalf("expected ErrNoTenantLabel, got retry=%v err=%v", retry, err)
			}
		}
	})
}
