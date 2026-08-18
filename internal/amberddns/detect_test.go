package amberddns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParsePublicV4 pins the guard that decides what may be published to the
// world's DNS.  Every rejected case below is an address a reflector can
// plausibly return and that would be wrong, useless, or harmful as an A record.
func TestParsePublicV4(t *testing.T) {
	testCases := []struct {
		name       string
		in         string
		wantAddr   string
		wantErrSub string
	}{{
		name:     "public",
		in:       "203.0.113.7",
		wantAddr: "203.0.113.7",
	}, {
		name:     "trailing newline",
		in:       "203.0.113.7\n",
		wantAddr: "203.0.113.7",
	}, {
		name:     "v4 mapped in v6",
		in:       "::ffff:203.0.113.7",
		wantAddr: "203.0.113.7",
	}, {
		name:       "rfc1918",
		in:         "192.168.0.4",
		wantErrSub: "RFC 1918",
	}, {
		name:       "rfc1918 ten",
		in:         "10.1.2.3",
		wantErrSub: "RFC 1918",
	}, {
		name:       "carrier grade nat",
		in:         "100.64.1.1",
		wantErrSub: "carrier-grade NAT",
	}, {
		name:       "loopback",
		in:         "127.0.0.1",
		wantErrSub: "loopback",
	}, {
		name:       "link local",
		in:         "169.254.1.1",
		wantErrSub: "link-local",
	}, {
		name:       "unspecified",
		in:         "0.0.0.0",
		wantErrSub: "unspecified",
	}, {
		name:       "reserved",
		in:         "240.0.0.1",
		wantErrSub: "reserved",
	}, {
		name:       "ipv6",
		in:         "2001:db8::1",
		wantErrSub: "not IPv4",
	}, {
		name:       "html error page",
		in:         "<html>error</html>",
		wantErrSub: "parsing",
	}, {
		name:       "empty",
		in:         "",
		wantErrSub: "parsing",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := ParsePublicV4(tc.in)
			if tc.wantErrSub != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAddr, addr.String())
		})
	}
}

// echoServer returns a reflector answering with body, and a counter of hits.
func echoServer(t *testing.T, body string, status int) (u string, hits *atomic.Int64) {
	t.Helper()

	hits = &atomic.Int64{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv.URL, hits
}

// TestDetector_fallsThroughFailures covers the reason there is more than one
// reflector: a dead one, and one answering junk, must not stop detection.
func TestDetector_fallsThroughFailures(t *testing.T) {
	dead, deadHits := echoServer(t, "", http.StatusInternalServerError)
	junk, junkHits := echoServer(t, "not an address", http.StatusOK)
	good, goodHits := echoServer(t, "203.0.113.7\n", http.StatusOK)

	d := NewDetector(http.DefaultClient, []string{dead, junk, good})

	addr, source, err := d.Detect(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", addr.String())
	assert.Equal(t, good, source)
	assert.Equal(t, int64(1), deadHits.Load())
	assert.Equal(t, int64(1), junkHits.Load())
	assert.Equal(t, int64(1), goodHits.Load())
}

// TestDetector_privateAnswerIsNotTrusted covers a reflector behind a proxy
// answering with a LAN address.  Falling through to the next one is right;
// publishing it is not.
func TestDetector_privateAnswerIsNotTrusted(t *testing.T) {
	lan, _ := echoServer(t, "192.168.0.4", http.StatusOK)
	good, _ := echoServer(t, "203.0.113.7", http.StatusOK)

	d := NewDetector(http.DefaultClient, []string{lan, good})

	addr, source, err := d.Detect(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", addr.String())
	assert.Equal(t, good, source)
}

func TestDetector_allFail(t *testing.T) {
	a, _ := echoServer(t, "", http.StatusInternalServerError)
	b, _ := echoServer(t, "192.168.0.4", http.StatusOK)

	d := NewDetector(http.DefaultClient, []string{a, b})

	_, _, err := d.Detect(context.Background())
	require.Error(t, err)

	// Both reasons are reported, not just the last: "everything is down" and
	// "one service is lying" are different repairs.
	assert.Contains(t, err.Error(), "http 500")
	assert.Contains(t, err.Error(), "RFC 1918")
}

// TestDetector_DetectExcluding is what makes the second opinion independent.
// Asking the same reflector twice would confirm nothing at all.
func TestDetector_DetectExcluding(t *testing.T) {
	first, firstHits := echoServer(t, "203.0.113.7", http.StatusOK)
	second, secondHits := echoServer(t, "203.0.113.9", http.StatusOK)

	d := NewDetector(http.DefaultClient, []string{first, second})

	addr, source, err := d.DetectExcluding(context.Background(), first)
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.9", addr.String())
	assert.Equal(t, second, source)
	assert.Equal(t, int64(0), firstHits.Load())
	assert.Equal(t, int64(1), secondHits.Load())
}

func TestDetector_DetectExcluding_noneLeft(t *testing.T) {
	only, _ := echoServer(t, "203.0.113.7", http.StatusOK)

	d := NewDetector(http.DefaultClient, []string{only})

	_, _, err := d.DetectExcluding(context.Background(), only)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "no reflector available")
}
