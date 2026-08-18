package amberddns

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// DefaultConfFileName is the fork-local config file, read from the AdGuard Home
// working directory.
//
// This is deliberately NOT part of AdGuardHome.yaml.  Two reasons, and the
// second is the load-bearing one.  Upstream owns that file's schema and runs
// migrations over it, so fork-only keys there are a standing merge conflict.
// And it holds a live credential: AdGuardHome.yaml is world-readable in common
// installs, is what operators paste into issues, and is what backup tooling
// copies around.  A separate file can be locked down on its own terms; see
// [Config.resolvePasswords].
const DefaultConfFileName = "amber_ddns.yaml"

// ConfFileEnv names the environment variable that overrides the config file
// path.
const ConfFileEnv = "ADGUARDHOME_AMBER_DDNS_CONF"

// Config is the dynamic DNS manager's configuration.
type Config struct {
	// Domains are the domains to keep updated.  A manager with no domains does
	// nothing.
	Domains []*DomainConfig `yaml:"domains"`

	// CheckInterval is how often the public address is re-detected.  Detection
	// is cheap and does not touch the provider; publishing happens only on a
	// change.  Defaults to [defaultCheckInterval].
	CheckInterval time.Duration `yaml:"check_interval"`

	// ForceRefreshInterval is how long a record is allowed to go without being
	// re-published even when the address has not changed.
	//
	// This is what makes the manager self-healing rather than merely reactive.
	// Its idea of the published address is a belief, and the belief can be
	// wrong in ways it cannot observe: someone edits the record by hand, a
	// provider-side rollback reverts it, or the process restarts having
	// published something it then forgets.  Without a periodic re-publish, a
	// record can sit wrong indefinitely while the manager reports everything
	// as fine.  Defaults to [defaultForceRefreshInterval].
	ForceRefreshInterval time.Duration `yaml:"force_refresh_interval"`

	// EchoURLs overrides the address reflectors.  Empty means
	// [DefaultEchoURLs].
	EchoURLs []string `yaml:"echo_urls"`

	// Endpoint overrides the provider endpoint.  Empty means
	// [NamecheapEndpoint].  Present for testing against a stub.
	Endpoint string `yaml:"endpoint"`

	// Enabled turns the manager on.  It is off unless explicitly enabled, so
	// that merely having the file present does not start publishing DNS.
	Enabled bool `yaml:"enabled"`
}

// DomainConfig is one domain and the host records to keep pointed at the
// current public address.
type DomainConfig struct {
	// Domain is the registered domain, e.g. "example.com".  Namecheap matches
	// it case-sensitively against the account.
	Domain string `yaml:"domain"`

	// PasswordEnv names an environment variable holding the domain's dynamic
	// DNS password.  Preferred over Password: it keeps the credential out of
	// the file entirely.
	PasswordEnv string `yaml:"password_env"`

	// Password is the domain's dynamic DNS password as a literal.  Using it
	// requires the config file to be unreadable by other users; see
	// [Config.resolvePasswords].
	Password string `yaml:"password"`

	// Hosts are the host records to update: "@" for the bare domain, "*" for
	// a wildcard, or a label such as "www".  Namecheap updates one record per
	// request, so each entry costs one call.
	Hosts []string `yaml:"hosts"`
}

// Defaults.
const (
	// defaultCheckInterval is how often the public address is re-detected.
	defaultCheckInterval = 5 * time.Minute

	// defaultForceRefreshInterval is how long an unchanged record may go
	// without being re-published.
	defaultForceRefreshInterval = 24 * time.Hour

	// minCheckInterval bounds how hard the reflectors can be polled.  They are
	// free services run by other people; a misconfigured interval of seconds
	// is abuse, and the address does not change that often anyway.
	minCheckInterval = time.Minute
)

// ReadConfig reads and validates the manager's config from path.  A missing
// file is not an error: it returns a disabled config, because the overwhelming
// majority of installs do not use this and must not be made to fail startup
// over a file they never created.
func ReadConfig(path string) (c *Config, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Enabled: false}, nil
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	c = &Config{}
	err = yaml.Unmarshal(data, c)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	err = c.validate()
	if err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}

	err = c.resolvePasswords(path)
	if err != nil {
		return nil, fmt.Errorf("resolving credentials for %s: %w", path, err)
	}

	return c, nil
}

// validate checks the config and fills in defaults.  A disabled config is not
// checked: an operator half-way through writing one should not be blocked from
// starting AdGuard Home.
func (c *Config) validate() (err error) {
	if !c.Enabled {
		return nil
	}

	if c.CheckInterval == 0 {
		c.CheckInterval = defaultCheckInterval
	}

	if c.CheckInterval < minCheckInterval {
		return fmt.Errorf(
			"check_interval %s is below the %s minimum",
			c.CheckInterval,
			minCheckInterval,
		)
	}

	if c.ForceRefreshInterval == 0 {
		c.ForceRefreshInterval = defaultForceRefreshInterval
	}

	if c.ForceRefreshInterval < c.CheckInterval {
		return fmt.Errorf(
			"force_refresh_interval %s is shorter than check_interval %s",
			c.ForceRefreshInterval,
			c.CheckInterval,
		)
	}

	if len(c.Domains) == 0 {
		return fmt.Errorf("enabled but no domains are configured")
	}

	for i, d := range c.Domains {
		err = d.validate()
		if err != nil {
			return fmt.Errorf("domains[%d]: %w", i, err)
		}
	}

	return nil
}

// validate checks one domain entry and fills in defaults.
func (d *DomainConfig) validate() (err error) {
	if d.Domain == "" {
		return fmt.Errorf("domain is required")
	}

	// Catch the common paste of a URL or a full hostname, which the provider
	// would reject with "Domain name not found" and leave the operator
	// guessing.
	if strings.ContainsAny(d.Domain, "/: ") {
		return fmt.Errorf(
			"domain %q must be a bare registered domain such as example.com,"+
				" not a URL or a host name",
			d.Domain,
		)
	}

	if len(d.Hosts) == 0 {
		// "@" is the bare domain, which is what almost everyone means.
		d.Hosts = []string{"@"}
	}

	for _, h := range d.Hosts {
		if h == "" {
			return fmt.Errorf("empty host entry; use %q for the bare domain", "@")
		}

		if strings.Contains(h, ".") && h != "*" {
			// A dotted host is legal at Namecheap for a sub-subdomain, but is
			// far more often someone writing "www.example.com" where "www"
			// belongs, which silently creates the wrong record.
			return fmt.Errorf(
				"host %q contains a dot; give the label only, e.g. %q rather than %q",
				h,
				"www",
				"www."+d.Domain,
			)
		}
	}

	if d.PasswordEnv == "" && d.Password == "" {
		return fmt.Errorf(
			"domain %q has no password_env or password;"+
				" this is the domain's Dynamic DNS Password, not the account password",
			d.Domain,
		)
	}

	return nil
}

// resolvePasswords replaces PasswordEnv references with their values and checks
// that any literal password is stored safely.
func (c *Config) resolvePasswords(path string) (err error) {
	if !c.Enabled {
		return nil
	}

	var literalUsed bool

	for _, d := range c.Domains {
		if d.PasswordEnv != "" {
			val := os.Getenv(d.PasswordEnv)
			if val == "" {
				return fmt.Errorf(
					"domain %q: environment variable %s is unset or empty",
					d.Domain,
					d.PasswordEnv,
				)
			}

			d.Password = val

			continue
		}

		literalUsed = true
	}

	if !literalUsed {
		return nil
	}

	// A literal credential in a file that other users can read is the same as
	// no credential.  Refusing to start is the right response: silently
	// continuing would leave a domain takeover available to any local account
	// while the manager reports itself healthy.
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking permissions of %s: %w", path, err)
	}

	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf(
			"%s holds a literal password but is mode %#o;"+
				" run chmod 600 on it, or use password_env instead",
			path,
			perm,
		)
	}

	return nil
}
