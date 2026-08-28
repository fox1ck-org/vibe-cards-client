package vibecards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to vibe-cards over Connect's JSON unary protocol: a POST to
// /<package>.<Service>/<Method> with a JSON body. That is the whole protocol
// for unary calls, which is why this package needs no generated code and no
// dependencies — a consumer importing it inherits nothing.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// slow is used for the one call that legitimately blocks: waiting for a
	// 3DS code to be delivered. Sharing a timeout with the fast path would
	// either cut the wait short or make every other call patient.
	slow *http.Client
}

// Option tunes a Client.
type Option func(*Client)

// WithHTTPClient replaces the transport used for ordinary calls.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithTimeout sets the deadline for ordinary calls.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http = &http.Client{Timeout: d} }
}

// DefaultBaseURL is the in-cluster address. Consumers override it in config.
const DefaultBaseURL = "http://vibe-cards-api.vibe-cards.svc:8080"

// New builds a client, or returns nil when vibe-cards is not configured.
//
// A nil client is not an error: every method is nil-safe and answers
// ErrNotConfigured, so a consumer that has no card integration yet degrades
// instead of failing at construction.
func New(baseURL, apiKey string, opts ...Option) *Client {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil
	}
	c := &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	// The 3DS wait is bounded server-side; this only has to outlast it.
	c.slow = &http.Client{Timeout: 4 * time.Minute}
	return c
}

// Errors a caller branches on. Everything else arrives as *APIError.
var (
	ErrNotConfigured = errors.New("vibe-cards client is not configured")
	// ErrNoBillingProfile means the card has no address to present. It is a
	// configuration problem, not a transient one: retrying will not fix it.
	ErrNoBillingProfile = errors.New("card has no billing profile")
	// ErrGrantExpired covers unknown, expired and already-redeemed alike —
	// vibe-cards does not distinguish them, so neither does this.
	ErrGrantExpired = errors.New("pan grant is expired or already used")
	// ErrNotBindable means the card or its claim is in a state that cannot be
	// presented: closed, blocked, revoked.
	ErrNotBindable = errors.New("card is not in a bindable state")
	ErrNotFound    = errors.New("not found")
	ErrForbidden   = errors.New("forbidden")
)

// APIError is a non-2xx answer from vibe-cards.
type APIError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vibe-cards: %s (http %d, code %q)", e.Message, e.StatusCode, e.Code)
}

// classify maps Connect's error codes onto the named errors above so callers
// branch on a value rather than on a string.
func classify(e *APIError) error {
	switch {
	case strings.Contains(e.Message, "billing profile"):
		return fmt.Errorf("%w: %s", ErrNoBillingProfile, e.Message)
	case strings.Contains(e.Message, "grant is expired"):
		return fmt.Errorf("%w: %s", ErrGrantExpired, e.Message)
	case strings.Contains(e.Message, "not bindable"):
		return fmt.Errorf("%w: %s", ErrNotBindable, e.Message)
	}
	switch e.Code {
	case "not_found":
		return fmt.Errorf("%w: %s", ErrNotFound, e.Message)
	case "permission_denied", "unauthenticated":
		return fmt.Errorf("%w: %s", ErrForbidden, e.Message)
	}
	return e
}

// call performs one Connect unary JSON request.
func (c *Client) call(ctx context.Context, method string, in, out any, slow bool) error {
	if c == nil {
		return ErrNotConfigured
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// vck_ keys ride on Authorization, the same as every other consumer.
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpc := c.http
	if slow {
		httpc = c.slow
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(raw, apiErr)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(raw))
		}
		return classify(apiErr)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", method, err)
	}
	return nil
}
