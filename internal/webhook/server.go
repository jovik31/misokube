package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	seterav1clientset "github/setera/pkg/generated/clientset/versioned"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const shutdownTimeout = 5 * time.Second

type WebhookServer struct {
	port               string
	tlsCert            string
	tlsKey             string
	Server             *http.Server
	seterav1Clientset  seterav1clientset.Interface
	kubernetsClientset kubernetes.Interface
}

func NewWebhookServer(
	seteraClient seterav1clientset.Interface,
	k8sClientset kubernetes.Interface,
	tlsCert string,
	tlsKey string,
) *WebhookServer {
	return &WebhookServer{
		port:               serverPort,
		tlsCert:            tlsCert,
		tlsKey:             tlsKey,
		seterav1Clientset:  seteraClient,
		kubernetsClientset: k8sClientset,
	}
}

// Run serves admission requests until the context stops or the server fails.
func (ws *WebhookServer) Run(
	ctx context.Context,
) error {
	if ws.tlsCert == "" {
		return errors.New(
			"webhook TLS certificate path is empty",
		)
	}

	if ws.tlsKey == "" {
		return errors.New(
			"webhook TLS key path is empty",
		)
	}

	router := http.NewServeMux()

	router.HandleFunc(
		validateEndpoint,
		ws.admissionValidationHandler,
	)

	middleware := runMiddleware(
		loggingMiddleware,
		validatingMiddleware,
	)

	ws.Server = &http.Server{
		Addr:    ws.port,
		Handler: middleware(router),
	}

	errCh := make(chan error, 1)

	go func() {
		klog.Info(
			"webhook server started",
			"address",
			ws.Server.Addr,
		)

		errCh <- ws.Server.ListenAndServeTLS(
			ws.tlsCert,
			ws.tlsKey,
		)
	}()

	select {
	case err := <-errCh:
		if errors.Is(
			err,
			http.ErrServerClosed,
		) {
			return nil
		}

		return fmt.Errorf(
			"serve webhook: %w",
			err,
		)

	case <-ctx.Done():
		shutdownCtx, cancel :=
			context.WithTimeout(
				context.Background(),
				shutdownTimeout,
			)

		defer cancel()

		if err := ws.Server.Shutdown(
			shutdownCtx,
		); err != nil {
			return fmt.Errorf(
				"stop webhook server: %w",
				err,
			)
		}

		err := <-errCh

		if err != nil &&
			!errors.Is(
				err,
				http.ErrServerClosed,
			) {
			return fmt.Errorf(
				"serve webhook: %w",
				err,
			)
		}

		return nil
	}
}
