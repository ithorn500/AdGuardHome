// Package amberbusconnector exposes AdGuardHome functions through Amber Bus
// request/response envelopes.
package amberbusconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

const (
	// AppID is the Amber Bus application identifier for this AdGuardHome fork.
	AppID = "adguardhome"

	// ResponseSchema is the response envelope schema identifier.
	ResponseSchema = "adguardhome.invoke_result.v1"
)

// Request is the Amber Bus invoke request accepted by the native connector.
type Request struct {
	Payload    json.RawMessage `json:"payload"`
	Source     string          `json:"source,omitempty"`
	FunctionID string          `json:"function_id"`
}

// Response is the Amber Bus invoke response returned by the native connector.
type Response struct {
	Data       any            `json:"data,omitempty"`
	Error      *FunctionError `json:"error,omitempty"`
	Schema     string         `json:"schema"`
	AppID      string         `json:"app_id"`
	FunctionID string         `json:"function_id"`
	OK         bool           `json:"ok"`
}

// FunctionError is a structured connector error.
type FunctionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (err *FunctionError) Error() string {
	if err == nil {
		return ""
	}

	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

// NewFunctionError returns a new structured function error.
func NewFunctionError(code, msg string) *FunctionError {
	return &FunctionError{
		Code:    code,
		Message: msg,
	}
}

// HandlerFunc handles one read-only Amber Bus function.
type HandlerFunc func(ctx context.Context, payload json.RawMessage) (data any, err error)

// Dispatcher dispatches Amber Bus invoke envelopes to native AdGuardHome
// handlers.
type Dispatcher struct {
	logger   *slog.Logger
	handlers map[string]HandlerFunc
}

// New returns a new connector dispatcher.
func New(logger *slog.Logger, handlers map[string]HandlerFunc) (d *Dispatcher) {
	if logger == nil {
		logger = slog.Default()
	}

	return &Dispatcher{
		logger:   logger,
		handlers: handlers,
	}
}

// ServeHTTP handles Amber Bus invoke requests.
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		writeResponse(ctx, d.logger, w, http.StatusMethodNotAllowed, Response{
			Schema: ResponseSchema,
			AppID:  AppID,
			OK:     false,
			Error:  NewFunctionError("method_not_allowed", "only POST is allowed"),
		})

		return
	}

	var req Request
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeResponse(ctx, d.logger, w, http.StatusBadRequest, Response{
			Schema: ResponseSchema,
			AppID:  AppID,
			OK:     false,
			Error:  NewFunctionError("invalid_request", fmt.Sprintf("decoding request: %s", err)),
		})

		return
	}

	resp, status := d.Invoke(ctx, &req)
	writeResponse(ctx, d.logger, w, status, resp)
}

// Invoke dispatches req to a native function.
func (d *Dispatcher) Invoke(ctx context.Context, req *Request) (resp Response, status int) {
	resp = Response{
		Schema:     ResponseSchema,
		AppID:      AppID,
		FunctionID: req.FunctionID,
	}

	if req.FunctionID == "" {
		resp.Error = NewFunctionError("missing_function_id", "function_id is required")

		return resp, http.StatusBadRequest
	}

	h, ok := d.handlers[req.FunctionID]
	if !ok {
		resp.Error = NewFunctionError("unknown_function", "function is not registered by this connector")

		return resp, http.StatusNotFound
	}

	data, err := h(ctx, req.Payload)
	if err != nil {
		var fnErr *FunctionError
		if !errors.As(err, &fnErr) {
			fnErr = NewFunctionError("function_failed", err.Error())
		}

		resp.Error = fnErr

		return resp, http.StatusUnprocessableEntity
	}

	resp.OK = true
	resp.Data = data

	return resp, http.StatusOK
}

func writeResponse(ctx context.Context, l *slog.Logger, w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		l.ErrorContext(ctx, "writing amber bus response", "error", err)
	}
}
