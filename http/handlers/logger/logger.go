package logger

import (
	"context"
	"net/http"

	"github.com/sirupsen/logrus"
)

type ctxKey struct{}

func Handler() func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler { //nolint
		return addLogger(h)
	}
}
func addLogger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// add all headers to the log
		headers := make(map[string]string)
		for k, v := range r.Header {
			headers[k] = v[0]
		}
		entry := logrus.WithFields(logrus.Fields{
			"user-agent": r.UserAgent(),
			"ip-address": r.RemoteAddr,
			"method":     r.Method,
			"url":        r.URL.String(),
			"headers":    headers,
		})
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), &ctxKey{}, entry)))
	})
}

func FromContext(ctx context.Context) *logrus.Entry {
	entry := ctx.Value(&ctxKey{})

	// The ctxKey only gives this type.
	return entry.(*logrus.Entry) // nolint:forcetypeassert
}
