package webhook

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

type wrappedWritter struct {
	http.ResponseWriter
	statusCode int
}

type Middleware func(http.Handler) http.Handler

func runMiddleware(xs ...Middleware) Middleware {

	return func(next http.Handler) http.Handler {
		for i := len(xs) - 1; i >= 0; i-- {
			x := xs[i]
			next = x(next)
		}
		return next
	}
}

func (w *wrappedWritter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	w.statusCode = statusCode
}

// validate http request fields
func validatingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		//validate http method
		if r.Method != http.MethodPost {
			http.Error(w, fmt.Sprintf("%s method is not allowed", r.Method), http.StatusMethodNotAllowed)
			klog.Error(http.StatusMethodNotAllowed, fmt.Sprintf(" %s method is not allowed", r.Method))
			return
		}

		//validate headers
		contentType := r.Header.Get(contentTypeHeader)
		if contentType != contentTypeJSON {
			http.Error(w, fmt.Sprintf("%s is not a supported content type", contentType), http.StatusUnsupportedMediaType)
			klog.Error(http.StatusUnsupportedMediaType, fmt.Sprintf(" %s is not a supported content type", contentType))
			return
		}

		//check if body is empty
		if r.Body == nil {
			http.Error(w, fmt.Sprintf(" %s request is empty ", r.Method), http.StatusMethodNotAllowed)
			klog.Error(fmt.Sprintf("error code %d request has an empty body", http.StatusBadRequest))
			return
		}

		next.ServeHTTP(&wrappedWritter{ResponseWriter: w, statusCode: http.StatusOK}, r)

	})
}

// logging middleware to log info on the requests made
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()
		wrappedWriter := &wrappedWritter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}
		next.ServeHTTP(wrappedWriter, r)
		klog.Info(r.RemoteAddr, " ", wrappedWriter.statusCode, " ", r.Method, " ", r.URL.Path, " ", time.Since(start))

	})

}
