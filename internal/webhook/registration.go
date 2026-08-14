package webhook

import (
	"context"
	"errors"
	"fmt"

	"github/setera/pkg/tenantmeta"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureMutatingWebhookConfiguration creates or updates Pod admission.
func EnsureMutatingWebhookConfiguration(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	namespace string,
	caBundle []byte,
) error {
	if kubeClient == nil {
		return errors.New(
			"kubernetes client is nil",
		)
	}

	if namespace == "" {
		return errors.New(
			"webhook namespace is empty",
		)
	}

	if len(caBundle) == 0 {
		return errors.New(
			"webhook CA bundle is empty",
		)
	}

	path := validateEndpoint
	port := WebhookServicePort

	failurePolicy := admissionregistrationv1.Fail
	sideEffects := admissionregistrationv1.SideEffectClassNone
	scope := admissionregistrationv1.NamespacedScope

	timeoutSeconds := int32(5)

	desired :=
		&admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: WebhookConfigurationName,
			},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					Name: "pods.setera.com",

					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Namespace: namespace,
							Name:      WebhookServiceName,
							Path:      &path,
							Port:      &port,
						},
						CABundle: caBundle,
					},

					Rules: []admissionregistrationv1.RuleWithOperations{
						{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups: []string{
									"",
								},
								APIVersions: []string{
									"v1",
								},
								Resources: []string{
									"pods",
								},
								Scope: &scope,
							},
						},
					},

					FailurePolicy:  &failurePolicy,
					SideEffects:    &sideEffects,
					TimeoutSeconds: &timeoutSeconds,

					AdmissionReviewVersions: []string{
						"v1",
					},

					ObjectSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      tenantmeta.PodTenantLabel,
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
				},
			},
		}

	client := kubeClient.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations()

	current, err := client.Get(
		ctx,
		WebhookConfigurationName,
		metav1.GetOptions{},
	)

	if apierrors.IsNotFound(err) {
		if _, err := client.Create(
			ctx,
			desired,
			metav1.CreateOptions{},
		); err != nil {
			return fmt.Errorf(
				"create mutating webhook configuration: %w",
				err,
			)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf(
			"get mutating webhook configuration: %w",
			err,
		)
	}

	desired.ResourceVersion =
		current.ResourceVersion

	if _, err := client.Update(
		ctx,
		desired,
		metav1.UpdateOptions{},
	); err != nil {
		return fmt.Errorf(
			"update mutating webhook configuration: %w",
			err,
		)
	}

	return nil
}
