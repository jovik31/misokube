package webhook

import (
	"encoding/json"
	"fmt"

	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
)

func (ws *WebhookServer) admissionValidationHandler(w http.ResponseWriter, r *http.Request) {

	//decode request body
	var requestAdmissionReview = admissionv1.AdmissionReview{}
	if err := json.NewDecoder(r.Body).Decode(&requestAdmissionReview); err != nil {

		http.Error(w, fmt.Sprintf("failed decoding admission request: %s", err), http.StatusBadRequest)
		klog.Error(fmt.Sprintf(" error code: %d failed decoding admission request with error %s", http.StatusBadRequest, err))
		return
	}

	// check if there is a request resource field so we can then read it
	if requestAdmissionReview.Request.RequestKind == nil {
		http.Error(w, "failed to extract resource from request", http.StatusBadRequest)
		klog.Error(fmt.Sprintf(" error code: %d failed to extract resource from request", http.StatusBadRequest))
		return
	}

	// declare variables for usage inside switch case
	var admissionResponse = &admissionv1.AdmissionResponse{}
	var err error

	//need to compare structs and not the pointer
	switch *requestAdmissionReview.Request.RequestKind {

	//daemonsets
	case daemonsetGVK:

	//deployments
	case deploymentGVK:

	//pods
	case podGVK:
		//call pod validation
		if admissionResponse, err = ws.validatePod(requestAdmissionReview.Request); err != nil {
			http.Error(w, fmt.Sprintf("%s", err), http.StatusInternalServerError)
			return

		}
	//tenants
	case tenantGVK:
		//call tenant validation
		if admissionResponse, err = ws.validateTenant(requestAdmissionReview.Request); err != nil {
			http.Error(w, fmt.Sprintf("%s", err), http.StatusInternalServerError)
			return
		}
	}

	//create new admission review with response
	responseObj := newAdmissionReview(requestAdmissionReview, admissionResponse)

	klog.Info("request ID ", requestAdmissionReview.Request.UID, " ", requestAdmissionReview.Request.Kind.Kind)

	var respBytes []byte
	if respBytes, err = json.Marshal(responseObj); err != nil {
		klog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return

	}
	w.Header().Set(contentTypeHeader, contentTypeJSON)
	if _, err := w.Write(respBytes); err != nil {
		klog.Error(err)

	}
}
