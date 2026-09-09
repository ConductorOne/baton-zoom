package zoom

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPaginationQuery(t *testing.T) {
	tests := []struct {
		name      string
		nextToken string
		want      string
	}{
		{
			name: "first page omits empty token",
			want: "page_size=50",
		},
		{
			name:      "next page includes encoded token",
			nextToken: "next+/=",
			want:      "next_page_token=next%2B%2F%3D&page_size=50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, paginationQuery(tt.nextToken).Encode())
		})
	}
}

func TestMapAuthenticationError(t *testing.T) {
	for _, code := range []string{"invalid_client", "invalid_grant"} {
		t.Run(code, func(t *testing.T) {
			oauthErr := &OAuthError{StatusCode: http.StatusBadRequest, Code: code, Reason: "authentication failed"}
			mapped := mapAuthenticationError(oauthErr)
			assert.Equal(t, codes.Unauthenticated, status.Code(mapped))

			var typedErr *OAuthError
			require.ErrorAs(t, mapped, &typedErr)
			assert.Same(t, oauthErr, typedErr)
		})
	}

	unauthorizedClient := errors.Join(
		status.Error(codes.InvalidArgument, "bad request"),
		&OAuthError{StatusCode: http.StatusBadRequest, Code: "unauthorized_client"},
	)
	assert.Same(t, unauthorizedClient, mapAuthenticationError(unauthorizedClient))
	assert.Equal(t, codes.InvalidArgument, status.Code(unauthorizedClient))

	genericBadRequest := status.Error(codes.InvalidArgument, "bad request")
	assert.Same(t, genericBadRequest, mapAuthenticationError(genericBadRequest))

	unsupportedGrant := errors.Join(
		genericBadRequest,
		&OAuthError{StatusCode: http.StatusBadRequest, Code: "unsupported_grant_type"},
	)
	assert.Same(t, unsupportedGrant, mapAuthenticationError(unsupportedGrant))
	assert.Equal(t, codes.InvalidArgument, status.Code(unsupportedGrant))

	unprocessableClient := errors.Join(
		status.Error(codes.InvalidArgument, "unprocessable"),
		&OAuthError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_client"},
	)
	assert.Same(t, unprocessableClient, mapAuthenticationError(unprocessableClient))
	assert.Equal(t, codes.InvalidArgument, status.Code(unprocessableClient))
}

func TestRequestAccessTokenErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode codes.Code
	}{
		{name: "invalid client", body: `{"error":"invalid_client","reason":"Invalid client credentials"}`, wantCode: codes.Unauthenticated},
		{name: "generic OAuth bad request", body: `{"error":"unsupported_grant_type","reason":"Unsupported grant type"}`, wantCode: codes.InvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("client-id:client-secret")), r.Header.Get("Authorization"))
				assert.Equal(t, "account-id", r.URL.Query().Get("account_id"))
				assert.Equal(t, "account_credentials", r.URL.Query().Get("grant_type"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			_, err := RequestAccessToken(t.Context(), "account-id", "client-id", "client-secret", srv.URL)
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, status.Code(err))

			var oauthErr *OAuthError
			require.ErrorAs(t, err, &oauthErr)
			assert.NotEmpty(t, oauthErr.Code)
		})
	}
}

func TestRequestAccessTokenPreservesTransientOAuthClassification(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "42")
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`{"error":"invalid_client","reason":"temporary upstream failure"}`))
			}))
			defer srv.Close()

			_, err := RequestAccessToken(t.Context(), "account-id", "client-id", "client-secret", srv.URL)
			require.Error(t, err)
			assert.Equal(t, codes.Unavailable, status.Code(err))

			var oauthErr *OAuthError
			require.ErrorAs(t, err, &oauthErr)
			assert.Equal(t, "invalid_client", oauthErr.Code)

			var description *v2.RateLimitDescription
			for _, detail := range status.Convert(err).Details() {
				if rateLimit, ok := detail.(*v2.RateLimitDescription); ok {
					description = rateLimit
				}
			}
			require.NotNil(t, description)
		})
	}
}

func TestRequestAccessTokenSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token"}`))
	}))
	defer srv.Close()

	token, err := RequestAccessToken(t.Context(), "account-id", "client-id", "client-secret", srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "token", token)
}
