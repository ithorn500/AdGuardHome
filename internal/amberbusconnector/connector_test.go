package amberbusconnector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestDispatcherInvoke(t *testing.T) {
	d := New(nil, map[string]HandlerFunc{
		"adguard.status.get": func(_ context.Context, payload json.RawMessage) (any, error) {
			return map[string]any{"payload": string(payload)}, nil
		},
		"adguard.querylog.search": func(context.Context, json.RawMessage) (any, error) {
			return nil, NewFunctionError("not_implemented", "query-log search is not wired yet")
		},
	})

	resp, status := d.Invoke(context.Background(), &Request{
		FunctionID: "adguard.status.get",
		Payload:    json.RawMessage(`{"include_connector":true}`),
	})
	if status != http.StatusOK {
		t.Fatalf("status: got %d, want %d", status, http.StatusOK)
	}
	if !resp.OK || resp.Error != nil {
		t.Fatalf("response: got ok=%t error=%v, want ok response", resp.OK, resp.Error)
	}

	resp, status = d.Invoke(context.Background(), &Request{FunctionID: "adguard.unknown"})
	if status != http.StatusNotFound {
		t.Fatalf("unknown status: got %d, want %d", status, http.StatusNotFound)
	}
	if resp.Error == nil || resp.Error.Code != "unknown_function" {
		t.Fatalf("unknown error: got %#v", resp.Error)
	}

	resp, status = d.Invoke(context.Background(), &Request{FunctionID: "adguard.querylog.search"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("function error status: got %d, want %d", status, http.StatusUnprocessableEntity)
	}
	if resp.Error == nil || resp.Error.Code != "not_implemented" {
		t.Fatalf("function error: got %#v", resp.Error)
	}
}

func TestFunctionError(t *testing.T) {
	err := NewFunctionError("code", "message")
	if err.Error() != "code: message" {
		t.Fatalf("error string: got %q", err.Error())
	}

	var target *FunctionError
	if !errors.As(err, &target) {
		t.Fatal("errors.As did not match FunctionError")
	}
}
