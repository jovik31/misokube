package webhook

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

func TestGenerateTLSFiles(
	t *testing.T,
) {
	directory := t.TempDir()

	files, err := GenerateTLSFiles(
		WebhookServiceName,
		"default",
		directory,
	)

	if err != nil {
		t.Fatal(err)
	}

	if len(files.CABundle) == 0 {
		t.Fatal(
			"CA bundle is empty",
		)
	}

	certPEM, err := os.ReadFile(
		files.CertFile,
	)
	if err != nil {
		t.Fatal(err)
	}

	certBlock, _ := pem.Decode(
		certPEM,
	)
	if certBlock == nil {
		t.Fatal(
			"server certificate is not PEM encoded",
		)
	}

	serverCert, err :=
		x509.ParseCertificate(
			certBlock.Bytes,
		)
	if err != nil {
		t.Fatal(err)
	}

	caBlock, _ := pem.Decode(
		files.CABundle,
	)
	if caBlock == nil {
		t.Fatal(
			"CA certificate is not PEM encoded",
		)
	}

	caCert, err :=
		x509.ParseCertificate(
			caBlock.Bytes,
		)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	serviceDNSName :=
		"setera-webhook.default.svc"

	if _, err := serverCert.Verify(
		x509.VerifyOptions{
			DNSName: serviceDNSName,
			Roots:   roots,
		},
	); err != nil {
		t.Fatalf(
			"verify webhook certificate: %v",
			err,
		)
	}
}
