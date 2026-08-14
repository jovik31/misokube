package ebpfmanager

import (
	"context"
	"errors"
	"testing"
)

func TestRecoverLocalPodUsesLocalInstallPath(t *testing.T) {
	fake := newFakeDependencies()
	manager := newWithDependencies(fake.dependencies())
	pod := testLocalPod()

	if err := manager.RecoverLocalPod(
		context.Background(),
		pod,
	); err != nil {
		t.Fatal(err)
	}

	if fake.attachCalls != 1 {
		t.Fatalf(
			"attach calls = %d, want 1",
			fake.attachCalls,
		)
	}
	if fake.upsertCalls != 1 {
		t.Fatalf(
			"upsert calls = %d, want 1",
			fake.upsertCalls,
		)
	}

	if _, ok := manager.local[pod.IP]; !ok {
		t.Fatal("recovered Pod was not added to manager state")
	}
}

func TestRecoverLocalPodWrapsInstallError(t *testing.T) {
	fake := newFakeDependencies()
	fake.attachErr = errors.New("attach failed")

	manager := newWithDependencies(fake.dependencies())

	err := manager.RecoverLocalPod(
		context.Background(),
		testLocalPod(),
	)
	if !errors.Is(err, fake.attachErr) {
		t.Fatalf(
			"error = %v, want wrapped %v",
			err,
			fake.attachErr,
		)
	}
}
