package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-http-utils/headers"
	"github.com/stretchr/testify/assert"
)

func TestGunzip(t *testing.T) {
	t.Parallel()

	ethChainID := `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`

	t.Run("compressed request", func(t *testing.T) {
		t.Parallel()

		tests := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			body := &strings.Builder{}

			nbytes, err := io.Copy(body, r.Body)
			assert.True(t, nbytes > 0)
			assert.Nil(t, err)

			assert.Equal(t, ethChainID, body.String())
			assert.Equal(t, int64(len(ethChainID)), r.ContentLength)
			assert.NotContains(t, r.Header.Get(headers.ContentEncoding), "gzip")
		})

		body := &bytes.Buffer{}
		w := gzip.NewWriter(body)

		nbytes, err := io.Copy(w, bytes.NewBufferString(ethChainID))

		assert.Nil(t, err)
		assert.True(t, nbytes > 0)
		assert.Nil(t, w.Close())

		request := httptest.NewRequest(http.MethodPost, "http://localhost", body)
		request.Header.Set(headers.ContentEncoding, "gzip")

		Gunzip(tests).
			ServeHTTP(httptest.NewRecorder(), request)
	})

	t.Run("uncompressed HTTP request", func(t *testing.T) {
		t.Parallel()

		tests := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			body := &strings.Builder{}

			nbytes, err := io.Copy(body, r.Body)
			assert.True(t, nbytes > 0)
			assert.Nil(t, err)

			assert.Equal(t, ethChainID, body.String())
			assert.Equal(t, int64(len(ethChainID)), r.ContentLength)
			assert.NotContains(t, r.Header.Get(headers.ContentEncoding), "gzip")
		})

		Gunzip(tests).
			ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPost, "http://localhost", bytes.NewBufferString(ethChainID)))
	})
}
