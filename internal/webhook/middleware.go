package webhook

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

type wrappedWriter struct {
	http.ResponseWriter
	statusCode int
}

type Middleware func(http.Handler) http.Handler

func runMiddleware(
	middlewares ...Middleware,
) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}

		return next
	}
}

func (w *wrappedWriter) WriteHeader(
	statusCode int,
) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func validatingMiddleware(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if r.Method != http.MethodPost {
				http.Error(
					w,
					fmt.Sprintf(
						"%s method is not allowed",
						r.Method,
					),
					http.StatusMethodNotAllowed,
				)

				return
			}

			contentType := r.Header.Get(
				contentTypeHeader,
			)

			if contentType != contentTypeJSON {
				http.Error(
					w,
					fmt.Sprintf(
						"%s is not a supported content type",
						contentType,
					),
					http.StatusUnsupportedMediaType,
				)

				return
			}

			if r.Body == nil {
				http.Error(
					w,
					"request body is empty",
					http.StatusBadRequest,
				)

				return
			}

			next.ServeHTTP(w, r)
		},
	)
}

func loggingMiddleware(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			start := time.Now()

			writer := &wrappedWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(writer, r)

			klog.Info(
				"webhook request",
				"remoteAddress",
				r.RemoteAddr,
				"status",
				writer.statusCode,
				"method",
				r.Method,
				"path",
				r.URL.Path,
				"duration",
				time.Since(start),
			)
		},
	)
}
