package home

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/amberddns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAmberDDNS clears the package-level manager between tests.
func resetAmberDDNS(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		amberDDNSMu.Lock()
		amberDDNSMgr = nil
		amberDDNSMu.Unlock()
	})
}

func TestAmberDDNSConfPath(t *testing.T) {
	assert.Equal(
		t,
		filepath.Join("/work", amberddns.DefaultConfFileName),
		amberDDNSConfPath("/work"),
	)

	t.Setenv(amberddns.ConfFileEnv, "/etc/ddns.yaml")
	assert.Equal(t, "/etc/ddns.yaml", amberDDNSConfPath("/work"))
}

// TestAmberDDNSUIStatusSnapshot_notConfigured pins the reported state when the
// feature is off.  It must still be present and legible rather than absent:
// the UI should be able to say "disabled" rather than show nothing.
func TestAmberDDNSUIStatusSnapshot_notConfigured(t *testing.T) {
	resetAmberDDNS(t)

	s := amberDDNSUIStatusSnapshot()
	require.NotNil(t, s)

	assert.False(t, s.Enabled)
	assert.Equal(t, amberDDNSStateDisabled, s.State)
	assert.NotNil(t, s.Records, "records must marshal as [] rather than null")

	// The block must survive JSON round-tripping into the status response.
	data, err := json.Marshal(s)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"state":"disabled"`)
	assert.Contains(t, string(data), `"records":[]`)
}

// TestInitAmberDDNS_missingConfigDoesNotStartAnything covers the ordinary
// install: no config file, no manager, no error, DNS unaffected.
func TestInitAmberDDNS_missingConfigDoesNotStartAnything(t *testing.T) {
	resetAmberDDNS(t)

	initAmberDDNS(context.Background(), testLogger, nil, t.TempDir())

	assert.Nil(t, amberDDNSManager())
	assert.Equal(t, amberDDNSStateDisabled, amberDDNSUIStatusSnapshot().State)
}

// TestInitAmberDDNS_badConfigDoesNotStopAdGuard is the load-bearing failure
// mode.  This process is the network's resolver; a wrong dynamic DNS password
// must never be able to take DNS down with it.
func TestInitAmberDDNS_badConfigDoesNotStopAdGuard(t *testing.T) {
	resetAmberDDNS(t)

	dir := t.TempDir()
	path := filepath.Join(dir, amberddns.DefaultConfFileName)

	// Enabled, but with no domains: a validation failure.
	require.NoError(t, os.WriteFile(path, []byte("enabled: true\n"), 0o600))

	assert.NotPanics(t, func() {
		initAmberDDNS(context.Background(), testLogger, nil, dir)
	})

	assert.Nil(t, amberDDNSManager())
}

// TestRefreshAmberDDNS_withoutManagerIsSafe covers SIGHUP arriving on an
// install that does not use dynamic DNS at all.
func TestRefreshAmberDDNS_withoutManagerIsSafe(t *testing.T) {
	resetAmberDDNS(t)

	assert.NotPanics(t, refreshAmberDDNS)
}

func TestAmberBusConnector_ddnsStatus_notConfigured(t *testing.T) {
	resetAmberDDNS(t)

	c := &amberBusConnector{logger: testLogger}

	data, err := c.ddnsStatus(context.Background(), nil)
	require.NoError(t, err)

	m, ok := data.(map[string]any)
	require.True(t, ok)

	assert.Equal(t, amberDDNSSchema, m["schema"])
	assert.Equal(t, false, m["enabled"])
	assert.Equal(t, amberDDNSStateDisabled, m["state"])
}
