package amberddns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// Backoff bounds.
const (
	// maxTransientBackoff caps the delay after network-level failures.
	maxTransientBackoff = time.Hour

	// providerErrorFloor is the minimum delay after the provider itself
	// rejects an update.  A rejection is nearly always a configuration fault —
	// wrong password, wrong domain, wrong host — and configuration does not
	// fix itself in sixty seconds.  Retrying at the normal interval would turn
	// one mistake into a permanent stream of failed authentications.
	providerErrorFloor = 15 * time.Minute

	// maxProviderBackoff caps the delay after provider rejections.
	maxProviderBackoff = 6 * time.Hour
)

// Manager keeps configured host records pointed at this host's current public
// IPv4 address.
type Manager struct {
	conf     *Config
	detector *Detector
	client   *NamecheapClient
	logger   *slog.Logger

	// now returns the current time, indirected so tests control the clock.
	now func() (t time.Time)

	// refreshCh requests an immediate check, coalescing multiple requests.
	refreshCh chan struct{}

	// doneCh is closed when the run loop has exited.
	doneCh chan struct{}

	// mu protects state.
	mu sync.Mutex

	// state is the snapshot served to operators and to the bus.
	state State
}

// State is the manager's externally visible state.
type State struct {
	// LastDetectedAt is when the public address was last successfully
	// detected.
	LastDetectedAt time.Time `json:"last_detected_at,omitzero"`

	// LastErrorAt is when LastError was recorded.
	LastErrorAt time.Time `json:"last_error_at,omitzero"`

	// CurrentIP is the most recently detected public address.
	CurrentIP string `json:"current_ip,omitempty"`

	// DetectedVia is the reflector that reported CurrentIP.
	DetectedVia string `json:"detected_via,omitempty"`

	// LastError is the most recent failure, or "".
	LastError string `json:"last_error,omitempty"`

	// Records is the per-record publication state.
	Records []RecordState `json:"records"`

	// ConsecutiveFailures counts checks that have failed in a row.
	ConsecutiveFailures int `json:"consecutive_failures"`

	// Enabled reports whether the manager is running.
	Enabled bool `json:"enabled"`
}

// RecordState is the publication state of one host record.
type RecordState struct {
	// LastPublishAt is when this record was last successfully published.
	LastPublishAt time.Time `json:"last_publish_at,omitzero"`

	// Domain is the registered domain.
	Domain string `json:"domain"`

	// Host is the host label within Domain.
	Host string `json:"host"`

	// PublishedIP is the address this record is believed to hold.  It is only
	// set from a confirmed successful update, never from an assumption.
	PublishedIP string `json:"published_ip,omitempty"`

	// LastError is the most recent failure for this record, or "".
	LastError string `json:"last_error,omitempty"`
}

// NewManager returns a manager for conf.  httpClient is used for both address
// detection and provider updates and must not be nil.  logger must not be nil.
func NewManager(conf *Config, httpClient *http.Client, logger *slog.Logger) (m *Manager) {
	return &Manager{
		conf:      conf,
		detector:  NewDetector(httpClient, conf.EchoURLs),
		client:    NewNamecheapClient(httpClient, conf.Endpoint),
		logger:    logger,
		now:       time.Now,
		refreshCh: make(chan struct{}, 1),
		doneCh:    make(chan struct{}),
		state: State{
			Enabled: conf.Enabled,
			Records: initialRecords(conf),
		},
	}
}

// initialRecords builds the record list so that state is complete and
// inspectable before the first check runs, rather than materialising as
// updates happen.
func initialRecords(conf *Config) (recs []RecordState) {
	for _, d := range conf.Domains {
		for _, h := range d.Hosts {
			recs = append(recs, RecordState{
				Domain: d.Domain,
				Host:   h,
			})
		}
	}

	return recs
}

// Start runs the manager until ctx is cancelled.  It returns immediately; the
// work happens in a goroutine.  Starting a disabled manager is a no-op.
func (m *Manager) Start(ctx context.Context) {
	if !m.conf.Enabled {
		m.logger.DebugContext(ctx, "dynamic dns is disabled")
		close(m.doneCh)

		return
	}

	m.logger.InfoContext(
		ctx,
		"starting dynamic dns manager",
		"records", len(m.state.Records),
		"check_interval", m.conf.CheckInterval,
		"force_refresh_interval", m.conf.ForceRefreshInterval,
	)

	go m.run(ctx)
}

// Shutdown waits for the run loop to exit, or for ctx to be cancelled.
func (m *Manager) Shutdown(ctx context.Context) (err error) {
	select {
	case <-m.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Refresh requests an immediate check.  It never blocks: if a check is already
// pending, the request is folded into it.
func (m *Manager) Refresh() {
	select {
	case m.refreshCh <- struct{}{}:
	default:
	}
}

// Snapshot returns a copy of the manager's current state, safe to serialise
// while the loop is running.
func (m *Manager) Snapshot() (s State) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s = m.state
	s.Records = make([]RecordState, len(m.state.Records))
	copy(s.Records, m.state.Records)

	return s
}

// run is the manager's loop.
func (m *Manager) run(ctx context.Context) {
	defer close(m.doneCh)

	// The first check runs immediately: a restart is exactly when the address
	// is most likely to have changed unobserved.
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.InfoContext(ctx, "stopping dynamic dns manager")

			return
		case <-m.refreshCh:
			if !timer.Stop() {
				// The timer may have fired between the select and here; drain
				// it so the Reset below is not pre-armed.
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}

		timer.Reset(m.checkOnce(ctx))
	}
}

// checkOnce performs one detect-and-publish cycle and returns how long to wait
// before the next one.
func (m *Manager) checkOnce(ctx context.Context) (next time.Duration) {
	addr, source, err := m.detector.Detect(ctx)
	if err != nil {
		return m.recordFailure(ctx, fmt.Errorf("detecting public address: %w", err), false)
	}

	prev := m.currentIP()

	// An apparent change is the one moment a wrong answer does damage, so it
	// is the one moment worth paying for a second opinion.  A reflector behind
	// a transparent proxy, or one that has started answering with its own
	// address, produces a plausible public IPv4 that is simply not ours;
	// believing it once is enough to point the domain at a stranger.  Steady
	// state costs nothing extra, because this runs only when the value moved.
	if prev != "" && prev != addr.String() {
		confirmed, confirmSource, confErr := m.detector.DetectExcluding(ctx, source)
		if confErr != nil {
			return m.recordFailure(
				ctx,
				fmt.Errorf("confirming change from %s to %s: %w", prev, addr, confErr),
				false,
			)
		}

		if confirmed != addr {
			// Not an error state so much as an unresolved one: two services
			// disagree, and neither is authoritative.  Wait rather than guess.
			return m.recordFailure(ctx, fmt.Errorf(
				"reflectors disagree: %s says %s, %s says %s; not publishing",
				source, addr, confirmSource, confirmed,
			), false)
		}

		m.logger.InfoContext(
			ctx,
			"public address changed",
			"from", prev,
			"to", addr.String(),
			"confirmed_by", confirmSource,
		)
	}

	m.recordDetection(addr, source)

	return m.publishAll(ctx, addr)
}

// publishAll updates every record that needs it and returns the next delay.
func (m *Manager) publishAll(ctx context.Context, addr netip.Addr) (next time.Duration) {
	var (
		attempted    int
		failed       int
		providerFail bool
	)

	for i := range m.recordCount() {
		rec, domain := m.recordAt(i)
		if !m.needsPublish(rec, addr) {
			continue
		}

		attempted++

		res, err := m.client.Update(ctx, rec.Domain, rec.Host, domain.Password, addr)
		if err != nil {
			failed++

			var provErr *ProviderError
			if errors.As(err, &provErr) {
				providerFail = true
			}

			m.logger.ErrorContext(
				ctx,
				"publishing record failed",
				"domain", rec.Domain,
				"host", rec.Host,
				"ip", addr.String(),
				"error", err,
			)

			m.setRecordError(i, err)

			continue
		}

		m.logger.InfoContext(
			ctx,
			"published record",
			"domain", rec.Domain,
			"host", rec.Host,
			"ip", res.PublishedIP,
		)

		m.setRecordPublished(i, res.PublishedIP)
	}

	if failed > 0 {
		return m.recordFailure(ctx, fmt.Errorf(
			"%d of %d record updates failed", failed, attempted,
		), providerFail)
	}

	m.recordSuccess()

	return m.conf.CheckInterval
}

// needsPublish reports whether rec should be sent to the provider.
//
// The decision is per record rather than global, so that when one host fails
// and another succeeds, the next cycle retries only the one that failed
// instead of either re-publishing everything or, worse, treating the whole
// domain as up to date and never retrying the failure at all.
func (m *Manager) needsPublish(rec RecordState, addr netip.Addr) (ok bool) {
	if rec.PublishedIP != addr.String() {
		return true
	}

	return m.now().Sub(rec.LastPublishAt) >= m.conf.ForceRefreshInterval
}

// nextDelay returns the delay after a failure.  providerFail selects the
// slower schedule used for provider rejections.
func (m *Manager) nextDelay(failures int, providerFail bool) (d time.Duration) {
	base, maxDelay := m.conf.CheckInterval, maxTransientBackoff
	if providerFail {
		base, maxDelay = providerErrorFloor, maxProviderBackoff
	}

	d = base
	for range failures - 1 {
		d *= 2
		if d >= maxDelay {
			return maxDelay
		}
	}

	return d
}

// State mutators.  Each takes the lock for exactly its own update, so that a
// slow provider call never holds it.

// currentIP returns the last detected address, or "".
func (m *Manager) currentIP() (ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.state.CurrentIP
}

// recordCount returns the number of managed records.
func (m *Manager) recordCount() (n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.state.Records)
}

// recordAt returns a copy of record i and the domain config it belongs to.
func (m *Manager) recordAt(i int) (rec RecordState, domain *DomainConfig) {
	m.mu.Lock()
	rec = m.state.Records[i]
	m.mu.Unlock()

	for _, d := range m.conf.Domains {
		if d.Domain == rec.Domain {
			return rec, d
		}
	}

	return rec, &DomainConfig{}
}

// recordDetection stores a successful detection.
func (m *Manager) recordDetection(addr netip.Addr, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.CurrentIP = addr.String()
	m.state.DetectedVia = source
	m.state.LastDetectedAt = m.now()
}

// recordSuccess clears the failure counters after a clean cycle.
func (m *Manager) recordSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.ConsecutiveFailures = 0
	m.state.LastError = ""
	m.state.LastErrorAt = time.Time{}
}

// recordFailure stores err, logs it, and returns the delay before the next
// attempt.
func (m *Manager) recordFailure(
	ctx context.Context,
	err error,
	providerFail bool,
) (next time.Duration) {
	m.mu.Lock()
	m.state.ConsecutiveFailures++
	m.state.LastError = err.Error()
	m.state.LastErrorAt = m.now()
	failures := m.state.ConsecutiveFailures
	m.mu.Unlock()

	next = m.nextDelay(failures, providerFail)

	m.logger.ErrorContext(
		ctx,
		"dynamic dns check failed",
		"consecutive_failures", failures,
		"retry_in", next,
		"error", err,
	)

	return next
}

// setRecordPublished marks record i as holding ip.
func (m *Manager) setRecordPublished(i int, ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.Records[i].PublishedIP = ip
	m.state.Records[i].LastPublishAt = m.now()
	m.state.Records[i].LastError = ""
}

// setRecordError marks record i as failed.
//
// PublishedIP is deliberately left untouched rather than cleared: it still
// describes what the record most probably holds, and clearing it would throw
// away the only evidence of what the world currently sees.
func (m *Manager) setRecordError(i int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.Records[i].LastError = err.Error()
}
