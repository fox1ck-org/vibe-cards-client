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

// DefaultBaseURL is a local endpoint. Configure the deployed service URL explicitly.
const DefaultBaseURL = "http://localhost:8080"

// The headers a service names the person behind a call with (WithOnBehalf):
// the platform's service-account protocol — a trusted realm customers service
// account names an org and a person. Anyone else sending these headers is
// refused, never ignored.
const (
	HeaderOnBehalfOrg = "X-On-Behalf-Org"
	HeaderOnBehalfSub = "X-On-Behalf-Sub"
)

// maxResponse bounds what one answer may be. A List page of a few hundred
// cards is far below it.
const maxResponse = 8 << 20

// Client talks to vibe-cards over Connect's unary JSON protocol: a POST to
// /cards.v2.<Service>/<Method> with a JSON body.
type Client struct {
	baseURL string
	apiKey  string
	// tokens mints the bearer when the context carries none (WithToken): a
	// platform service account's token, for WithOnBehalf calls.
	tokens func(context.Context) (string, error)
	http   *http.Client
	// slow is used for the one call that legitimately blocks: WaitThreeDS.
	// Sharing a timeout with the fast path would either cut the wait short
	// or make every other call patient.
	slow *http.Client
}

// Option tunes a Client.
type Option func(*Client)

// WithHTTPClient replaces the transport used for ordinary calls.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithTokenSource authenticates every call whose context carries no
// WithToken with a token from tokens, instead of the API key — a platform
// service account's access token (client credentials of realm customers),
// for the calls it makes WithOnBehalf. tokens is asked on every call, so it
// should cache (oauth2.ReuseTokenSource does). A failure fails the call
// before any request.
func WithTokenSource(tokens func(context.Context) (string, error)) Option {
	return func(c *Client) { c.tokens = tokens }
}

// WithTimeout sets the deadline for ordinary calls.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http = &http.Client{Timeout: d, Transport: c.http.Transport} }
}

// New builds a client, or returns nil when baseURL is empty.
//
// apiKey is the service's `vck_` key. It may be empty for a service that
// forwards its callers' own tokens (WithToken on every call) or calls as its
// service account (WithTokenSource); a call that carries no credential at
// all answers ErrNotConfigured.
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
	keyToken ctxKey = iota
	keyOnBehalf
)

type onBehalf struct {
	org, sub string
	active   bool
}

// WithOnBehalf marks the calls made with the returned context as made by a
// platform service account for the person `canonicalSub` (a realm customers
// subject) of the org `org`. Pair it with the service account's own token:
// WithTokenSource on the client, or WithToken on the context.
//
// vibe-cards believes it only from a service account it trusts (its
// ON_BEHALF_TRUSTED) and judges the call as that person of that org: their
// Cards role from console, what they see, "me" in a Filter or a Principal.
// What else the account may do is its ON_BEHALF_SCOPES: none — only
// CardService.List and SpendService.Query; `on_behalf` — every call, with a
// service's reach (take a card for a person it names, name who runs a
// usage); `pan:redeem` — CardDetails. Both org and person are required; a
// call missing either fails here, before any request. The headers confer no
// authority of their own and carry no role or email. Resolve a historical
// subject of another realm through the identity directory first.
func WithOnBehalf(ctx context.Context, org, canonicalSub string) context.Context {
	return context.WithValue(ctx, keyOnBehalf, onBehalf{org: strings.TrimSpace(org), sub: strings.TrimSpace(canonicalSub), active: true})
}

// WithToken makes the calls made with the returned context authenticate
// with `token` instead of the client's API key — a person's Keycloak access
// token forwarded by a backend-for-frontend, or another `vck_` key.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, keyToken, strings.TrimSpace(token))
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	if t, _ := ctx.Value(keyToken).(string); t != "" {
		return t, nil
	}
	if c.tokens != nil {
		t, err := c.tokens(ctx)
		if err != nil {
			return "", fmt.Errorf("service account token: %w", err)
		}
		return strings.TrimSpace(t), nil
	}
	return c.apiKey, nil
}

// call performs one Connect unary JSON request.
func (c *Client) call(ctx context.Context, procedure string, in, out any, slow bool) error {
	if c == nil {
		return ErrNotConfigured
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", procedure, err)
	}
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
	if behalf, ok := ctx.Value(keyOnBehalf).(onBehalf); ok && behalf.active {
		if behalf.org == "" || behalf.sub == "" {
			return fmt.Errorf("%s: an on-behalf call names both an organization and a person", procedure)
		}
		req.Header.Set(HeaderOnBehalfOrg, behalf.org)
		req.Header.Set(HeaderOnBehalfSub, behalf.sub)
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
