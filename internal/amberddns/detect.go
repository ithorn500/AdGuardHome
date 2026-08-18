package amberddns

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
)

// DefaultEchoURLs are the address-reflection services used to learn the current
// public address, tried in order.
//
// More than one is listed because this is the single point on which every
// publish depends: if the only reflector is down, the manager cannot tell a
// changed address from a broken one.  They are deliberately run by different
// operators, so that an outage of one does not take the rest with it.
var DefaultEchoURLs = []string{
	"https://checkip.amazonaws.com",
	"https://api.ipify.org",
	"https://icanhazip.com",
	"https://ifconfig.me/ip",
}

// Detector reports the current public IPv4 address of this host.
type Detector struct {
	// httpClient performs the reflection requests.  It must not be nil.
	httpClient *http.Client

	// urls are the reflectors, tried in order.  It must not be empty.
	urls []string
}

// NewDetector returns a detector querying urls in order.  If urls is empty,
// [DefaultEchoURLs] is used.  c must not be nil.
func NewDetector(c *http.Client, urls []string) (d *Detector) {
	if len(urls) == 0 {
		urls = DefaultEchoURLs
	}

	return &Detector{
		httpClient: c,
		urls:       urls,
	}
}

// Detect returns the current public IPv4 address, using the first reflector
// that answers with one.  It reports which reflector was believed, because when
// two runs disagree the next question is always "which service said that".
func (d *Detector) Detect(ctx context.Context) (addr netip.Addr, source string, err error) {
	return d.detectFrom(ctx, d.urls)
}

// DetectExcluding returns the current public IPv4 address using any reflector
// other than exclude.  The manager uses it to get a genuinely independent
// second opinion before acting on an apparent change; asking the same service
// twice would confirm nothing.
func (d *Detector) DetectExcluding(
	ctx context.Context,
	exclude string,
) (addr netip.Addr, source string, err error) {
	urls := make([]string, 0, len(d.urls))
	for _, u := range d.urls {
		if u != exclude {
			urls = append(urls, u)
		}
	}

	if len(urls) == 0 {
		return netip.Addr{}, "", fmt.Errorf("no reflector available other than %s", exclude)
	}

	return d.detectFrom(ctx, urls)
}

// detectFrom tries each of urls in order and returns the first public IPv4
// address obtained.  If none answers, it returns the reasons they all failed,
// rather than only the last, because "all four are unreachable" and "three are
// fine and one returns junk" call for different repairs.
func (d *Detector) detectFrom(
	ctx context.Context,
	urls []string,
) (addr netip.Addr, source string, err error) {
	var failures []string

	for _, u := range urls {
		addr, err = d.query(ctx, u)
		if err == nil {
			return addr, u, nil
		}

		failures = append(failures, fmt.Sprintf("%s: %s", u, err))

		// Keep going after a failure, but stop if the caller gave up.
		if ctx.Err() != nil {
			break
		}
	}

	return netip.Addr{}, "", fmt.Errorf(
		"no reflector returned a public IPv4 address: %s",
		strings.Join(failures, "; "),
	)
}

// query asks one reflector for this host's address.
func (d *Detector) query(ctx context.Context, u string) (addr netip.Addr, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("building request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("http %d", resp.StatusCode)
	}

	// These endpoints answer with a bare address and a newline.  The limit
	// guards against one of them being replaced by something that streams.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reading body: %w", err)
	}

	return ParsePublicV4(string(body))
}

// ParsePublicV4 parses s as an IPv4 address that is routable on the public
// internet, rejecting anything else with a reason.
//
// This is the guard that stops the manager publishing a LAN address to the
// world's DNS.  A reflector behind a captive portal, a proxy that answers with
// its own interface, or a carrier-grade NAT deployment will all hand back
// something that parses perfectly well as an address and is useless — or
// actively harmful — as an A record.
func ParsePublicV4(s string) (addr netip.Addr, err error) {
	s = strings.TrimSpace(s)

	addr, err = netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parsing %q: %w", firstLine(s), err)
	}

	// Unmap first: a reflector answering ::ffff:203.0.113.7 means the same
	// IPv4 address, and should be treated as one rather than refused.
	addr = addr.Unmap()

	// Namecheap's dynamic DNS updates A records only; it has no AAAA support,
	// so an IPv6 answer cannot be published even though it is perfectly valid.
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("%s is not IPv4; only A records can be published", addr)
	}

	if reason := nonPublicReason(addr); reason != "" {
		return netip.Addr{}, fmt.Errorf("%s is not a public address: %s", addr, reason)
	}

	return addr, nil
}

// cgnatPrefix is RFC 6598 carrier-grade NAT space.  netip has no predicate for
// it, and it is the one an ISP hands out when there is no public address to be
// had at all.
var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// reservedPrefix is 240.0.0.0/4, reserved and not routable.
var reservedPrefix = netip.MustParsePrefix("240.0.0.0/4")

// nonPublicReason returns why addr is not publicly routable, or "" if it is.
func nonPublicReason(addr netip.Addr) (reason string) {
	switch {
	case addr.IsUnspecified():
		return "unspecified"
	case addr.IsLoopback():
		return "loopback"
	case addr.IsPrivate():
		return "RFC 1918 private"
	case addr.IsLinkLocalUnicast():
		return "link-local"
	case addr.IsMulticast():
		return "multicast"
	case cgnatPrefix.Contains(addr):
		// Worth naming precisely: under CGNAT no dynamic DNS record can reach
		// this host, so the repair is with the ISP, not with this config.
		return "carrier-grade NAT (RFC 6598); this connection has no public address to publish"
	case reservedPrefix.Contains(addr):
		return "reserved"
	case addr == netip.MustParseAddr("255.255.255.255"):
		return "broadcast"
	default:
		return ""
	}
}
