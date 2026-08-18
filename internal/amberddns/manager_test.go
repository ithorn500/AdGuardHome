package amberddns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateCall is one request the stub provider received.
type updateCall struct {
	domain string
	host   string
	ip     string
}

// stubProvider is a controllable stand-in for Namecheap's update endpoint.
type stubProvider struct {
	// failFor maps "domain/host" to a provider rejection message.
	failFor map[string]string

	// mu protects calls and failFor.
	mu sync.Mutex

	// calls records every update received, in order.
	calls []updateCall

	// httpFail makes the endpoint answer 500 instead of a provider reply,
	// which is the transient-failure path rather than the rejection path.
	httpFail bool
}

// start returns a running stub and its URL.
func (s *stubProvider) start(t *testing.T) (u string) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		call := updateCall{domain: q.Get("domain"), host: q.Get("host"), ip: q.Get("ip")}

		s.mu.Lock()
		s.calls = append(s.calls, call)
		httpFail := s.httpFail
		msg := s.failFor[call.domain+"/"+call.host]
		s.mu.Unlock()

		if httpFail {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if msg != "" {
			_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-16"?>
<interface-response><ErrCount>1</ErrCount><errors><Err1>%s</Err1></errors>
<responses><response><Description>%s</Description><ResponseNumber>304156</ResponseNumber></response></responses>
<Done>true</Done></interface-response>`, msg, msg)

			return
		}

		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-16"?>
<interface-response><IP>%s</IP><ErrCount>0</ErrCount><Done>true</Done></interface-response>`, call.ip)
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// callsFor returns the addresses published for one domain and host.
func (s *stubProvider) callsFor(domain, host string) (ips []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.calls {
		if c.domain == domain && c.host == host {
			ips = append(ips, c.ip)
		}
	}

	return ips
}

// callCount returns how many updates the provider received.
func (s *stubProvider) callCount() (n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.calls)
}

// mutableEcho returns a reflector whose answer can be changed during a test.
func mutableEcho(t *testing.T, initial string) (u string, set func(v string)) {
	t.Helper()

	var (
		mu  sync.Mutex
		val = initial
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		v := val
		mu.Unlock()

		_, _ = w.Write([]byte(v))
	}))
	t.Cleanup(srv.Close)

	return srv.URL, func(v string) {
		mu.Lock()
		val = v
		mu.Unlock()
	}
}

// testManager builds a manager wired to stubs, with a controllable clock.
func testManager(
	t *testing.T,
	echoURLs []string,
	endpoint string,
	hosts []string,
) (m *Manager, clock *fakeClock) {
	t.Helper()

	clock = &fakeClock{now: time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)}

	conf := &Config{
		Enabled:              true,
		CheckInterval:        5 * time.Minute,
		ForceRefreshInterval: 24 * time.Hour,
		EchoURLs:             echoURLs,
		Endpoint:             endpoint,
		Domains: []*DomainConfig{{
			Domain:   "example.com",
			Password: "s3cret",
			Hosts:    hosts,
		}},
	}
	require.NoError(t, conf.validate())

	m = NewManager(conf, http.DefaultClient, slogutil.NewDiscardLogger())
	m.now = clock.Now

	return m, clock
}

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now returns the current fake time.
func (c *fakeClock) Now() (t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// advance moves the clock forward.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func TestManager_publishesOnFirstCheck(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@", "www"})

	next := m.checkOnce(context.Background())
	assert.Equal(t, 5*time.Minute, next)

	assert.Equal(t, []string{"203.0.113.7"}, prov.callsFor("example.com", "@"))
	assert.Equal(t, []string{"203.0.113.7"}, prov.callsFor("example.com", "www"))

	s := m.Snapshot()
	assert.Equal(t, "203.0.113.7", s.CurrentIP)
	assert.Zero(t, s.ConsecutiveFailures)
	assert.Empty(t, s.LastError)

	for _, rec := range s.Records {
		assert.Equal(t, "203.0.113.7", rec.PublishedIP)
	}
}

// TestManager_steadyStateDoesNotRepublish is the point of the whole change
// detector: a provider must not be called every five minutes to be told what
// it already knows.
func TestManager_steadyStateDoesNotRepublish(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, clock := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	m.checkOnce(context.Background())
	require.Equal(t, 1, prov.callCount())

	for range 10 {
		clock.advance(5 * time.Minute)
		m.checkOnce(context.Background())
	}

	assert.Equal(t, 1, prov.callCount())
}

// TestManager_forceRefreshRepublishesUnchangedAddress covers the self-healing
// case: the record was changed behind our back, so an unchanged address must
// still be re-asserted eventually.
func TestManager_forceRefreshRepublishesUnchangedAddress(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, clock := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	m.checkOnce(context.Background())
	require.Equal(t, 1, prov.callCount())

	// Just short of the window: still silent.
	clock.advance(23 * time.Hour)
	m.checkOnce(context.Background())
	assert.Equal(t, 1, prov.callCount())

	// Past it: re-asserted.
	clock.advance(2 * time.Hour)
	m.checkOnce(context.Background())
	assert.Equal(t, 2, prov.callCount())
}

// TestManager_changeRequiresIndependentConfirmation is the guard against a
// single reflector that has started answering with somebody else's address.
// Acting on one opinion is how a domain gets pointed at a stranger.
func TestManager_changeRequiresIndependentConfirmation(t *testing.T) {
	first, setFirst := mutableEcho(t, "203.0.113.7")
	second, setSecond := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, _ := testManager(t, []string{first, second}, prov.start(t), []string{"@"})

	m.checkOnce(context.Background())
	require.Equal(t, 1, prov.callCount())

	// Only the first reflector changes its answer.  They disagree, so nothing
	// is published and the state records why.
	setFirst("198.51.100.9")

	next := m.checkOnce(context.Background())
	assert.Equal(t, 1, prov.callCount(), "must not publish on an unconfirmed change")
	assert.Greater(t, next, time.Duration(0))

	s := m.Snapshot()
	assert.Contains(t, s.LastError, "disagree")
	assert.Equal(t, "203.0.113.7", s.CurrentIP, "stored address must not move on a disagreement")

	// Once the second agrees, the change is real and gets published.
	setSecond("198.51.100.9")

	m.checkOnce(context.Background())
	assert.Equal(t, 2, prov.callCount())
	assert.Equal(t, []string{"203.0.113.7", "198.51.100.9"}, prov.callsFor("example.com", "@"))

	s = m.Snapshot()
	assert.Equal(t, "198.51.100.9", s.CurrentIP)
	assert.Empty(t, s.LastError)
}

// TestManager_partialFailureRetriesOnlyTheFailedRecord is why publication
// state is per record.  A shared "last published" would either re-send every
// host or, worse, mark the whole domain current and never retry the host that
// failed.
func TestManager_partialFailureRetriesOnlyTheFailedRecord(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{failFor: map[string]string{"example.com/www": "Passwords do not match"}}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@", "www"})

	m.checkOnce(context.Background())

	assert.Len(t, prov.callsFor("example.com", "@"), 1)
	assert.Len(t, prov.callsFor("example.com", "www"), 1)

	s := m.Snapshot()
	require.Len(t, s.Records, 2)
	assert.Equal(t, "203.0.113.7", s.Records[0].PublishedIP)
	assert.Empty(t, s.Records[0].LastError)
	assert.Empty(t, s.Records[1].PublishedIP)
	assert.Contains(t, s.Records[1].LastError, "Passwords do not match")

	// Next cycle: the good record is left alone, the failed one is retried.
	m.checkOnce(context.Background())

	assert.Len(t, prov.callsFor("example.com", "@"), 1, "successful record must not be re-sent")
	assert.Len(t, prov.callsFor("example.com", "www"), 2, "failed record must be retried")

	// And once it starts working, it recovers without operator action.
	prov.mu.Lock()
	prov.failFor = nil
	prov.mu.Unlock()

	m.checkOnce(context.Background())

	s = m.Snapshot()
	assert.Equal(t, "203.0.113.7", s.Records[1].PublishedIP)
	assert.Empty(t, s.Records[1].LastError)
	assert.Zero(t, s.ConsecutiveFailures)
}

// TestManager_providerRejectionBacksOffHarderThanNetworkFailure pins the retry
// policy.  A rejected password is a configuration fault: it will be rejected
// again in five minutes, so hammering it just fills the log and the provider's
// rate limiter.
func TestManager_providerRejectionBacksOffHarderThanNetworkFailure(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{failFor: map[string]string{"example.com/@": "Passwords do not match"}}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	next := m.checkOnce(context.Background())
	assert.GreaterOrEqual(t, next, providerErrorFloor)

	// A transport-level failure is transient and retried on the normal cadence.
	prov2 := &stubProvider{httpFail: true}
	m2, _ := testManager(t, []string{echo}, prov2.start(t), []string{"@"})

	next2 := m2.checkOnce(context.Background())
	assert.Equal(t, m2.conf.CheckInterval, next2)
	assert.Less(t, next2, providerErrorFloor)
}

func TestManager_backoffGrowsAndIsCapped(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{httpFail: true}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	var last time.Duration
	for range 12 {
		last = m.checkOnce(context.Background())
	}

	assert.Equal(t, maxTransientBackoff, last)
	assert.Greater(t, m.Snapshot().ConsecutiveFailures, 1)
}

// TestManager_detectionFailurePublishesNothing covers the reflectors being
// down.  Not knowing the address is not a reason to write one.
func TestManager_detectionFailurePublishesNothing(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})).URL
	prov := &stubProvider{}

	m, _ := testManager(t, []string{dead}, prov.start(t), []string{"@"})

	m.checkOnce(context.Background())

	assert.Zero(t, prov.callCount())
	assert.Contains(t, m.Snapshot().LastError, "detecting public address")
}

// TestManager_privateAddressIsNeverPublished is the last line of defence
// against putting a LAN address into public DNS.
func TestManager_privateAddressIsNeverPublished(t *testing.T) {
	echo, _ := mutableEcho(t, "192.168.0.4")
	prov := &stubProvider{}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	m.checkOnce(context.Background())

	assert.Zero(t, prov.callCount())
	assert.Contains(t, m.Snapshot().LastError, "RFC 1918")
}

func TestManager_disabledStartIsANoOp(t *testing.T) {
	m := NewManager(&Config{Enabled: false}, http.DefaultClient, slogutil.NewDiscardLogger())

	m.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, m.Shutdown(ctx))
	assert.False(t, m.Snapshot().Enabled)
}

func TestManager_startAndShutdown(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@"})

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)

	require.Eventually(t, func() (ok bool) {
		return prov.callCount() > 0
	}, 5*time.Second, 10*time.Millisecond)

	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()

	require.NoError(t, m.Shutdown(shutCtx))
}

func TestManager_snapshotIsACopy(t *testing.T) {
	echo, _ := mutableEcho(t, "203.0.113.7")
	prov := &stubProvider{}

	m, _ := testManager(t, []string{echo}, prov.start(t), []string{"@"})
	m.checkOnce(context.Background())

	s := m.Snapshot()
	s.Records[0].PublishedIP = "tampered"

	assert.Equal(t, "203.0.113.7", m.Snapshot().Records[0].PublishedIP)
}
