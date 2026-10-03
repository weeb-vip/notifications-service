package requestinfo_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weeb-vip/notifications-service/http/handlers/requestinfo"

	"github.com/stretchr/testify/assert"
)

func TestFromContext(t *testing.T) {
	t.Run("panics if there no request info in context", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/graphql", nil)
		assert.PanicsWithValue(t, "handlers not set correctly", func() {
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestinfo.FromContext(request.Context())
			}).ServeHTTP(httptest.NewRecorder(), req)
		})
	})
}
func TestHandler(t *testing.T) {
	t.Run("adds request information onto request context", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/graphql", nil)
		req.Header.Add("x-user-agent", "user_agent")
		req.Header.Add("x-remote-ip", "192.168.1.1")
		req.Header.Add("x-user-id", "user_something")
		req.Header.Add("x-token-purpose", "purpose")
		req.Header.Add("x-raw-token", "raw-token")

		f := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			info := requestinfo.FromContext(request.Context())
			assert.NotNil(t, info)
			assert.Equal(t, "user_agent", *info.UserAgent)
			assert.Equal(t, "192.168.1.1", *info.RemoteIP)
			assert.Equal(t, "user_something", *info.UserID)
			assert.Equal(t, "purpose", *info.Purpose)
			assert.Equal(t, "raw-token", *info.RawToken)
			assert.Equal(t, requestinfo.UserTypeUser, *info.UserType)
		})
		requestinfo.Handler()(f).ServeHTTP(httptest.NewRecorder(), req)
	})

	t.Run("sets user type correctly", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/graphql", nil)
		req.Header.Add("x-user-id", "guest_something")
		req.Header.Add("x-token-purpose", "purpose")
		req.Header.Add("x-raw-token", "raw-token")

		f := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			info := requestinfo.FromContext(request.Context())
			assert.NotNil(t, info)
			assert.Equal(t, requestinfo.UserTypeGuest, *info.UserType)
		})
		requestinfo.Handler()(f).ServeHTTP(httptest.NewRecorder(), req)
	})
	t.Run("if the user is not set, user_type is nil", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/graphql", nil)

		f := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			info := requestinfo.FromContext(request.Context())
			assert.NotNil(t, info)
			assert.Nil(t, info.UserType)
		})
		requestinfo.Handler()(f).ServeHTTP(httptest.NewRecorder(), req)
	})
	t.Run("if the user id is of unknown type, user_type is nil", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/graphql", nil)
		req.Header.Add("x-user-id", "session_something")
		req.Header.Add("x-token-purpose", "purpose")
		req.Header.Add("x-raw-token", "raw-token")

		f := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			info := requestinfo.FromContext(request.Context())
			assert.NotNil(t, info)
			assert.Nil(t, info.UserType)
		})
		requestinfo.Handler()(f).ServeHTTP(httptest.NewRecorder(), req)
	})
}
