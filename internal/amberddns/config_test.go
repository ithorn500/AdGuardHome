package amberddns

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConf writes a config file with the given mode and returns its path.
func writeConf(t *testing.T, body string, mode os.FileMode) (path string) {
	t.Helper()

	path = filepath.Join(t.TempDir(), DefaultConfFileName)
	require.NoError(t, os.WriteFile(path, []byte(body), mode))

	return path
}

// TestReadConfig_missingFileIsNotAnError covers the install that never uses
// this feature.  Failing startup over a file the operator never created would
// break AdGuard Home for everyone who does not want dynamic DNS.
func TestReadConfig_missingFileIsNotAnError(t *testing.T) {
	c, err := ReadConfig(filepath.Join(t.TempDir(), "absent.yaml"))
	require.NoError(t, err)

	assert.False(t, c.Enabled)
}

// TestReadConfig_presentButDisabled covers the half-written config.  Merely
// having the file must not start publishing DNS.
func TestReadConfig_presentButDisabled(t *testing.T) {
	path := writeConf(t, "enabled: false\ndomains: []\n", 0o600)

	c, err := ReadConfig(path)
	require.NoError(t, err)

	assert.False(t, c.Enabled)
}

func TestReadConfig_defaults(t *testing.T) {
	t.Setenv("TEST_DDNS_PW", "s3cret")

	path := writeConf(t, `
enabled: true
domains:
  - domain: example.com
    password_env: TEST_DDNS_PW
`, 0o600)

	c, err := ReadConfig(path)
	require.NoError(t, err)

	assert.True(t, c.Enabled)
	assert.Equal(t, defaultCheckInterval, c.CheckInterval)
	assert.Equal(t, defaultForceRefreshInterval, c.ForceRefreshInterval)

	require.Len(t, c.Domains, 1)

	// An unspecified host list means the bare domain, which is what almost
	// everyone configuring this actually wants.
	assert.Equal(t, []string{"@"}, c.Domains[0].Hosts)
	assert.Equal(t, "s3cret", c.Domains[0].Password)
}

func TestReadConfig_errors(t *testing.T) {
	testCases := []struct {
		name       string
		body       string
		wantErrSub string
	}{{
		name:       "enabled with no domains",
		body:       "enabled: true\n",
		wantErrSub: "no domains",
	}, {
		name: "domain given as a url",
		body: `
enabled: true
domains:
  - domain: https://example.com
    password: x
`,
		wantErrSub: "bare registered domain",
	}, {
		name: "host written as fqdn",
		body: `
enabled: true
domains:
  - domain: example.com
    password: x
    hosts: ["www.example.com"]
`,
		wantErrSub: "contains a dot",
	}, {
		name: "no credential",
		body: `
enabled: true
domains:
  - domain: example.com
`,
		wantErrSub: "Dynamic DNS Password",
	}, {
		name: "check interval too aggressive",
		body: `
enabled: true
check_interval: 5s
domains:
  - domain: example.com
    password: x
`,
		wantErrSub: "below the",
	}, {
		name: "force refresh shorter than check",
		body: `
enabled: true
check_interval: 10m
force_refresh_interval: 5m
domains:
  - domain: example.com
    password: x
`,
		wantErrSub: "shorter than",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConf(t, tc.body, 0o600)

			_, err := ReadConfig(path)
			require.Error(t, err)

			assert.Contains(t, err.Error(), tc.wantErrSub)
		})
	}
}

func TestReadConfig_missingEnvVar(t *testing.T) {
	path := writeConf(t, `
enabled: true
domains:
  - domain: example.com
    password_env: TEST_DDNS_ABSENT
`, 0o600)

	_, err := ReadConfig(path)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "TEST_DDNS_ABSENT")
}

// TestReadConfig_literalPasswordRequiresTightPermissions is the credential
// guard.  A dynamic DNS password in a world-readable file hands any local
// account the ability to repoint the domain, so starting up regardless — and
// reporting healthy — would be the worst of the available behaviours.
func TestReadConfig_literalPasswordRequiresTightPermissions(t *testing.T) {
	body := `
enabled: true
domains:
  - domain: example.com
    password: s3cret
`

	t.Run("world readable is refused", func(t *testing.T) {
		path := writeConf(t, body, 0o644)

		_, err := ReadConfig(path)
		require.Error(t, err)

		assert.Contains(t, err.Error(), "chmod 600")
	})

	t.Run("owner only is accepted", func(t *testing.T) {
		path := writeConf(t, body, 0o600)

		c, err := ReadConfig(path)
		require.NoError(t, err)

		assert.Equal(t, "s3cret", c.Domains[0].Password)
	})

	// The permission check exists to protect a literal credential only; an
	// env-var config has nothing to protect and must not be held to it.
	t.Run("env var config ignores file mode", func(t *testing.T) {
		t.Setenv("TEST_DDNS_PW", "s3cret")

		path := writeConf(t, `
enabled: true
domains:
  - domain: example.com
    password_env: TEST_DDNS_PW
`, 0o644)

		c, err := ReadConfig(path)
		require.NoError(t, err)

		assert.Equal(t, "s3cret", c.Domains[0].Password)
	})
}

func TestReadConfig_malformedYAML(t *testing.T) {
	path := writeConf(t, "enabled: true\n  domains: [", 0o600)

	_, err := ReadConfig(path)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "parsing")
}

func TestConfig_multipleHostsAndDomains(t *testing.T) {
	t.Setenv("PW_A", "a")
	t.Setenv("PW_B", "b")

	path := writeConf(t, `
enabled: true
check_interval: 2m
force_refresh_interval: 12h
domains:
  - domain: example.com
    password_env: PW_A
    hosts: ["@", "www", "*"]
  - domain: example.net
    password_env: PW_B
    hosts: ["home"]
`, 0o600)

	c, err := ReadConfig(path)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, c.CheckInterval)
	assert.Equal(t, 12*time.Hour, c.ForceRefreshInterval)
	require.Len(t, c.Domains, 2)
	assert.Equal(t, []string{"@", "www", "*"}, c.Domains[0].Hosts)
	assert.Equal(t, "b", c.Domains[1].Password)
}
