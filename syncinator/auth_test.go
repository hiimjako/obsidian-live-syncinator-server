package syncinator

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"testing"

	"github.com/hiimjako/syncinator/internal/migration"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/internal/testutils"
	"github.com/hiimjako/syncinator/syncinator/filestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

func Test_fetchWorkspaceHandler(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(db))

	repo := repository.New(db)
	mockFileStorage := new(filestorage.MockFileStorage)
	server := New(repo, mockFileStorage, Options{JWTSecret: []byte("secret")})

	hash, err := bcrypt.GenerateFromPassword([]byte("strong_password"), bcrypt.DefaultCost)
	require.NoError(t, err)

	require.NoError(t, repo.AddWorkspace(context.Background(), repository.AddWorkspaceParams{
		Name:     "workspace1",
		Password: string(hash),
	}))

	const apiPath = PathHttpAuth + "/login"

	t.Run("existing workspace", func(t *testing.T) {
		data := WorkspaceCredentials{
			Name:     "workspace1",
			Password: "strong_password",
		}

		res, body := testutils.DoRequest[LoginResponse](t, server, http.MethodPost, apiPath, data)

		assert.Equal(t, http.StatusOK, res.Code)
		jwtRegex := `^[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+$`
		matched, err := regexp.MatchString(jwtRegex, body.Token)
		assert.NoError(t, err)
		assert.True(t, matched)
	})

	t.Run("wrong password", func(t *testing.T) {
		data := WorkspaceCredentials{
			Name:     "workspace1",
			Password: "invalid_pass",
		}
		res, body := testutils.DoRequest[string](t, server, http.MethodPost, apiPath, data)
		assert.Equal(t, http.StatusUnauthorized, res.Code)
		assert.Equal(t, ErrIncorrectPassword, body)
	})

	t.Run("missing workspace", func(t *testing.T) {
		data := WorkspaceCredentials{
			Name:     "workspace2",
			Password: "random",
		}

		res, body := testutils.DoRequest[string](t, server, http.MethodPost, apiPath, data)
		assert.Equal(t, http.StatusNotFound, res.Code)
		assert.Equal(t, ErrWorkspaceNotFound, body)
	})

	t.Cleanup(func() {
		server.Close()
		db.Close()
	})
}
