package home

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/amberbusconnector"
	"github.com/AdguardTeam/AdGuardHome/internal/stats"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

const amberBusInvokePath = "/control/amber_bus/invoke"

// amberBusTokenEnv names the environment variable holding the shared connector
// token.  Declared here, next to the connector it belongs to, and used by the
// auth middleware as well, so the two can never disagree about the name.
const amberBusTokenEnv = "ADGUARDHOME_AMBER_BUS_TOKEN"

// amberBusMaxQueryLogLimit caps how many query-log entries one bus call may
// ask for.  The bus manifest promises that only BOUNDED query responses are
// exposed, and nothing enforced that: the handler passed any positive limit
// straight through and AdGuard applies no ceiling of its own, it merely
// defaults to 500.  A guardrail that lives only in a manifest is documentation.
const amberBusMaxQueryLogLimit = 1000

// Narrow dependencies.
//
// The connector used to reach into globalContext for each of these. Upstream is
// dismantling that global a couple of fields per release — the 2026-08-15 merge
// alone brought AGDNS-4352-rm-global-context-web and
// AGDNS-4289-rm-global-context-etc-hosts — so every reach is a future merge
// conflict with a deadline nobody controls.
//
// Naming only what the connector actually uses has three payoffs: the day a
// field leaves globalContext exactly ONE function below changes rather than
// three handler bodies; the handlers become testable with fakes, which is how
// the query-log filter bug got a regression test; and stats.Interface and
// querylog.QueryLog are ALREADY interfaces upstream, so when they are handed to
// the connector directly instead of fetched, the very same values satisfy these
// and nothing here moves at all.
type (
	// amberBusStats is the statistics module, as the connector uses it.
	amberBusStats interface {
		Snapshot(recent string) (resp *stats.StatsResp, err error)
	}

	// amberBusQueryLog is the query log, as the connector uses it.
	amberBusQueryLog interface {
		Search(ctx context.Context, values url.Values) (resp any, err error)
	}

	// amberBusClients is the persistent client list, as the connector uses it.
	//
	// Unlike the two above, clientsContainer is a concrete struct upstream, so
	// this is the one dependency that needs a described shape rather than
	// already having one.
	amberBusClients interface {
		forConfig() (objs []*clientObject)
	}
)

// amberBusDeps are the connector's dependencies.  A nil field means "not
// injected": resolve falls back to globalContext for it.
type amberBusDeps struct {
	stats    amberBusStats
	queryLog amberBusQueryLog
	clients  amberBusClients
}

// resolve returns d with any unset dependency filled in from globalContext.
//
// THIS IS THE ONLY PLACE THAT KNOWS globalContext EXISTS, and it is deliberately
// tolerant of both worlds: while upstream still has the global, an uninjected
// connector works exactly as before; once a dependency is handed in, the
// injected one wins and the fallback is dead code for it. When a field finally
// leaves globalContext, the compiler stops HERE — one function, one line each —
// instead of in three handler bodies. It stops loudly, which is the point: a
// missing dependency should fail the build, not the DNS server.
func (d amberBusDeps) resolve() (resolved amberBusDeps) {
	resolved = d

	if resolved.stats == nil && globalContext.stats != nil {
		resolved.stats = globalContext.stats
	}

	if resolved.queryLog == nil && globalContext.queryLog != nil {
		resolved.queryLog = globalContext.queryLog
	}

	if resolved.clients == nil && globalContext.clients.storage != nil {
		resolved.clients = &globalContext.clients
	}

	return resolved
}

// amberBusConnector serves the estate's read-only Amber Bus functions.
//
// It is a type of its own rather than a set of methods on webAPI so that the
// fork adds no fields to upstream's structs: the whole connector surface is
// constructed here and everything it needs is passed in.
type amberBusConnector struct {
	logger *slog.Logger
	deps   amberBusDeps

	// statusSnapshot reports the same status the /control/status endpoint
	// serves, passed as a function so the connector does not need webAPI.
	statusSnapshot func(ctx context.Context) (resp statusResponse, err error)
}

// registerAmberBusConnectorHandlers wires the connector into the web API.
func (web *webAPI) registerAmberBusConnectorHandlers() {
	logger := web.baseLogger.With(slogutil.KeyPrefix, "amber_bus")

	c := &amberBusConnector{
		logger:         logger,
		statusSnapshot: web.statusSnapshot,
	}

	dispatcher := amberbusconnector.New(logger, map[string]amberbusconnector.HandlerFunc{
		"adguard.status.get":       c.statusGet,
		"adguard.stats.get":        c.statsGet,
		"adguard.querylog.search":  c.queryLogSearch,
		"adguard.clients.list":     c.clientsList,
		"adguard.filtering.status": c.filteringStatus,
		"adguard.security.summary": c.securitySummary,
		"adguard.ddns.status":      c.ddnsStatus,
	})

	web.httpReg.Register(http.MethodPost, amberBusInvokePath, dispatcher.ServeHTTP)
}

// amberBusUIStatus is the fork-local Amber Bus block in the /control/status
// response.  It exists so the web UI can report the connector's real state:
// the mark used to be a static label that claimed "connected" unconditionally,
// which stayed reassuring through exactly the outage it should have reported.
type amberBusUIStatus struct {
	// Path is the invoke endpoint, so the UI does not hard-code it.
	Path string `json:"path"`

	// Mode is the connector's exposure, currently always read-only.
	Mode string `json:"mode"`

	// Configured reports whether the connector token is set.  When it is not,
	// bus callers fall through to session auth and the connector is silently
	// unreachable — the one state worth surfacing in the UI.
	Configured bool `json:"configured"`
}

// amberBusUIStatusSnapshot returns the connector state for /control/status.
func amberBusUIStatusSnapshot() (s *amberBusUIStatus) {
	return &amberBusUIStatus{
		Path:       amberBusInvokePath,
		Mode:       "read-only",
		Configured: os.Getenv(amberBusTokenEnv) != "",
	}
}

func (c *amberBusConnector) statusGet(ctx context.Context, _ json.RawMessage) (data any, err error) {
	status, err := c.statusSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"schema":                       "adguardhome.status.v1",
		"running":                      status.IsRunning,
		"version":                      status.Version,
		"dns_addresses":                status.DNSAddrs,
		"dns_port":                     status.DNSPort,
		"http_port":                    status.HTTPPort,
		"protection_enabled":           status.ProtectionEnabled,
		"protection_disabled_duration": status.ProtectionDisabledDuration,
		"dhcp_available":               status.IsDHCPAvailable,
		"start_time":                   status.StartTime,
		"connector": map[string]any{
			"native": true,
			"path":   amberBusInvokePath,
			"mode":   "read-only",
		},
	}, nil
}

func (c *amberBusConnector) statsGet(_ context.Context, payload json.RawMessage) (data any, err error) {
	st := c.deps.resolve().stats
	if st == nil {
		return nil, amberbusconnector.NewFunctionError(
			"stats_unavailable",
			"statistics module is not initialized",
		)
	}

	req := struct {
		Recent string `json:"recent"`
	}{}
	if err = decodeAmberBusPayload(payload, &req); err != nil {
		return nil, err
	}

	snapshot, err := st.Snapshot(req.Recent)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"schema": "adguardhome.stats.v1",
		"stats":  snapshot,
	}, nil
}

// amberBusQueryLogRequest is the payload of adguard.querylog.search.
type amberBusQueryLogRequest struct {
	Search         string `json:"search"`
	Domain         string `json:"domain"`
	Client         string `json:"client"`
	Status         string `json:"status"`
	ResponseStatus string `json:"response_status"`
	Reason         string `json:"reason"`
	OlderThan      string `json:"older_than"`
	Limit          int    `json:"limit"`
	Offset         int    `json:"offset"`
}

// searchTerm returns the single query-log search term req asks for.
//
// AdGuard's query log takes ONE term. The previous implementation appended
// search, domain and client to the same url.Values key, which produces a
// multi-valued key — and querylog reads it with url.Values.Get, which returns
// only the FIRST value. So a caller filtering by client got the whole log back
// with ok:true and no way to detect it. Silently answering a different question
// is worse than refusing, because nothing downstream can catch it.
//
// Any one of the three is accepted and means the same thing. More than one is
// refused, naming the offenders, rather than quietly honouring whichever
// happened to be first.
func (req *amberBusQueryLogRequest) searchTerm() (term string, err error) {
	given := make([]string, 0, 3)
	if req.Search != "" {
		given, term = append(given, "search"), req.Search
	}

	if req.Domain != "" {
		given, term = append(given, "domain"), req.Domain
	}

	if req.Client != "" {
		given, term = append(given, "client"), req.Client
	}

	if len(given) > 1 {
		return "", amberbusconnector.NewFunctionError("invalid_payload", fmt.Sprintf(
			"the query log accepts a single search term, but %v were given; "+
				"send one of search, domain or client",
			given,
		))
	}

	return term, nil
}

func (c *amberBusConnector) queryLogSearch(
	ctx context.Context,
	payload json.RawMessage,
) (data any, err error) {
	ql := c.deps.resolve().queryLog
	if ql == nil {
		return nil, amberbusconnector.NewFunctionError(
			"querylog_unavailable",
			"query log module is not initialized",
		)
	}

	req := amberBusQueryLogRequest{}
	if err = decodeAmberBusPayload(payload, &req); err != nil {
		return nil, err
	}

	term, err := req.searchTerm()
	if err != nil {
		return nil, err
	}

	respStatus := firstNonEmpty(req.ResponseStatus, req.Status)

	// AdGuard refuses these two together. The connector knows that statically,
	// so it says which fields conflict instead of forwarding the request and
	// relaying an opaque internal error.
	if respStatus != "" && req.Reason != "" {
		return nil, amberbusconnector.NewFunctionError(
			"invalid_payload",
			"response_status and reason cannot be used together",
		)
	}

	values := url.Values{}
	addQueryValue(values, "search", term)
	addQueryValue(values, "response_status", respStatus)
	addQueryValue(values, "reason", req.Reason)
	addQueryValue(values, "older_than", req.OlderThan)

	limit := req.Limit
	if limit <= 0 || limit > amberBusMaxQueryLogLimit {
		limit = amberBusMaxQueryLogLimit
	}
	values.Set("limit", strconv.Itoa(limit))

	if req.Offset > 0 {
		values.Set("offset", strconv.Itoa(req.Offset))
	}

	resp, err := ql.Search(ctx, values)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"schema": "adguardhome.querylog.search.v1",
		"result": resp,

		// Report the limit actually applied, so a truncated answer is visibly
		// truncated rather than looking like the end of the data.
		"limit": limit,
	}, nil
}

func (c *amberBusConnector) clientsList(_ context.Context, _ json.RawMessage) (data any, err error) {
	cl := c.deps.resolve().clients
	if cl == nil {
		return nil, amberbusconnector.NewFunctionError(
			"clients_unavailable",
			"clients storage is not initialized",
		)
	}

	clients := cl.forConfig()
	respClients := make([]map[string]any, 0, len(clients))
	for _, c := range clients {
		respClients = append(respClients, map[string]any{
			"name":                    c.Name,
			"ids":                     c.IDs,
			"tags":                    c.Tags,
			"use_global_settings":     c.UseGlobalSettings,
			"filtering_enabled":       c.FilteringEnabled,
			"safe_browsing_enabled":   c.SafeBrowsingEnabled,
			"parental_enabled":        c.ParentalEnabled,
			"ignore_query_log":        c.IgnoreQueryLog,
			"ignore_statistics":       c.IgnoreStatistics,
			"use_global_blocked_svcs": c.UseGlobalBlockedServices,
		})
	}

	return map[string]any{
		"schema":  "adguardhome.clients.v1",
		"clients": respClients,
	}, nil
}

func (c *amberBusConnector) filteringStatus(_ context.Context, _ json.RawMessage) (data any, err error) {
	config.RLock()
	defer config.RUnlock()

	if config.Filtering == nil {
		return nil, amberbusconnector.NewFunctionError(
			"filtering_unavailable",
			"filtering config is not initialized",
		)
	}

	flt := config.Filtering

	return map[string]any{
		"schema":                        "adguardhome.filtering.status.v1",
		"protection_enabled":            flt.ProtectionEnabled,
		"filtering_enabled":             flt.FilteringEnabled,
		"safe_browsing_enabled":         flt.SafeBrowsingEnabled,
		"safe_search_enabled":           flt.SafeSearchConf.Enabled,
		"parental_enabled":              flt.ParentalEnabled,
		"rewrites_enabled":              flt.RewritesEnabled,
		"filters_update_interval_hours": flt.FiltersUpdateIntervalHours,
		"filter_count":                  len(config.Filters),
		"whitelist_filter_count":        len(config.WhitelistFilters),
		"user_rule_count":               len(config.UserRules),
		"protection_disabled_until":     flt.ProtectionDisabledUntil,
	}, nil
}

func (c *amberBusConnector) securitySummary(
	ctx context.Context,
	payload json.RawMessage,
) (data any, err error) {
	statusData, err := c.statusGet(ctx, nil)
	if err != nil {
		return nil, err
	}

	filteringData, err := c.filteringStatus(ctx, nil)
	if err != nil {
		return nil, err
	}

	statsData, statsErr := c.statsGet(ctx, payload)

	// Collect every signal unconditionally, THEN derive the severity from what
	// was collected. The previous version guarded the appends on the severity
	// so far, which meant a critical finding suppressed the signals that
	// explained it: when AdGuard was down the summary reported "not_running"
	// and dropped "protection_disabled" — the very detail that distinguishes a
	// crash from a deliberate pause.
	signals := []string{}

	status, ok := statusData.(map[string]any)
	if !ok {
		return nil, amberbusconnector.NewFunctionError(
			"status_malformed",
			"status snapshot was not the expected shape",
		)
	}

	if running, _ := status["running"].(bool); !running {
		signals = append(signals, "adguard.not_running")
	}

	if enabled, _ := status["protection_enabled"].(bool); !enabled {
		signals = append(signals, "adguard.protection_disabled")
	}

	if statsErr != nil {
		signals = append(signals, "adguard.stats_unavailable")
	}

	return map[string]any{
		"schema":              "adguardhome.security.summary.v1",
		"generated_at":        time.Now().UTC().Format(time.RFC3339),
		"risk_level":          amberBusRiskLevel(signals),
		"signals":             signals,
		"status":              statusData,
		"filtering":           filteringData,
		"stats":               statsData,
		"stats_error":         functionErrorString(statsErr),
		"recommended_actions": amberBusRecommendedActions(signals),
	}, nil
}

// amberBusRiskLevel returns the severity of the worst signal present.
func amberBusRiskLevel(signals []string) (level string) {
	level = "info"
	for _, s := range signals {
		switch s {
		case "adguard.not_running":
			return "critical"
		case "adguard.protection_disabled", "adguard.stats_unavailable":
			level = "warning"
		}
	}

	return level
}

// amberBusRecommendedActions returns what an operator should do about signals.
//
// The field was published in adguardhome.security.summary.v1 from the start and
// hard-coded to an empty slice, which is a promise to consumers that nothing
// ever kept.
func amberBusRecommendedActions(signals []string) (actions []string) {
	actions = []string{}
	for _, s := range signals {
		switch s {
		case "adguard.not_running":
			actions = append(actions, "start the AdGuardHome service: the estate has no other resolver")
		case "adguard.protection_disabled":
			actions = append(actions, "re-enable protection, or confirm the pause was deliberate")
		case "adguard.stats_unavailable":
			actions = append(actions, "check the statistics module; the summary is incomplete without it")
		}
	}

	return actions
}

func decodeAmberBusPayload(payload json.RawMessage, dst any) (err error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil
	}

	if err = json.Unmarshal(payload, dst); err != nil {
		return amberbusconnector.NewFunctionError("invalid_payload", fmt.Sprintf("decoding payload: %s", err))
	}

	return nil
}

func functionErrorString(err error) (msg string) {
	if err == nil {
		return ""
	}

	return err.Error()
}

func addQueryValue(values url.Values, key, value string) {
	if value != "" {
		values.Add(key, value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
