package operator

import (
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	// internal
	seterav1 "github/setera/pkg/api/setera.com/v1"
)

func ContainsString(slice []string, item string) bool {

	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

//func jsonpaytload

func IsFinalizerRvUpdate_tenant(old, new *seterav1.Tenant) bool {
	// Check if only finalizers are updated
	oldCopy := old.DeepCopy()
	newCopy := new.DeepCopy()

	// remove the finalizers for comparison
	oldCopy.Finalizers = nil
	newCopy.Finalizers = nil

	oldCopy.ObjectMeta.ResourceVersion = ""
	newCopy.ObjectMeta.ResourceVersion = ""

	return equality.Semantic.DeepEqual(oldCopy, newCopy)

}
func RemoveIndex[T comparable](slice []T, val T) []T {
	idx := -1
	for i, v := range slice {
		if v == val {
			idx = i
			break
		}
	}
	if idx < 0 {
		// not found; nothing to remove
		return slice
	}
	// remove at idx
	return append(slice[:idx], slice[idx+1:]...)
}

// metaObjectFrom extracts metav1.Object from regular objects or tombstones.
func metaObjectFrom(obj any) (metav1.Object, bool) {
	if o, ok := obj.(metav1.Object); ok {
		return o, true
	}
	if tomb, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if o, ok := tomb.Obj.(metav1.Object); ok {
			return o, true
		}
	}
	return nil, false
}
