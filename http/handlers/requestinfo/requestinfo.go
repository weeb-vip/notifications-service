package requestinfo

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey struct{}

func FromContext(ctx context.Context) RequestInfo {
	details, found := ctx.Value(&ctxKey{}).(RequestInfo)
	if !found {
		panic("handlers not set correctly")
	}

	return details
}

func Handler() func(http.Handler) http.Handler {
	return getHandler
}

func getHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := addRequestInfoToContext(request.Context(), getRequestInfoFromRequest(request))
		handler.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func getRequestInfoFromRequest(request *http.Request) RequestInfo {
	return RequestInfo{
		UserID:    getNillableHeaderValue(request, "x-user-id"),
		Purpose:   getNillableHeaderValue(request, "x-token-purpose"),
		RawToken:  getNillableHeaderValue(request, "x-raw-token"),
		UserType:  getUserType(getNillableHeaderValue(request, "x-user-id")),
		RemoteIP:  getNillableHeaderValue(request, "x-remote-ip"),
		UserAgent: getNillableHeaderValue(request, "x-user-agent"),
	}
}
func getUserType(userID *string) *UserType {
	if userID == nil {
		return nil
	}

	if strings.HasPrefix(*userID, "user_") {
		u := UserTypeUser

		return &u
	}

	if strings.HasPrefix(*userID, "guest_") {
		u := UserTypeGuest

		return &u
	}

	return nil
}
func getNillableHeaderValue(request *http.Request, headerKey string) *string {
	value := request.Header.Get(headerKey)
	if value == "" {
		return nil
	}

	return &value
}

func addRequestInfoToContext(ctx context.Context, details RequestInfo) context.Context {
	return context.WithValue(ctx, &ctxKey{}, details)
}
