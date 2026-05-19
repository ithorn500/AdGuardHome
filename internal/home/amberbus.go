package home

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/amberbusconnector"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

const amberBusInvokePath = "/control/amber_bus/invoke"

func (web *webAPI) registerAmberBusConnectorHandlers() {
	logger := web.baseLogger.With(slogutil.KeyPrefix, "amber_bus")
	dispatcher := amberbusconnector.New(logger, map[string]amberbusconnector.HandlerFunc{
		"adguard.status.get":       web.amberBusStatusGet,
		"adguard.stats.get":        web.amberBusStatsGet,
		"adguard.querylog.search":  web.amberBusQueryLogSearch,
		"adguard.clients.list":     web.amberBusClientsList,
		"adguard.filtering.status": web.amberBusFilteringStatus,
		"adguard.security.summary": web.amberBusSecuritySummary,
	})

	web.httpReg.Register(http.MethodPost, amberBusInvokePath, dispatcher.ServeHTTP)
}

func (web *webAPI) amberBusStatusGet(ctx context.Context, _ json.RawMessage) (data any, err error) {
	status, err := web.statusSnapshot(ctx)
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

func (web *webAPI) amberBusStatsGet(_ context.Context, payload json.RawMessage) (data any, err error) {
	if globalContext.stats == nil {
		return nil, amberbusconnector.NewFunctionError("stats_unavailable", "statistics module is not initialized")
	}

	req := struct {
		Recent string `json:"recent"`
	}{}
	if err = decodeAmberBusPayload(payload, &req); err != nil {
		return nil, err
	}

	stats, err := globalContext.stats.Snapshot(req.Recent)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"schema": "adguardhome.stats.v1",
		"stats":  stats,
	}, nil
}

func (web *webAPI) amberBusQueryLogSearch(ctx context.Context, payload json.RawMessage) (data any, err error) {
	if globalContext.queryLog == nil {
		return nil, amberbusconnector.NewFunctionError("querylog_unavailable", "query log module is not initialized")
	}

	req := struct {
		Search         string `json:"search"`
		Domain         string `json:"domain"`
		Client         string `json:"client"`
		Status         string `json:"status"`
		ResponseStatus string `json:"response_status"`
		Reason         string `json:"reason"`
		OlderThan      string `json:"older_than"`
		Limit          int    `json:"limit"`
		Offset         int    `json:"offset"`
	}{}
	if err = decodeAmberBusPayload(payload, &req); err != nil {
		return nil, err
	}

	values := url.Values{}
	addQueryValue(values, "search", req.Search)
	if req.Domain != "" {
		addQueryValue(values, "search", req.Domain)
	}
	if req.Client != "" {
		addQueryValue(values, "search", req.Client)
	}
	addQueryValue(values, "response_status", firstNonEmpty(req.ResponseStatus, req.Status))
	addQueryValue(values, "reason", req.Reason)
	addQueryValue(values, "older_than", req.OlderThan)
	if req.Limit > 0 {
		values.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		values.Set("offset", strconv.Itoa(req.Offset))
	}

	resp, err := globalContext.queryLog.Search(ctx, values)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"schema": "adguardhome.querylog.search.v1",
		"result": resp,
	}, nil
}

func (web *webAPI) amberBusClientsList(_ context.Context, _ json.RawMessage) (data any, err error) {
	if globalContext.clients.storage == nil {
		return nil, amberbusconnector.NewFunctionError("clients_unavailable", "clients storage is not initialized")
	}

	clients := globalContext.clients.forConfig()
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

func (web *webAPI) amberBusFilteringStatus(_ context.Context, _ json.RawMessage) (data any, err error) {
	config.RLock()
	defer config.RUnlock()

	if config.Filtering == nil {
		return nil, amberbusconnector.NewFunctionError("filtering_unavailable", "filtering config is not initialized")
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

func (web *webAPI) amberBusSecuritySummary(ctx context.Context, payload json.RawMessage) (data any, err error) {
	statusData, err := web.amberBusStatusGet(ctx, nil)
	if err != nil {
		return nil, err
	}

	filteringData, err := web.amberBusFilteringStatus(ctx, nil)
	if err != nil {
		return nil, err
	}

	statsData, statsErr := web.amberBusStatsGet(ctx, payload)
	signals := []string{}
	riskLevel := "info"

	status := statusData.(map[string]any)
	if running, _ := status["running"].(bool); !running {
		riskLevel = "critical"
		signals = append(signals, "adguard.not_running")
	}
	if enabled, _ := status["protection_enabled"].(bool); !enabled && riskLevel != "critical" {
		riskLevel = "warning"
		signals = append(signals, "adguard.protection_disabled")
	}
	if statsErr != nil {
		if riskLevel == "info" {
			riskLevel = "warning"
		}
		signals = append(signals, "adguard.stats_unavailable")
	}

	return map[string]any{
		"schema":              "adguardhome.security.summary.v1",
		"generated_at":        time.Now().UTC().Format(time.RFC3339),
		"risk_level":          riskLevel,
		"signals":             signals,
		"status":              statusData,
		"filtering":           filteringData,
		"stats":               statsData,
		"stats_error":         functionErrorString(statsErr),
		"recommended_actions": []string{},
	}, nil
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
