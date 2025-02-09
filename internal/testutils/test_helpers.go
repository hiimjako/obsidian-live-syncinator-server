package testutils

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path"
	"strings"
	"testing"

	"github.com/hiimjako/syncinator/internal/migration"
	"github.com/hiimjako/syncinator/internal/repository"
	"github.com/hiimjako/syncinator/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func CreateDB(t testing.TB) *sql.DB {
	// https://stackoverflow.com/a/77150429
	db, err := sql.Open("sqlite3", "file:memdb1?mode=memory&cache=shared")
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
			contentTranseferEncoding := part.Header.Get("Content-Transfer-Encoding")

			switch contentType {
			case "application/json":
				partBody, err := io.ReadAll(part)
				require.NoError(t, err)
				err = json.Unmarshal(partBody, &fileWithContent.Metadata)
				require.NoError(t, err)
			default:
				var partBody []byte
				if contentTranseferEncoding == "base64" {
					decoder := base64.NewDecoder(base64.StdEncoding, part)
					partBody, err = io.ReadAll(decoder)
					require.NoError(t, err)
				} else {
					partBody, err = io.ReadAll(part)
					require.NoError(t, err)
				}
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
			assert.NoError(t, err, "error: "+string(body))
		}
	}

	return res, resBody
}

func CreateMultipart(t *testing.T, filepath string, content []byte, encodeBase64 bool) (*bytes.Buffer, string) {
	buf := &bytes.Buffer{}
	mpw := multipart.NewWriter(buf)

	filename := path.Base(filepath)
	mimeHeader := textproto.MIMEHeader{
		"Content-Type":        []string{"application/octet-stream"},
		"Content-Disposition": []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, "file", filename)},
	}
	if encodeBase64 {
		mimeHeader["Content-Transfer-Encoding"] = []string{"base64"}
	}

	fileWriter, err := mpw.CreatePart(mimeHeader)
	require.NoError(t, err)

	if encodeBase64 {
		require.NoError(t, err)
		encoder := base64.NewEncoder(base64.StdEncoding, fileWriter)
		defer encoder.Close()
		_, err = encoder.Write(content)
		require.NoError(t, err)
	} else {
		_, err = fileWriter.Write(content)
		require.NoError(t, err)
	}

	pathWriter, err := mpw.CreateFormField("path")
	require.NoError(t, err)
	_, err = pathWriter.Write([]byte(filepath))
	require.NoError(t, err)

	require.NoError(t, mpw.Close())

	return buf, mpw.FormDataContentType()
}
