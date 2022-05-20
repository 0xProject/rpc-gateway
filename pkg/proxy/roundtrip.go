package proxy

import (
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type RetryRoundTripHandler func(*http.Response) bool

type RetryRoundTripConfig struct {
	Retries int
	Delay   time.Duration
}

type RetryRoundTrip struct {
	Next    http.RoundTripper
	Config  RetryRoundTripConfig
	RetryOn RetryRoundTripHandler
}

func (rr *RetryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	var retries int

	for {
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()

		case <-time.After(rr.Config.Delay):
			continue

		default:
			resp, err := rr.Next.RoundTrip(r)
			retries++
			defer resp.Body.Close()

			if err != nil && retries == rr.Config.Retries {
				return resp, errors.Wrap(err, "max retries reached")
			}

			if rr.RetryOn != nil && rr.RetryOn(resp) {
				continue
			}

			return resp, err
		}
	}
}
