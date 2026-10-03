package logger_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weeb-vip/notifications-service/http/handlers/logger"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	req, _ := http.NewRequest("GET", "/", nil)

	var entry *logrus.Entry

	logger.Handler()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // nolint
		entry = logger.FromContext(r.Context())
		entry.Info("message here.")
	})).ServeHTTP(httptest.NewRecorder(), req)
	assert.NotNil(t, entry)
}
