package vibecards

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the in-cluster address. Consumers override it in config.
const DefaultBaseURL = "http://vibe-cards-api.vibe-cards.svc:8080"

// The headers a service names the person behind a call with. vibe-cards
// believes them only from a service key carrying the `on_behalf` scope;
// from anybody else they are ignored, not refused.
const (
	HeaderActorSub   = "X-Actor-Sub"
	HeaderActorEmail = "X-Actor-Email"
)

// maxResponse bounds what one answer may be. A List page of a few hundred
// cards is far below it.
const maxResponse = 8 << 20

// Client talks to vibe-cards over Connect's unary JSON protocol: a POST to
// /cards.v2.<Service>/<Method> with a JSON body.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// slow is used for the one call that legitimately blocks: WaitThreeDS.
	// Sharing a timeout with the fast path would either cut the wait short
	// or make every other call patient.
	slow *http.Client
}

// Option tunes a Client.
type Option func(*Client)

// WithHTTPClient replaces the transport used for ordinary calls.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithTimeout sets the deadline for ordinary calls.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http = &http.Client{Timeout: d, Transport: c.http.Transport} }
}

// New builds a client, or returns nil when baseURL is empty.
//
// apiKey is the service's `vck_` key. It may be empty for a service that
// only ever forwards its callers' own tokens (WithToken on every call); a
// call that carries no credential at all answers ErrNotConfigured.
func New(baseURL, apiKey string, opts ...Option) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	c := &Client{
		baseURL: strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	// The server clamps a 3DS wait; this only has to outlast it.
	c.slow = &http.Client{Timeout: 4 * time.Minute, Transport: c.http.Transport}
	return c
}

type ctxKey int

const (
	keyActor ctxKey = iota
	keyToken
)

type actor struct{ sub, email string }

// OnBehalfOf marks the calls made with the returned context as made for the
// person `sub` (a Keycloak subject; email is optional, for the journal).
//
// It changes who vibe-cards RECORDS as having acted, and lets a service
// with the `on_behalf` scope take, present and wait for a card the person
// may use. It never widens what the service itself may see. Note that "me"
// in a Filter or a Principal is still the service: name the person
// explicitly.
func OnBehalfOf(ctx context.Context, sub, email string) context.Context {
	return context.WithValue(ctx, keyActor, actor{sub: strings.TrimSpace(sub), email: strings.TrimSpace(email)})
}

// WithToken makes the calls made with the returned context authenticate
// with `token` instead of the client's API key — a person's Keycloak access
// token forwarded by a backend-for-frontend, or another `vck_` key.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, keyToken, strings.TrimSpace(token))
}

func (c *Client) bearer(ctx context.Context) string {
	if t, _ := ctx.Value(keyToken).(string); t != "" {
		return t
	}
	return c.apiKey
}

// call performs one Connect unary JSON request.
func (c *Client) call(ctx context.Context, procedure string, in, out any, slow bool) error {
	if c == nil {
		return ErrNotConfigured
	}
	token := c.bearer(ctx)
	if token == "" {
		return fmt.Errorf("%w: no API key and no token in the context", ErrNotConfigured)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", procedure, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+procedure, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s: %w", procedure, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer "+token)
	if a, ok := ctx.Value(keyActor).(actor); ok && a.sub != "" {
		req.Header.Set(HeaderActorSub, a.sub)
		if a.email != "" {
			req.Header.Set(HeaderActorEmail, a.email)
		}
	}

	httpc := c.http
	if slow {
		httpc = c.slow
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", procedure, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("read %s: %w", procedure, err)
	}
	if resp.StatusCode != http.StatusOK {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", procedure, err)
	}
	return nil
}
