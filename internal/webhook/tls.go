package webhook

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const certificateLifetime = 365 * 24 * time.Hour

type TLSFiles struct {
	CABundle []byte
	CertFile string
	KeyFile  string
}

// GenerateTLSFiles creates a self-signed CA and a webhook serving certificate.
func GenerateTLSFiles(
	serviceName string,
	namespace string,
	directory string,
) (TLSFiles, error) {
	if serviceName == "" {
		return TLSFiles{}, errors.New(
			"webhook service name is empty",
		)
	}

	if namespace == "" {
		return TLSFiles{}, errors.New(
			"webhook namespace is empty",
		)
	}

	if directory == "" {
		return TLSFiles{}, errors.New(
			"webhook TLS directory is empty",
		)
	}

	if err := os.MkdirAll(
		directory,
		0o700,
	); err != nil {
		return TLSFiles{}, fmt.Errorf(
			"create webhook TLS directory: %w",
			err,
		)
	}

	caPublicKey, caPrivateKey, err :=
		ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"generate webhook CA key: %w",
			err,
		)
	}

	now := time.Now()

	caTemplate := &x509.Certificate{
		SerialNumber: newSerialNumber(),

		Subject: pkix.Name{
			CommonName: "setera-webhook-ca",
		},

		NotBefore: now.Add(-time.Minute),
		NotAfter:  now.Add(certificateLifetime),

		KeyUsage: x509.KeyUsageDigitalSignature |
			x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(
		rand.Reader,
		caTemplate,
		caTemplate,
		caPublicKey,
		caPrivateKey,
	)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"create webhook CA certificate: %w",
			err,
		)
	}

	caCertificate, err :=
		x509.ParseCertificate(caDER)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"parse webhook CA certificate: %w",
			err,
		)
	}

	serverPublicKey, serverPrivateKey, err :=
		ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"generate webhook server key: %w",
			err,
		)
	}

	serviceDNSName := fmt.Sprintf(
		"%s.%s.svc",
		serviceName,
		namespace,
	)

	serverTemplate := &x509.Certificate{
		SerialNumber: newSerialNumber(),

		Subject: pkix.Name{
			CommonName: serviceDNSName,
		},

		DNSNames: []string{
			serviceDNSName,
			fmt.Sprintf(
				"%s.%s.svc.cluster.local",
				serviceName,
				namespace,
			),
		},

		NotBefore: now.Add(-time.Minute),
		NotAfter:  now.Add(certificateLifetime),

		KeyUsage: x509.KeyUsageDigitalSignature,

		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	serverDER, err := x509.CreateCertificate(
		rand.Reader,
		serverTemplate,
		caCertificate,
		serverPublicKey,
		caPrivateKey,
	)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"create webhook server certificate: %w",
			err,
		)
	}

	serverKeyDER, err :=
		x509.MarshalPKCS8PrivateKey(
			serverPrivateKey,
		)
	if err != nil {
		return TLSFiles{}, fmt.Errorf(
			"marshal webhook server key: %w",
			err,
		)
	}

	caPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: caDER,
		},
	)

	certPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: serverDER,
		},
	)

	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: serverKeyDER,
		},
	)

	certFile := filepath.Join(
		directory,
		"tls.crt",
	)

	keyFile := filepath.Join(
		directory,
		"tls.key",
	)

	if err := os.WriteFile(
		certFile,
		certPEM,
		0o600,
	); err != nil {
		return TLSFiles{}, fmt.Errorf(
			"write webhook certificate: %w",
			err,
		)
	}

	if err := os.WriteFile(
		keyFile,
		keyPEM,
		0o600,
	); err != nil {
		return TLSFiles{}, fmt.Errorf(
			"write webhook key: %w",
			err,
		)
	}

	return TLSFiles{
		CABundle: caPEM,
		CertFile: certFile,
		KeyFile:  keyFile,
	}, nil
}

func newSerialNumber() *big.Int {
	limit := new(big.Int).Lsh(
		big.NewInt(1),
		128,
	)

	serial, err := rand.Int(
		rand.Reader,
		limit,
	)
	if err != nil {
		return big.NewInt(
			time.Now().UnixNano(),
		)
	}

	return serial
}
