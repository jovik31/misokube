package webhook

import (
	"bytes"
	"context"
	"testing"

	"github/setera/pkg/tenantmeta"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureMutatingWebhookConfiguration(
	t *testing.T,
) {
	client := fake.NewSimpleClientset()

	caBundle := []byte("test-ca")

	if err := EnsureMutatingWebhookConfiguration(
		context.Background(),
		client,
		"default",
		caBundle,
	); err != nil {
		t.Fatal(err)
	}

	config, err := client.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(
			context.Background(),
			WebhookConfigurationName,
			metav1.GetOptions{},
		)

	if err != nil {
		t.Fatal(err)
	}

	if len(config.Webhooks) != 1 {
		t.Fatalf(
			"got %d webhooks, want 1",
			len(config.Webhooks),
		)
	}

	got := config.Webhooks[0]

	if got.ClientConfig.Service == nil {
		t.Fatal(
			"webhook Service reference is missing",
		)
	}

	if got.ClientConfig.Service.Name !=
		WebhookServiceName {
		t.Fatalf(
			"got Service %q, want %q",
			got.ClientConfig.Service.Name,
			WebhookServiceName,
		)
	}

	if got.ClientConfig.Service.Namespace !=
		"default" {
		t.Fatalf(
			"got namespace %q, want default",
			got.ClientConfig.Service.Namespace,
		)
	}

	if !bytes.Equal(
		got.ClientConfig.CABundle,
		caBundle,
	) {
		t.Fatal(
			"webhook CA bundle does not match",
		)
	}

	if got.FailurePolicy == nil ||
		*got.FailurePolicy !=
			admissionregistrationv1.Fail {
		t.Fatal(
			"webhook failure policy must be Fail",
		)
	}

	if got.ObjectSelector == nil ||
		len(
			got.ObjectSelector.MatchExpressions,
		) != 1 {
		t.Fatal(
			"webhook tenant object selector is missing",
		)
	}

	if got.ObjectSelector.
		MatchExpressions[0].
		Key != tenantmeta.PodTenantLabel {
		t.Fatalf(
			"got selector key %q, want %q",
			got.ObjectSelector.
				MatchExpressions[0].
				Key,
			tenantmeta.PodTenantLabel,
		)
	}
}

func TestEnsureMutatingWebhookConfigurationUpdatesCA(
	t *testing.T,
) {
	oldCA := []byte("old-ca")
	newCA := []byte("new-ca")

	client := fake.NewSimpleClientset(
		&admissionregistrationv1.
			MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: WebhookConfigurationName,
			},
			Webhooks: []admissionregistrationv1.
				MutatingWebhook{
				{
					Name: "pods.setera.com",
					ClientConfig: admissionregistrationv1.
						WebhookClientConfig{
						CABundle: oldCA,
					},
				},
			},
		},
	)

	if err := EnsureMutatingWebhookConfiguration(
		context.Background(),
		client,
		"default",
		newCA,
	); err != nil {
		t.Fatal(err)
	}

	config, err := client.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(
			context.Background(),
			WebhookConfigurationName,
			metav1.GetOptions{},
		)

	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		config.Webhooks[0].
			ClientConfig.
			CABundle,
		newCA,
	) {
		t.Fatal(
			"webhook CA bundle was not updated",
		)
	}
}
