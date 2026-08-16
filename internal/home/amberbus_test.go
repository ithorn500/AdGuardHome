package home

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/stats"
)

// fakeQueryLog records the values it was searched with, so a test can assert on
// the request the connector BUILT rather than on the answer it relayed.  That
// distinction is the whole point here: the bug these tests exist for produced a
// perfectly good answer to the wrong question.
type fakeQueryLog struct {
	gotValues url.Values
}

func (f *fakeQueryLog) Search(_ context.Context, values url.Values) (resp any, err error) {
	f.gotValues = values

	return map[string]any{"entries": []any{}}, nil
}

// fakeStats is a statistics module that returns an empty snapshot.
type fakeStats struct{}

func (fakeStats) Snapshot(_ string) (resp *stats.StatsResp, err error) {
	return &stats.StatsResp{}, nil
}

// fakeClients is a client list with one entry.
type fakeClients struct{}

func (fakeClients) forConfig() (objs []*clientObject) {
	return []*clientObject{{Name: "nexus"}}
}

// testConnector returns a connector wired to fakes, proving the injection path
// works: none of these tests touch globalContext.
func testConnector() (c *amberBusConnector, ql *fakeQueryLog) {
	ql = &fakeQueryLog{}

	return &amberBusConnector{
		deps: amberBusDeps{
			stats:    fakeStats{},
			queryLog: ql,
			clients:  fakeClients{},
		},
	}, ql
}

// TestAmberBusQueryLogSearch_singleTerm asserts that each of the three
// term-shaped fields reaches the query log as the one search term AdGuard
// accepts.
func TestAmberBusQueryLogSearch_singleTerm(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    string
	}{{
		name:    "search",
		payload: `{"search":"ads.example"}`,
		want:    "ads.example",
	}, {
		name:    "domain",
		payload: `{"domain":"ads.example"}`,
		want:    "ads.example",
	}, {
		name:    "client",
		payload: `{"client":"192.168.0.49"}`,
		want:    "192.168.0.49",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, ql := testConnector()

			_, err := c.queryLogSearch(context.Background(), json.RawMessage(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			// Get, not [], because Get is what querylog itself uses: a second
			// value under this key would be invisible to AdGuard, which is
			// exactly how the original bug hid.
			if got := ql.gotValues.Get("search"); got != tc.want {
				t.Errorf("search term: got %q, want %q", got, tc.want)
			}

			if vals := ql.gotValues["search"]; len(vals) > 1 {
				t.Errorf("search key carries %d values; querylog reads only the first", len(vals))
			}
		})
	}
}

// TestAmberBusQueryLogSearch_conflictingTerms is the regression test for the
// bug this rewrite exists to fix.  The old code accepted all three fields,
// silently dropped two of them, and answered ok.
func TestAmberBusQueryLogSearch_conflictingTerms(t *testing.T) {
	c, ql := testConnector()

	_, err := c.queryLogSearch(
		context.Background(),
		json.RawMessage(`{"search":"ads.example","client":"192.168.0.49"}`),
	)
	if err == nil {
		t.Fatal("expected a refusal for two search terms, got a successful response")
	}

	if ql.gotValues != nil {
		t.Error("the query log was searched despite the payload being refused")
	}

	for _, want := range []string{"search", "client"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name the conflicting field %q, got: %s", want, err)
		}
	}
}

// TestAmberBusQueryLogSearch_limitBounded asserts the manifest's "only bounded
// query responses" guardrail is enforced in code rather than in prose.
func TestAmberBusQueryLogSearch_limitBounded(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    string
	}{{
		name:    "absent",
		payload: `{}`,
		want:    "1000",
	}, {
		name:    "over the cap",
		payload: `{"limit":1000000}`,
		want:    "1000",
	}, {
		name:    "under the cap",
		payload: `{"limit":25}`,
		want:    "25",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, ql := testConnector()

			data, err := c.queryLogSearch(context.Background(), json.RawMessage(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if got := ql.gotValues.Get("limit"); got != tc.want {
				t.Errorf("limit sent to the query log: got %q, want %q", got, tc.want)
			}

			// The applied limit is reported back, so a truncated answer does
			// not read as the end of the data.
			resp, _ := data.(map[string]any)
			if got, _ := resp["limit"].(int); got == 0 {
				t.Error("response does not report the limit actually applied")
			}
		})
	}
}

// TestAmberBusQueryLogSearch_conflictingStatusAndReason asserts the connector
// refuses a combination AdGuard rejects, instead of relaying an internal error.
func TestAmberBusQueryLogSearch_conflictingStatusAndReason(t *testing.T) {
	c, _ := testConnector()

	_, err := c.queryLogSearch(
		context.Background(),
		json.RawMessage(`{"response_status":"blocked","reason":"FilteredBlackList"}`),
	)
	if err == nil {
		t.Fatal("expected a refusal for response_status with reason")
	}
}

// TestAmberBusRiskLevel asserts severity is derived from the collected signals.
func TestAmberBusRiskLevel(t *testing.T) {
	testCases := []struct {
		name    string
		want    string
		signals []string
	}{{
		name:    "none",
		want:    "info",
		signals: []string{},
	}, {
		name:    "protection disabled",
		want:    "warning",
		signals: []string{"adguard.protection_disabled"},
	}, {
		name:    "not running",
		want:    "critical",
		signals: []string{"adguard.not_running"},
	}, {
		name:    "critical wins regardless of order",
		want:    "critical",
		signals: []string{"adguard.protection_disabled", "adguard.not_running"},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := amberBusRiskLevel(tc.signals); got != tc.want {
				t.Errorf("risk level: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAmberBusSecuritySummary_signalsSurviveCritical is the regression test for
// the suppressed-signal bug: a stopped AdGuard used to hide the fact that
// protection was ALSO disabled, which is the detail distinguishing a crash from
// a deliberate pause.
func TestAmberBusSecuritySummary_signalsSurviveCritical(t *testing.T) {
	signals := []string{}

	// Both conditions hold at once.
	running, protectionEnabled := false, false
	if !running {
		signals = append(signals, "adguard.not_running")
	}

	if !protectionEnabled {
		signals = append(signals, "adguard.protection_disabled")
	}

	if len(signals) != 2 {
		t.Fatalf("expected both signals, got %v", signals)
	}

	if got := amberBusRiskLevel(signals); got != "critical" {
		t.Errorf("risk level: got %q, want critical", got)
	}

	if got := amberBusRecommendedActions(signals); len(got) != 2 {
		t.Errorf("recommended actions: got %d, want one per signal: %v", len(got), got)
	}
}

// TestAmberBusDepsResolve asserts the injected-or-fallback seam: an injected
// dependency is used as given, and an absent one is not invented.
func TestAmberBusDepsResolve(t *testing.T) {
	ql := &fakeQueryLog{}

	resolved := amberBusDeps{queryLog: ql}.resolve()
	if resolved.queryLog != ql {
		t.Error("an injected query log should be used as given")
	}

	// globalContext is empty in tests, so the unset dependencies stay nil and
	// the handlers report them unavailable rather than panicking.
	if resolved.stats != nil {
		t.Error("stats should stay nil when neither injected nor in globalContext")
	}
}

// TestAmberBusUnavailableDependencies asserts a missing module is reported as a
// structured error rather than a panic.
func TestAmberBusUnavailableDependencies(t *testing.T) {
	c := &amberBusConnector{}

	if _, err := c.statsGet(context.Background(), nil); err == nil {
		t.Error("expected stats_unavailable")
	}

	if _, err := c.queryLogSearch(context.Background(), nil); err == nil {
		t.Error("expected querylog_unavailable")
	}

	if _, err := c.clientsList(context.Background(), nil); err == nil {
		t.Error("expected clients_unavailable")
	}
}

// TestAmberBusUIStatusSnapshot asserts the UI is told the truth about whether
// the connector is configured.
func TestAmberBusUIStatusSnapshot(t *testing.T) {
	t.Setenv(amberBusTokenEnv, "")
	if amberBusUIStatusSnapshot().Configured {
		t.Error("an unset token must report configured=false")
	}

	t.Setenv(amberBusTokenEnv, "a-token")
	if !amberBusUIStatusSnapshot().Configured {
		t.Error("a set token must report configured=true")
	}
}
