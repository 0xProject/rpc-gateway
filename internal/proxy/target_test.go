package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/iotest"
	"time"

	"github.com/go-http-utils/headers"
	"github.com/stretchr/testify/assert"
)

func doGzip(t *testing.T, payload []byte) []byte {
	body := &bytes.Buffer{}

	n, err := io.Copy(gzip.NewWriter(body), bytes.NewReader(payload))

	assert.NotZero(t, n)
	assert.NoError(t, err)

	return body.Bytes()
}

var ethChainID = []byte(`{"id":2, "method":"eth_chainId", "params":[]}`)

// Check a scenario when client sends uncompress request and node provider
// supports request compression.
func TestClientWithoutGzipAndNodeProviderWithGzip(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header, headers.ContentLength)
			assert.Contains(t, r.Header.Get(headers.ContentType), "application/json")
			assert.Contains(t, r.Header.Get(headers.ContentEncoding), "gzip")
			assert.NoError(t, iotest.TestReader(r.Body, doGzip(t, ethChainID)))

			rw.WriteHeader(http.StatusOK)
		}))
	defer node.Close()

	target := HTTPTarget{
		Config: TargetConfig{
			Name: "Primary",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL:         node.URL,
					Compression: true,
				},
			},
		},
		ClientOptions: HTTPTargetClientOptions{
			Timeout: 10 * time.Second,
		},
	}

	request, err := http.NewRequest(http.MethodPost,
		target.Config.Connection.HTTP.URL, bytes.NewReader(ethChainID))

	assert.NoError(t, err)

	response, err := target.Do(context.TODO(), request)

	assert.NoError(t, err)
	assert.Equal(t, response.StatusCode, http.StatusOK)
	assert.NoError(t, response.Body.Close())
}

// Check a scenario when client sends compressed request and node provider
// supports request compression.
func TestClientWithGzipAndNodeProviderWithGzip(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header, headers.ContentLength)
			assert.Contains(t, r.Header.Get(headers.ContentType), "application/json")
			assert.Contains(t, r.Header.Get(headers.ContentEncoding), "gzip")
			assert.NoError(t, iotest.TestReader(r.Body, doGzip(t, ethChainID)))

			rw.WriteHeader(http.StatusOK)
		}))
	defer node.Close()

	target := HTTPTarget{
		Config: TargetConfig{
			Name: "Primary",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL:         node.URL,
					Compression: true,
				},
			},
		},
		ClientOptions: HTTPTargetClientOptions{
			Timeout: 10 * time.Second,
		},
	}

	request, err := http.NewRequest(http.MethodPost,
		target.Config.Connection.HTTP.URL, bytes.NewReader(doGzip(t, ethChainID)))

	assert.NoError(t, err)

	response, err := target.Do(context.TODO(), request)

	assert.NoError(t, err)
	assert.Equal(t, response.StatusCode, http.StatusOK)
	assert.NoError(t, response.Body.Close())
}

// Check a scenario when client sends uncompress request and node provider
// does not support compression.
func TestClientWithoutGzipAndNodeProviderWithoutGzip(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header, headers.ContentLength)
			assert.Contains(t, r.Header.Get(headers.ContentType), "application/json")
			assert.NotContains(t, r.Header.Get(headers.ContentEncoding), "gzip")
			assert.NoError(t, iotest.TestReader(r.Body, ethChainID))

			rw.WriteHeader(http.StatusOK)
		}))
	defer node.Close()

	target := HTTPTarget{
		Config: TargetConfig{
			Name: "Primary",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL:         node.URL,
					Compression: false,
				},
			},
		},
		ClientOptions: HTTPTargetClientOptions{
			Timeout: 10 * time.Second,
		},
	}

	request, err := http.NewRequest(http.MethodPost,
		target.Config.Connection.HTTP.URL, bytes.NewReader(ethChainID))

	assert.NoError(t, err)

	response, err := target.Do(context.TODO(), request)

	assert.NoError(t, err)
	assert.Equal(t, response.StatusCode, http.StatusOK)
	assert.NoError(t, response.Body.Close())
}

// Check a scenario when client sends compressed request and node provider
// does not support request compression.
func TestClientWithGzipAndNodeProviderWithoutGzip(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(
		http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header, headers.ContentLength)
			assert.Contains(t, r.Header.Get(headers.ContentType), "application/json")
			assert.NotContains(t, r.Header.Get(headers.ContentEncoding), "gzip")
			assert.NoError(t, iotest.TestReader(r.Body, ethChainID))

			rw.WriteHeader(http.StatusOK)
		}))
	defer node.Close()

	target := HTTPTarget{
		Config: TargetConfig{
			Name: "Primary",
			Connection: TargetConfigConnection{
				HTTP: TargetConnectionHTTP{
					URL:         node.URL,
					Compression: false,
				},
			},
		},
		ClientOptions: HTTPTargetClientOptions{
			Timeout: 10 * time.Second,
		},
	}

	request, err := http.NewRequest(http.MethodPost,
		target.Config.Connection.HTTP.URL, bytes.NewReader(ethChainID))

	assert.NoError(t, err)

	response, err := target.Do(context.TODO(), request)

	assert.NoError(t, err)
	assert.Equal(t, response.StatusCode, http.StatusOK)
	assert.NoError(t, response.Body.Close())
}
