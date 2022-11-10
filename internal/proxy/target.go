package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-http-utils/headers"
	"github.com/pkg/errors"
)

type HTTPTarget struct {
	Config        TargetConfig
	ClientOptions HTTPTargetClientOptions
}

type HTTPTargetClientOptions struct {
	Timeout time.Duration
}

func (h *HTTPTarget) initializeRequestWithContext(c context.Context, r *http.Request) (*http.Request, error) {
	// go doc http.NewRequestWithContext
	//
	// If body is of type *bytes.Buffer, *bytes.Reader, or
	// *strings.Reader, the returned request's ContentLength is set to its
	// exact value (instead of -1), GetBody is populated (so 307 and 308
	// redirects can replay the body), and Body is set to NoBody if the
	// ContentLength is 0.
	//
	body := &bytes.Buffer{}

	if _, err := io.Copy(body, r.Body); err != nil {
		return nil, errors.Wrap(err, "request copy failed")
	}

	req, err := http.NewRequestWithContext(c, http.MethodPost,
		h.Config.Connection.HTTP.URL, body)
	if err != nil {
		return nil, errors.Wrap(err, "request setup failed")
	}

	// If you don't specify header Content-Type, node will error out.
	//
	req.Header.Set(headers.ContentType, "application/json")

	if h.hasContentEncodingSettoGZIP(r) {
		req.Header.Set(headers.ContentEncoding, "gzip")
	}

	return req, err
}

func (h *HTTPTarget) hasContentEncodingSettoGZIP(r *http.Request) bool {
	contentEncoding := strings.ToLower(r.Header.Get(headers.ContentEncoding))
	if contentEncoding == "" {
		return false
	}

	return strings.Contains(contentEncoding, "gzip")
}

func (h *HTTPTarget) gunzip(r *http.Request) (*http.Request, error) {
	g, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "request gunzip failed")
	}

	body := &bytes.Buffer{}
	if _, err := io.Copy(body, g); err != nil { //nolint:gosec
		return nil, errors.Wrap(err, "request copy failed")
	}

	r.Body = io.NopCloser(body)
	r.ContentLength = int64(body.Len())

	if h.hasContentEncodingSettoGZIP(r) {
		r.Header.Del(headers.ContentEncoding)
	}

	return r, nil
}

func (h *HTTPTarget) gzip(r *http.Request) (*http.Request, error) {
	body := &bytes.Buffer{}

	if _, err := io.Copy(gzip.NewWriter(body), r.Body); err != nil {
		return nil, errors.Wrap(err, "request gzip failed")
	}

	r.Header.Set(headers.ContentEncoding, "gzip")
	r.ContentLength = int64(body.Len())
	r.Body = io.NopCloser(body)

	return r, nil
}

func (h *HTTPTarget) buildRequestWithContext(c context.Context, r *http.Request) (*http.Request, error) {
	req, err := h.initializeRequestWithContext(c, r)

	// Verify if request is compressed with gzip.
	//
	if h.hasContentEncodingSettoGZIP(r) {
		// Verify if node provider supports request compression.
		//
		if h.Config.Connection.HTTP.Compression {
			return req, err
		}

		// If node provider does not support request compression, we will
		// decompress the request before passing it through.
		return h.gunzip(req)
	}

	// Request is uncompressed, we check if node provider supports compression.
	//
	if h.Config.Connection.HTTP.Compression {
		// Compress the data before passing it through to a node provider.
		return h.gzip(req)
	}

	// All good, go plain.
	//
	return req, err
}

func (h *HTTPTarget) Do(c context.Context, r *http.Request) (*http.Response, error) {
	req, err := h.buildRequestWithContext(c, r)
	if err != nil {
		return nil, errors.Wrap(err, "request build failed")
	}

	client := &http.Client{
		Timeout: h.ClientOptions.Timeout,
	}

	return client.Do(req)
}
