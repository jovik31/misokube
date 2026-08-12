package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAdmissionHandlerUsesRequestKind(
	t *testing.T,
) {
	server := newTestWebhookServer(
		readyTenant("tenant-a", 1),
	)

	podRequest := podAdmissionRequest(
		t,
		tenantPod("tenant-a"),
	)

	podRequest.UID = types.UID("request-a")
	podRequest.Kind = podGVK
	podRequest.RequestKind = nil

	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.
				SchemeGroupVersion.
				String(),
			Kind: "AdmissionReview",
		},
		Request: podRequest,
	}

	body, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		validateEndpoint,
		bytes.NewReader(body),
	)

	request.Header.Set(
		contentTypeHeader,
		contentTypeJSON,
	)

	recorder := httptest.NewRecorder()

	server.admissionValidationHandler(
		recorder,
		request,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"got HTTP status %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	var response admissionv1.AdmissionReview

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf(
			"decode admission response: %v",
			err,
		)
	}

	if response.Response == nil {
		t.Fatal("admission response is missing")
	}

	if !response.Response.Allowed {
		t.Fatalf(
			"Pod was denied: %s",
			response.Response.Result.Message,
		)
	}

	if response.Response.UID != podRequest.UID {
		t.Fatalf(
			"got UID %q, want %q",
			response.Response.UID,
			podRequest.UID,
		)
	}
}

func TestAdmissionHandlerRejectsMissingRequest(
	t *testing.T,
) {
	server := newTestWebhookServer()

	body, err := json.Marshal(
		admissionv1.AdmissionReview{},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		validateEndpoint,
		bytes.NewReader(body),
	)

	request.Header.Set(
		contentTypeHeader,
		contentTypeJSON,
	)

	recorder := httptest.NewRecorder()

	server.admissionValidationHandler(
		recorder,
		request,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"got HTTP status %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}
