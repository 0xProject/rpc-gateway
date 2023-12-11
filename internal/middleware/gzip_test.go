package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-http-utils/headers"
	"github.com/stretchr/testify/assert"
)

func TestGzip(t *testing.T) {
	t.Parallel()

	ethChainID := `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`
	t.Run("compressed HTTP request", func(t *testing.T) {
		t.Parallel()

		tests := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			body := &bytes.Buffer{}

			g := gzip.NewWriter(body)
			nbytes, err := io.Copy(g, bytes.NewBufferString(ethChainID))
			assert.Nil(t, err)
			assert.True(t, nbytes > 0)
			assert.Nil(t, g.Close())

			assert.Equal(t, int64(body.Len()), r.ContentLength)
			assert.Equal(t, io.NopCloser(body), r.Body)
			assert.Contains(t, r.Header.Get(headers.ContentEncoding), "gzip")
		})

		Gzip(tests).
			ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPost, "http://localhost", bytes.NewBufferString(ethChainID)),
			)
	})

	t.Run("uncompressed HTTP request", func(t *testing.T) {
		t.Parallel()

		tests := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			body := &bytes.Buffer{}

			g, err := gzip.NewReader(r.Body)
			assert.Nil(t, err)

			nbytes, err := io.Copy(body, g) // nolint:gosec
			assert.Nil(t, err)
			assert.True(t, nbytes > 0)
			assert.Nil(t, g.Close())

			assert.Equal(t, ethChainID, body.String())
			assert.Contains(t, r.Header.Get(headers.ContentEncoding), "gzip")
		})

		Gzip(tests).
			ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPost, "http://localhost", bytes.NewBufferString(ethChainID)),
			)
	})
}
