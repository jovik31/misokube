package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
)

func (ws *WebhookServer) admissionValidationHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	var requestAdmissionReview admissionv1.AdmissionReview

	if err := json.NewDecoder(r.Body).Decode(
		&requestAdmissionReview,
	); err != nil {
		http.Error(
			w,
			fmt.Sprintf(
				"decode admission request: %v",
				err,
			),
			http.StatusBadRequest,
		)

		return
	}

	if requestAdmissionReview.Request == nil {
		http.Error(
			w,
			"admission request is missing",
			http.StatusBadRequest,
		)

		return
	}

	var admissionResponse *admissionv1.AdmissionResponse
	var err error

	switch requestAdmissionReview.Request.Kind {
	case daemonsetGVK:
		admissionResponse = createAdmissionResponse(
			true,
			"DaemonSet is valid",
		)

	case deploymentGVK:
		admissionResponse = createAdmissionResponse(
			true,
			"Deployment is valid",
		)

	case podGVK:
		admissionResponse, err = ws.admitPod(
			r.Context(),
			requestAdmissionReview.Request,
		)

	case tenantGVK:
		admissionResponse, err = ws.validateTenant(
			requestAdmissionReview.Request,
		)

	default:
		admissionResponse = createAdmissionResponse(
			true,
			"resource is not managed by Setera",
		)
	}

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)

		return
	}

	response := newAdmissionReview(
		requestAdmissionReview,
		admissionResponse,
	)

	responseBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(
			w,
			"encode admission response",
			http.StatusInternalServerError,
		)

		return
	}

	klog.Info(
		"processed admission request",
		"uid",
		requestAdmissionReview.Request.UID,
		"kind",
		requestAdmissionReview.Request.Kind.Kind,
	)

	w.Header().Set(
		contentTypeHeader,
		contentTypeJSON,
	)

	if _, err := w.Write(responseBytes); err != nil {
		klog.ErrorS(
			err,
			"write admission response",
		)
	}
}
