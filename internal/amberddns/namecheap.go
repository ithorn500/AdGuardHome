// Package amberddns keeps a rotating internet-facing IPv4 address published to
// a dynamic DNS provider.  It is a fork-local Amber addition: nothing in
// upstream AdGuard Home refers to it, and it owns no upstream types.
//
// The manager detects the current public address, decides whether the provider
// needs telling, and publishes it.  Detection lives in [Detector], publishing
// in [NamecheapClient], and the decision in [Manager].
package amberddns

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// NamecheapEndpoint is Namecheap's dynamic DNS update endpoint.
//
// Note that this is NOT the general Namecheap API at api.namecheap.com.  That
// one authenticates with an API key and requires the calling address to be
// whitelisted in advance, which cannot work when the whole point is that the
// calling address keeps changing; its setHosts method also replaces a domain's
// entire record set, so using it to move one A record risks deleting every
// other record on the domain.  This endpoint updates exactly one host record
// and authenticates with the per-domain dynamic DNS password.
const NamecheapEndpoint = "https://dynamicdns.park-your-domain.com/update"

// namecheapResponse is Namecheap's update response.
//
// The wire format has three traps, all confirmed against the live endpoint on
// 2026-08-18, and all of which make a naive client report success forever while
// never updating a record:
//
//  1. A rejected update still answers HTTP 200.  The status code says only
//     that Namecheap received the request.
//  2. A rejected update still answers <Done>true</Done>.  Done means "request
//     processed", not "record updated".
//  3. The XML declares encoding="utf-16" while the body is plain ASCII, and it
//     is served as Content-Type: application/json for good measure.
//
// So ErrCount is the only success signal, and the decoder needs a
// CharsetReader or it refuses the document outright.  See [parseNamecheap].
type namecheapResponse struct {
	// XMLName pins the root element.  Without it, any XML-ish document that
	// happens to lack an ErrCount field — an HTML error page from a proxy, a
	// captive portal's login form — decodes into a zero-valued struct, whose
	// ErrCount of 0 is indistinguishable from a successful update.  Silence
	// must not read as success.
	XMLName xml.Name `xml:"interface-response"`

	// IP is the address Namecheap says it published.  Absent on failure.
	IP string `xml:"IP"`

	// Errors are the human-readable failure reasons, when ErrCount is
	// non-zero.
	Errors []string `xml:"errors>Err1"`

	// Responses carry Namecheap's own numeric codes, which are more stable
	// than the message strings.
	Responses []namecheapResponseDetail `xml:"responses>response"`

	// ErrCount is zero on success.  This is the ONLY field that means it
	// worked.
	ErrCount int `xml:"ErrCount"`
}

// namecheapResponseDetail is one entry of Namecheap's responses list.
type namecheapResponseDetail struct {
	Description    string `xml:"Description"`
	ResponseString string `xml:"ResponseString"`
	ResponseNumber int    `xml:"ResponseNumber"`
}

// UpdateResult is the outcome of publishing one host record.
type UpdateResult struct {
	// PublishedIP is the address Namecheap echoed back, when it reported one.
	PublishedIP string

	// ProviderMessage is Namecheap's own text, kept verbatim so an operator
	// reads what the provider actually said rather than our paraphrase.
	ProviderMessage string

	// ProviderCode is Namecheap's numeric response code, or zero.
	ProviderCode int
}

// NamecheapClient publishes host records through Namecheap's dynamic DNS
// endpoint.
type NamecheapClient struct {
	// httpClient performs the update request.  It must not be nil.
	httpClient *http.Client

	// endpoint is the update URL, overridable for tests.
	endpoint string
}

// NewNamecheapClient returns a client publishing through endpoint.  If endpoint
// is empty, [NamecheapEndpoint] is used.  c must not be nil.
func NewNamecheapClient(c *http.Client, endpoint string) (n *NamecheapClient) {
	if endpoint == "" {
		endpoint = NamecheapEndpoint
	}

	return &NamecheapClient{
		httpClient: c,
		endpoint:   endpoint,
	}
}

// Update publishes addr as the A record for host.domain.  host is "@" for the
// bare domain and "*" for a wildcard.  password is the domain's dynamic DNS
// password, not a Namecheap account password.
//
// Namecheap matches domain and host case-sensitively against the account, and
// supports only IPv4, both of which are the caller's problem to get right; see
// [HostConfig.validate].
func (n *NamecheapClient) Update(
	ctx context.Context,
	domain string,
	host string,
	password string,
	addr netip.Addr,
) (res *UpdateResult, err error) {
	// The address is sent explicitly rather than letting Namecheap infer it
	// from the request's source address.  Inferring is fewer moving parts, but
	// it publishes an address nothing in this process ever saw, so a wrong
	// value could not be logged, compared against the previous one, or
	// explained afterwards.
	q := url.Values{
		"host":     {host},
		"domain":   {domain},
		"password": {password},
		"ip":       {addr.String()},
	}

	reqURL := n.endpoint + "?" + q.Encode()

	// Only GET works here; Namecheap ignores POST bodies.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		// Redact before this reaches a log or an error string: the password is
		// a query parameter, so the raw URL is a credential.
		return nil, fmt.Errorf("building request for %s: %w", redactPassword(reqURL), err)
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		// Do not wrap err with %w directly into a message: *url.Error prints
		// the URL it was given, password and all, so a redacted format string
		// with a raw error in it leaks the credential anyway.  redactedError
		// scrubs the rendered message while keeping the chain intact for
		// errors.Is, which shutdown relies on to recognise a cancelled
		// context.
		return nil, &redactedError{err: fmt.Errorf("requesting %s: %w", redactPassword(reqURL), err)}
	}
	defer func() { _ = resp.Body.Close() }()

	// Cap the read: this is an unauthenticated-response path and the body is a
	// few hundred bytes in every observed case.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// A non-200 is not the success signal, but it IS worth distinguishing from
	// a well-formed rejection, because it usually means we are talking to
	// something that is not Namecheap — a captive portal, say.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"namecheap returned http %d: %s",
			resp.StatusCode,
			firstLine(string(body)),
		)
	}

	return parseNamecheap(body, addr)
}

// parseNamecheap turns an update response body into a result, or into the error
// Namecheap actually reported.  want is the address we asked it to publish.
func parseNamecheap(body []byte, want netip.Addr) (res *UpdateResult, err error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))

	// The document lies about its encoding: it declares utf-16 and is ASCII.
	// Without this, Decode fails with "xml: encoding \"utf-16\" declared but
	// Decoder.CharsetReader is nil" and every update looks like a parse error.
	// Passing the reader through unchanged is correct precisely BECAUSE the
	// declaration is wrong.
	dec.CharsetReader = func(_ string, input io.Reader) (r io.Reader, err error) {
		return input, nil
	}

	var parsed namecheapResponse
	err = dec.Decode(&parsed)
	if err != nil {
		return nil, fmt.Errorf("parsing response %q: %w", firstLine(string(body)), err)
	}

	if parsed.XMLName.Local != "interface-response" {
		return nil, fmt.Errorf(
			"response is not a namecheap update reply, got root element %q in %q",
			parsed.XMLName.Local,
			firstLine(string(body)),
		)
	}

	res = &UpdateResult{
		PublishedIP: parsed.IP,
	}

	if len(parsed.Responses) > 0 {
		d := parsed.Responses[0]
		res.ProviderCode = d.ResponseNumber
		res.ProviderMessage = firstNonEmpty(d.Description, d.ResponseString)
	}

	// The one real check.  Not the status code, not <Done>.
	if parsed.ErrCount != 0 {
		msg := firstNonEmpty(
			strings.Join(parsed.Errors, "; "),
			res.ProviderMessage,
			"no reason given",
		)

		return res, &ProviderError{
			Message: msg,
			Code:    res.ProviderCode,
		}
	}

	// Namecheap echoes the published address on success.  If it echoes a
	// different one than we asked for, the record is not what we think it is,
	// and silently believing our own request would make the manager's stored
	// state a fiction.
	if parsed.IP != "" && parsed.IP != want.String() {
		return res, fmt.Errorf(
			"namecheap accepted the update but published %s, not %s",
			parsed.IP,
			want,
		)
	}

	if res.PublishedIP == "" {
		res.PublishedIP = want.String()
	}

	return res, nil
}

// ProviderError is a rejection reported by the provider itself, as distinct
// from a network or parse failure.  The difference matters to the retry policy:
// a provider that says "Passwords do not match" will say it again in sixty
// seconds, so retrying hard is pointless noise, whereas a refused connection is
// worth another try soon.  See [Manager.nextDelay].
type ProviderError struct {
	// Message is the provider's own wording.
	Message string

	// Code is the provider's numeric response code, or zero.
	Code int
}

// Error implements the error interface for *ProviderError.
func (err *ProviderError) Error() string {
	if err.Code != 0 {
		return fmt.Sprintf("provider rejected update: %s (code %d)", err.Message, err.Code)
	}

	return fmt.Sprintf("provider rejected update: %s", err.Message)
}

// redactPassword replaces the value of the password query parameter, so a URL
// can be logged or wrapped into an error.  It works on the raw string rather
// than through net/url so that it still redacts a URL too malformed to parse.
func redactPassword(raw string) (safe string) {
	const key = "password="

	// The value ends at a query separator, but a URL quoted inside an error
	// message ends at the quote instead, and one embedded in a log line may
	// end at whitespace.  Terminating on any of them keeps the redaction
	// correct wherever the URL has been carried to, and every occurrence is
	// replaced because errors nest.
	const delims = "&\"'<> \t\n"

	var b strings.Builder
	rest := raw

	for {
		i := strings.Index(rest, key)
		if i < 0 {
			b.WriteString(rest)

			return b.String()
		}

		b.WriteString(rest[:i+len(key)])
		b.WriteString("REDACTED")

		rest = rest[i+len(key):]

		end := strings.IndexAny(rest, delims)
		if end < 0 {
			return b.String()
		}

		rest = rest[end:]
	}
}

// redactedError renders a wrapped error with any credential removed, while
// leaving the error chain intact for errors.Is and errors.As.
type redactedError struct {
	err error
}

// Error implements the error interface for *redactedError.
func (e *redactedError) Error() string { return redactPassword(e.err.Error()) }

// Unwrap returns the wrapped error.
func (e *redactedError) Unwrap() (err error) { return e.err }

// firstLine returns the first line of s, trimmed and shortened, for use in
// error messages where the whole body would be noise.
func firstLine(s string) (line string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}

	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}

	return s
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) (s string) {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}
