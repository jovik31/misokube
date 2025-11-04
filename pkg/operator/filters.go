package operator

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// Predicates let you filter which events should enqueue.
type AddPredicate func(obj any) bool
type UpdatePredicate func(oldObj, newObj any) bool
type DeletePredicate func(obj any) bool

// FilteredInformerHandlers enqueues only if predicates pass.
// Nil predicates are treated as "always true".
func FilteredInformerHandlers(
	c *BaseOperator,
	src Source,
	addEv, updEv, delEv Event,
	onAdd AddPredicate,
	onUpdate UpdatePredicate,
	onDelete DeletePredicate,
) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if onAdd == nil || onAdd(obj) {
				c.EnqueueObjectWith(src, addEv, obj)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			if onUpdate == nil || onUpdate(oldObj, newObj) {
				c.EnqueueObjectWith(src, updEv, newObj)
			}
		},
		DeleteFunc: func(obj any) {
			if onDelete == nil || onDelete(obj) {
				c.EnqueueObjectWith(src, delEv, obj)
			}
		},
	}
}

// UpdateFieldChanged compares a projected field with DeepEqual.
func UpdateFieldChanged(field func(obj any) any) UpdatePredicate {
	return func(oldObj, newObj any) bool {
		return !reflect.DeepEqual(field(oldObj), field(newObj))
	}
}

// UpdateComparableChanged compares a projected comparable value.
func UpdateComparableChanged[T comparable](field func(obj any) T) UpdatePredicate {
	return func(oldObj, newObj any) bool {
		return field(oldObj) != field(newObj)
	}
}

// UpdateGenerationChanged triggers only when .metadata.generation changes.
func UpdateGenerationChanged() UpdatePredicate {
	return func(oldObj, newObj any) bool {
		o1, ok1 := metaObjectFrom(oldObj)
		o2, ok2 := metaObjectFrom(newObj)
		return ok1 && ok2 && o1.GetGeneration() != o2.GetGeneration()
	}
}

// UpdateLabelChanged triggers when a specific label value changes.
func UpdateLabelChanged(key string) UpdatePredicate {
	return func(oldObj, newObj any) bool {
		o1, ok1 := metaObjectFrom(oldObj)
		o2, ok2 := metaObjectFrom(newObj)
		if !ok1 || !ok2 {
			return false
		}
		return o1.GetLabels()[key] != o2.GetLabels()[key]
	}
}

// UpdateAnnotationChanged triggers when a specific annotation value changes.
func UpdateAnnotationChanged(key string) UpdatePredicate {
	return func(oldObj, newObj any) bool {
		o1, ok1 := metaObjectFrom(oldObj)
		o2, ok2 := metaObjectFrom(newObj)
		if !ok1 || !ok2 {
			return false
		}
		return o1.GetAnnotations()[key] != o2.GetAnnotations()[key]
	}
}

// Keep metav1 import used (lint aid)
var _ metav1.Object
