package webhook

import (
	v1 "github/setera/pkg/api/setera.com/v1"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var runtimeScheme = runtime.NewScheme()

func init() {
	corev1.AddToScheme(runtimeScheme)
	admissionv1.AddToScheme(runtimeScheme)
	v1.AddToScheme(runtimeScheme)
}
