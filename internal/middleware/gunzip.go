package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/go-http-utils/headers"
)

func Gunzip(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		// Skip if not gzip.
		//
		if !strings.Contains(r.Header.Get(headers.ContentEncoding), "gzip") {
			next.ServeHTTP(w, r)

			return
		}

		body := &bytes.Buffer{}

		g, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		if _, err := io.Copy(body, g); err != nil { // nolint:gosec
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		r.Header.Del(headers.ContentEncoding)
		r.Body = io.NopCloser(body)
		r.ContentLength = int64(body.Len())

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
