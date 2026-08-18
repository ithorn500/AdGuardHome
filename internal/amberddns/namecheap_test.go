package amberddns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveRejectionBody is a verbatim rejection captured from Namecheap's live
// endpoint on 2026-08-18, using a non-existent domain and a dummy password.
//
// It is kept byte-for-byte because every one of its oddities is a trap a
// plausible implementation falls into: the utf-16 declaration on an ASCII
// body, and <Done>true</Done> sitting directly above the failure it does not
// describe.  The request that produced it also answered HTTP 200.
const liveRejectionBody = `<?xml version="1.0" encoding="utf-16"?>
<interface-response>
  <Command>SETDNSHOST</Command>
  <Language>eng</Language>
  <ErrCount>1</ErrCount>
  <errors>
    <Err1>Domain name not found</Err1>
  </errors>
  <ResponseCount>1</ResponseCount>
  <responses>
    <response>
      <Description>Domain name not found</Description>
      <ResponseNumber>316153</ResponseNumber>
      <ResponseString>Validation error; not found; domain name(s)</ResponseString>
    </response>
  </responses>
  <Done>true</Done>
  <debug><![CDATA[]]></debug>
</interface-response>`

// successBody is a successful update in the same shape.
const successBody = `<?xml version="1.0" encoding="utf-16"?>
<interface-response>
  <Command>SETDNSHOST</Command>
  <Language>eng</Language>
  <IP>203.0.113.7</IP>
  <ErrCount>0</ErrCount>
  <errors />
  <ResponseCount>0</ResponseCount>
  <responses />
  <Done>true</Done>
</interface-response>`

func TestParseNamecheap_success(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.7")

	res, err := parseNamecheap([]byte(successBody), want)
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.PublishedIP)
}

// TestParseNamecheap_rejectionIsNotSuccess is the regression test for the
// central trap: Namecheap answers HTTP 200 and <Done>true</Done> on a rejected
// update, so anything that reads either as success reports a working updater
// that has never changed a record.
func TestParseNamecheap_rejectionIsNotSuccess(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.7")

	res, err := parseNamecheap([]byte(liveRejectionBody), want)
	require.Error(t, err)

	var provErr *ProviderError
	require.ErrorAs(t, err, &provErr)

	assert.Equal(t, "Domain name not found", provErr.Message)
	assert.Equal(t, 316153, provErr.Code)

	// The provider's own wording must survive to the operator.
	assert.Contains(t, provErr.Error(), "Domain name not found")

	// A rejection publishes nothing.
	require.NotNil(t, res)
	assert.Empty(t, res.PublishedIP)
}

// TestParseNamecheap_lyingCharsetDeclaration pins the workaround for the
// encoding="utf-16" declaration on an ASCII body.  A plain xml.Decoder refuses
// the document outright, which would make every update — including successful
// ones — look like a parse failure.
func TestParseNamecheap_lyingCharsetDeclaration(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.7")

	_, err := parseNamecheap([]byte(successBody), want)
	require.NoError(t, err)

	assert.Contains(t, successBody, `encoding="utf-16"`, "fixture must keep the false declaration")
}

// TestParseNamecheap_mismatchedIP covers the provider accepting the update but
// reporting a different address than the one requested.  Believing our own
// request there would make the manager's stored state a fiction.
func TestParseNamecheap_mismatchedIP(t *testing.T) {
	body := `<?xml version="1.0" encoding="utf-16"?>
<interface-response><IP>198.51.100.9</IP><ErrCount>0</ErrCount><Done>true</Done></interface-response>`

	res, err := parseNamecheap([]byte(body), netip.MustParseAddr("203.0.113.7"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "198.51.100.9")
	assert.Contains(t, err.Error(), "203.0.113.7")
	assert.Equal(t, "198.51.100.9", res.PublishedIP)
}

func TestParseNamecheap_garbage(t *testing.T) {
	_, err := parseNamecheap([]byte("<html>502 Bad Gateway</html>"), netip.MustParseAddr("203.0.113.7"))
	require.Error(t, err)
}

func TestNamecheapClient_Update(t *testing.T) {
	var gotQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()

		// Mirror the live endpoint's content type, which is wrong on purpose.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successBody))
	}))
	t.Cleanup(srv.Close)

	c := NewNamecheapClient(srv.Client(), srv.URL)

	res, err := c.Update(
		context.Background(),
		"example.com",
		"@",
		"s3cret",
		netip.MustParseAddr("203.0.113.7"),
	)
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.PublishedIP)
	assert.Equal(t, "example.com", gotQuery.Get("domain"))
	assert.Equal(t, "@", gotQuery.Get("host"))
	assert.Equal(t, "s3cret", gotQuery.Get("password"))

	// The address is sent explicitly rather than inferred from the source
	// address of the request.
	assert.Equal(t, "203.0.113.7", gotQuery.Get("ip"))
}

func TestNamecheapClient_Update_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway down"))
	}))
	t.Cleanup(srv.Close)

	c := NewNamecheapClient(srv.Client(), srv.URL)

	_, err := c.Update(
		context.Background(),
		"example.com",
		"@",
		"s3cret",
		netip.MustParseAddr("203.0.113.7"),
	)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "502")
}

// TestNamecheapClient_Update_errorsDoNotLeakPassword guards the credential
// path.  The password is a query parameter, so any error that quotes the URL
// is a credential disclosure into logs.
func TestNamecheapClient_Update_errorsDoNotLeakPassword(t *testing.T) {
	const password = "supersecretpassword"

	// A closed server produces a transport error, which is the path that wraps
	// the request URL into its message.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewNamecheapClient(srv.Client(), url)

	_, err := c.Update(
		context.Background(),
		"example.com",
		"@",
		password,
		netip.MustParseAddr("203.0.113.7"),
	)
	require.Error(t, err)

	assert.NotContains(t, err.Error(), password)
	assert.Contains(t, err.Error(), "REDACTED")
}

func TestRedactPassword(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{{
		name: "middle",
		in:   "https://h/update?domain=example.com&password=abc123&ip=1.2.3.4",
		want: "https://h/update?domain=example.com&password=REDACTED&ip=1.2.3.4",
	}, {
		name: "last",
		in:   "https://h/update?domain=example.com&password=abc123",
		want: "https://h/update?domain=example.com&password=REDACTED",
	}, {
		name: "absent",
		in:   "https://h/update?domain=example.com",
		want: "https://h/update?domain=example.com",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, redactPassword(tc.in))
		})
	}
}
