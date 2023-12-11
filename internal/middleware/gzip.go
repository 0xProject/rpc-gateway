package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/go-http-utils/headers"
)

func Gzip(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		// Skip if compressed.
		if strings.Contains(r.Header.Get(headers.ContentEncoding), "gzip") {
			next.ServeHTTP(w, r)

			return
		}

		body := &bytes.Buffer{}
		g := gzip.NewWriter(body)

		if _, err := io.Copy(g, r.Body); err != nil {
			http.Error(w,
				http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		if err := g.Close(); err != nil {
			http.Error(w,
				http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		r.Header.Set(headers.ContentEncoding, "gzip")
		r.Body = io.NopCloser(body)
		r.ContentLength = int64(body.Len())

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
