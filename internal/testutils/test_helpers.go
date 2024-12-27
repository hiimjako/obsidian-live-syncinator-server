package testutils

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/hiimjako/syncinator/internal/migration"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func CreateDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(db))

	t.Cleanup(func() { db.Close() })

	return db
}

type requestOption func(req *http.Request) error

func WithAuthHeader(secretKey []byte, workspaceID int64) requestOption {
	return func(req *http.Request) error {
		token, err := middleware.CreateToken(middleware.AuthOptions{SecretKey: secretKey}, workspaceID)
		if err != nil {
			return err
		}
		req.Header.Add("Authorization", "Bearer "+token)
		return nil
	}
}

func WithContentTypeHeader(contentType string) requestOption {
	return func(req *http.Request) error {
		req.Header.Add("Content-Type", contentType)
		return nil
	}
}

type FileWithContent struct {
	Metadata repository.File
	Content  []byte
}

func DoRequest[T any](
	t *testing.T,
	server http.Handler,
	method string,
	filepath string,
	input any,
	options ...requestOption,
) (*httptest.ResponseRecorder, T) {
	var reqBody io.Reader

	if multipartInput, ok := input.(*bytes.Buffer); ok {
		reqBody = multipartInput
	} else {
		reqBodyBytes, err := json.Marshal(input)
		require.NoError(t, err)
		reqBody = bytes.NewBuffer(reqBodyBytes)
	}

	req := httptest.NewRequest(method, filepath, reqBody)
	for _, opt := range options {
		require.NoError(t, opt(req))
	}

	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var resBody T
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	contentType := res.Header().Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/mixed") {
		// Parse multipart/mixed response
		boundary := contentType[len("multipart/mixed; boundary="):]
		reader := multipart.NewReader(bytes.NewReader(body), boundary)

		// Decode multipart parts into FileWithContent
		var fileWithContent FileWithContent
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)

			contentType := part.Header.Get("Content-Type")
			partBody, err := io.ReadAll(part)
			require.NoError(t, err)

			switch contentType {
			case "application/json":
				err := json.Unmarshal(partBody, &fileWithContent.Metadata)
				require.NoError(t, err)
			default:
				fileWithContent.Content = partBody
			}
		}

		if result, ok := any(&resBody).(*FileWithContent); ok {
			*result = fileWithContent
		} else {
			require.Fail(t, "Unsupported type for multipart/mixed response decoding")
		}
	} else {
		// Handle non-multipart response
		if str, ok := any(&resBody).(*string); ok {
			if body == nil {
				*str = ""
			} else {
				*str = strings.Trim(string(body), "\n")
			}
		} else {
			err = json.Unmarshal(body, &resBody)
			assert.NoError(t, err)
		}
	}

	return res, resBody
}

func CreateMultipart(t *testing.T, filepath, content string) (*bytes.Buffer, string) {
	buf := &bytes.Buffer{}
	mpw := multipart.NewWriter(buf)

	filename := path.Base(filepath)
	fileWriter, err := mpw.CreateFormFile("file", filename)
	assert.NoError(t, err)
	_, err = fileWriter.Write([]byte(content))
	assert.NoError(t, err)

	pathWriter, err := mpw.CreateFormField("path")
	assert.NoError(t, err)
	_, err = pathWriter.Write([]byte(filepath))
	assert.NoError(t, err)

	assert.NoError(t, mpw.Close())

	return buf, mpw.FormDataContentType()
}
