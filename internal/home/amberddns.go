package home

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/AdguardTeam/AdGuardHome/internal/amberddns"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// amberDDNSMu protects amberDDNSMgr.
var amberDDNSMu sync.Mutex

// amberDDNSMgr is the running dynamic DNS manager, or nil when the feature is
// not configured.
//
// It is a package-level value rather than a field on an upstream struct for
// the same reason the Amber Bus connector is: the fork adds no fields to
// upstream types, so a merge never has to reconcile one.
var amberDDNSMgr *amberddns.Manager

// amberDDNSConfPath returns the path of the dynamic DNS config file.
func amberDDNSConfPath(workDir string) (path string) {
	if p := os.Getenv(amberddns.ConfFileEnv); p != "" {
		return p
	}

	return filepath.Join(workDir, amberddns.DefaultConfFileName)
}

// initAmberDDNS starts the dynamic DNS manager if it is configured.
//
// A configuration fault here is logged and does not stop AdGuard Home.  That is
// a deliberate trade: this manager keeps a DNS record current, while the
// process it lives in is the network's resolver, and refusing to serve DNS to
// the whole house because a dynamic DNS password is wrong would be a far worse
// failure than the one being reported.  The fault is loud, and it is visible on
// /control/status rather than only in the log.
func initAmberDDNS(ctx context.Context, baseLogger *slog.Logger, tlsMgr *tlsManager, workDir string) {
	logger := baseLogger.With(slogutil.KeyPrefix, "amber_ddns")
	path := amberDDNSConfPath(workDir)

	conf, err := amberddns.ReadConfig(path)
	if err != nil {
		logger.ErrorContext(ctx, "dynamic dns is not running", "conf", path, "error", err)

		return
	}

	if !conf.Enabled {
		logger.DebugContext(ctx, "dynamic dns is not enabled", "conf", path)

		return
	}

	// Resolve through AdGuard Home's own DNS server, as the updater and the
	// version check already do.  It costs nothing extra and means the provider
	// and the reflectors are reached through the upstreams the operator
	// actually configured, rather than through the host's resolver.
	mgr := amberddns.NewManager(conf, httpClient(tlsMgr), logger)

	amberDDNSMu.Lock()
	amberDDNSMgr = mgr
	amberDDNSMu.Unlock()

	mgr.Start(ctx)
}

// amberDDNSManager returns the running manager, or nil.
func amberDDNSManager() (m *amberddns.Manager) {
	amberDDNSMu.Lock()
	defer amberDDNSMu.Unlock()

	return amberDDNSMgr
}

// refreshAmberDDNS asks the manager for an immediate check, if it is running.
//
// This is wired to SIGHUP alongside the rest of the reload handling, which is
// what makes it reachable: after correcting a password, an operator would
// otherwise have to wait out a backoff that can have grown to hours before
// learning whether the fix worked.
func refreshAmberDDNS() {
	if m := amberDDNSManager(); m != nil {
		m.Refresh()
	}
}

// amberDDNSUIStatus is the fork-local dynamic DNS block in the /control/status
// response.  Like the Amber Bus block beside it, it reports real state: an
// operator must be able to see that publishing has been failing, which is
// otherwise invisible until an external name stops resolving.
type amberDDNSUIStatus struct {
	// State is one of "disabled", "ok" or "failing", so the UI can colour it
	// without reimplementing the judgement.
	State string `json:"state"`

	// CurrentIP is the most recently detected public address, or "".
	CurrentIP string `json:"current_ip,omitempty"`

	// LastError is the most recent failure, or "".
	LastError string `json:"last_error,omitempty"`

	// Records is the per-record publication state.
	Records []amberddns.RecordState `json:"records"`

	// Enabled reports whether the manager is running at all.
	Enabled bool `json:"enabled"`
}

// Status values for [amberDDNSUIStatus.State].
const (
	amberDDNSStateDisabled = "disabled"
	amberDDNSStateOK       = "ok"
	amberDDNSStateFailing  = "failing"
)

// amberDDNSUIStatusSnapshot returns the dynamic DNS state for /control/status.
func amberDDNSUIStatusSnapshot() (s *amberDDNSUIStatus) {
	m := amberDDNSManager()
	if m == nil {
		return &amberDDNSUIStatus{
			State:   amberDDNSStateDisabled,
			Enabled: false,
			Records: []amberddns.RecordState{},
		}
	}

	st := m.Snapshot()

	state := amberDDNSStateOK
	if st.LastError != "" {
		state = amberDDNSStateFailing
	}

	return &amberDDNSUIStatus{
		State:     state,
		Enabled:   st.Enabled,
		CurrentIP: st.CurrentIP,
		LastError: st.LastError,
		Records:   st.Records,
	}
}

// ddnsStatus serves the dynamic DNS state over Amber Bus.  It is a read: the
// connector's write surface still requires its own contract change, so
// triggering an update is deliberately not exposed here.
func (c *amberBusConnector) ddnsStatus(_ context.Context, _ json.RawMessage) (data any, err error) {
	m := amberDDNSManager()
	if m == nil {
		return map[string]any{
			"schema":  amberDDNSSchema,
			"enabled": false,
			"state":   amberDDNSStateDisabled,
			"records": []amberddns.RecordState{},
		}, nil
	}

	st := m.Snapshot()
	ui := amberDDNSUIStatusSnapshot()

	return map[string]any{
		"schema":               amberDDNSSchema,
		"enabled":              st.Enabled,
		"state":                ui.State,
		"provider":             "namecheap",
		"current_ip":           st.CurrentIP,
		"detected_via":         st.DetectedVia,
		"last_detected_at":     st.LastDetectedAt,
		"last_error":           st.LastError,
		"last_error_at":        st.LastErrorAt,
		"consecutive_failures": st.ConsecutiveFailures,
		"records":              st.Records,
	}, nil
}

// amberDDNSSchema is the response schema identifier for the dynamic DNS
// function.
const amberDDNSSchema = "adguardhome.ddns.status.v1"
