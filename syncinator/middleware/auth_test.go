package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAuthenticated(t *testing.T) {
	ao := AuthOptions{SecretKey: []byte("secret-key")}

	createToken := func(workspaceID int64) string {
		token, err := CreateToken(ao, workspaceID)
		require.NoError(t, err)
		require.NotEmpty(t, token)
		return token
	}

	runTest := func(
		t *testing.T,
		name, authHeader, authQuery string,
		expectedStatus int,
		expectedWorkspaceID int64,
		tokenExtractor JWTTokenExtractor,
	) {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if authHeader != "" {
				req.Header.Set("Authorization", authHeader)
			}
			if authQuery != "" {
				q := req.URL.Query()
				q.Add("jwt", authQuery)
				req.URL.RawQuery = q.Encode()
			}

			rec := httptest.NewRecorder()

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, expectedWorkspaceID, WorkspaceIDFromCtx(r.Context()))
				w.WriteHeader(http.StatusOK)
			})

			handler := IsAuthenticated(ao, tokenExtractor)(next)
			handler.ServeHTTP(rec, req)

			assert.Equal(t, expectedStatus, rec.Code)
		})
	}

	// Test cases
	tests := []struct {
		name                string
		authHeader          string
		authQuery           string
		expectedStatus      int
		expectedWorkspaceID int64
		tokenExtractor      JWTTokenExtractor
	}{
		{"No Auth Header (Bearer)", "", "", http.StatusUnauthorized, 0, ExtractBearerToken},
		{"Invalid Token (Bearer)", "Bearer invalidToken", "", http.StatusUnauthorized, 0, ExtractBearerToken},
		{"Valid Token (Bearer)", "Bearer " + createToken(123), "", http.StatusOK, 123, ExtractBearerToken},

		{"No Auth Header (WS)", "", "", http.StatusUnauthorized, 0, ExtractWsToken},
		{"Invalid Token (WS)", "", "invalidToken", http.StatusUnauthorized, 0, ExtractWsToken},
		{"Valid Token (WS)", "", createToken(123), http.StatusOK, 123, ExtractWsToken},
	}

	// Execute all tests
	for _, tt := range tests {
		runTest(t, tt.name, tt.authHeader, tt.authQuery, tt.expectedStatus, tt.expectedWorkspaceID, tt.tokenExtractor)
	}
}

func TestWorkspaceIDFromCtx(t *testing.T) {
	expectedWorkspaceID := int64(10)
	ctx := context.WithValue(context.Background(), AuthWorkspaceID, int64(10))
	workspaceID := WorkspaceIDFromCtx(ctx)

	assert.Equal(t, expectedWorkspaceID, workspaceID)
}
