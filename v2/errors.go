package vibecards

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Reason is what a refusal's google.rpc.ErrorInfo.reason says: a cards.v2
// ErrorReason name without its prefix, in lower snake case. A new reason is
// an addition; a reason's meaning never changes.
type Reason string

// The reasons cards.v2 answers with (proto: cards.v2.ErrorReason).
const (
	ReasonNotFound            Reason = "not_found"
	ReasonNotAllowed          Reason = "not_allowed"
	ReasonLimitReached        Reason = "limit_reached"
	ReasonInsufficientBalance Reason = "insufficient_balance"
	ReasonIssuerUnsupported   Reason = "issuer_unsupported"
	ReasonIssuerFailed        Reason = "issuer_failed"
	ReasonCardNotActive       Reason = "card_not_active"
	ReasonHeldByOther         Reason = "held_by_other"
	ReasonNotSharedForUse     Reason = "not_shared_for_use"
	ReasonCooldown            Reason = "cooldown"
	ReasonGrantSpent          Reason = "grant_spent"
	ReasonCardUnreadable      Reason = "card_unreadable"
	ReasonInProgress          Reason = "in_progress"
	ReasonNotInPlan           Reason = "not_in_plan"
	ReasonTopUpDisabled       Reason = "topup_disabled"
	ReasonLabelExists         Reason = "label_exists"
	ReasonRuleInvalid         Reason = "rule_invalid"
	ReasonNoHolderProfile     Reason = "no_holder_profile"
	ReasonProfileInUse        Reason = "profile_in_use"
)

const (
	errorInfoType                = "google.rpc.ErrorInfo"
	errorInfoTypeURLPrefix       = "type.googleapis.com/"
	metadataKeyMissingCapability = "capability"
)

// Error is a refusal from vibe-cards: the Connect code, the message, and —
// when the server said why — the ErrorInfo reason. Compare it with
// errors.Is against the Err* values below.
type Error struct {
	// HTTPStatus is the status the answer came with.
	HTTPStatus int
	// Code is the Connect code: "not_found", "failed_precondition", ….
	Code string
	// Message is for people and logs. Never branch on it.
	Message string
	// Reason is ErrorInfo.reason; empty when the server gave none.
	Reason Reason
	// Domain is ErrorInfo.domain ("cards.v2").
	Domain string
	// Metadata is ErrorInfo.metadata — e.g. "capability" on
	// issuer_unsupported names the capability the issuer lacks.
	Metadata map[string]string
}

func (e *Error) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("vibe-cards: %s: %s (%s)", e.Reason, e.Message, e.Code)
	}
	return fmt.Sprintf("vibe-cards: %s (%s, http %d)", e.Message, e.Code, e.HTTPStatus)
}

// MissingCapability names the issuer capability an issuer_unsupported
// refusal is about ("pause", "limit_per_txn", …); empty otherwise.
func (e *Error) MissingCapability() string {
	if e == nil {
		return ""
	}
	return e.Metadata[metadataKeyMissingCapability]
}

// Is matches a reason sentinel by reason, and a code sentinel by Connect
// code.
func (e *Error) Is(target error) bool {
	s, ok := target.(sentinel)
	if !ok {
		return false
	}
	if s.reason != "" {
		return e.Reason == s.reason
	}
	return s.code != "" && e.Code == s.code
}

// sentinel is what errors.Is compares an *Error with. Unexported so the
// exported values cannot be mutated by a consumer.
type sentinel struct {
	reason Reason
	code   string
	text   string
}

func (s sentinel) Error() string { return s.text }

func reasonErr(r Reason, text string) error { return sentinel{reason: r, text: text} }

// One error per ErrorReason. errors.Is(err, ErrHeldByOther) is true exactly
// when the server answered with that reason.
var (
	ErrNotFound            = reasonErr(ReasonNotFound, "card, share, label, rule or alert not found or not visible")
	ErrNotAllowed          = reasonErr(ReasonNotAllowed, "the caller's role or right does not allow this")
	ErrLimitReached        = reasonErr(ReasonLimitReached, "a limit does not allow it")
	ErrInsufficientBalance = reasonErr(ReasonInsufficientBalance, "the organisation's balance does not cover it")
	ErrIssuerUnsupported   = reasonErr(ReasonIssuerUnsupported, "the card's issuer cannot do this")
	ErrIssuerFailed        = reasonErr(ReasonIssuerFailed, "the issuer refused or did not answer; retrying may help")
	ErrCardNotActive       = reasonErr(ReasonCardNotActive, "the card is not active")
	ErrHeldByOther         = reasonErr(ReasonHeldByOther, "somebody else holds the card")
	ErrNotSharedForUse     = reasonErr(ReasonNotSharedForUse, "the card is not the holder's and not shared with them for use")
	ErrCooldown            = reasonErr(ReasonCooldown, "the same person read this card's number too recently")
	// ErrGrantSpent covers an unknown, expired and already-redeemed ticket
	// alike: telling them apart would tell a prober which tokens exist.
	ErrGrantSpent = reasonErr(ReasonGrantSpent, "the pan ticket expired or was already redeemed")
	// ErrCardUnreadable: the issuer's page lacked a usable number, expiry or
	// CVV and the card went to QUARANTINE. Do not retry — return the card and
	// take another.
	ErrCardUnreadable = reasonErr(ReasonCardUnreadable, "the issuer hides the card's number, expiry or cvv")
	// ErrInProgress: the same request_id is still being processed. Ask again
	// with the SAME request_id later; a new one would buy another card.
	ErrInProgress      = reasonErr(ReasonInProgress, "the same request is still being processed")
	ErrNotInPlan       = reasonErr(ReasonNotInPlan, "the organisation's plan does not include this")
	ErrTopUpDisabled   = reasonErr(ReasonTopUpDisabled, "top-up is off")
	ErrLabelExists     = reasonErr(ReasonLabelExists, "a label with this name already exists")
	ErrRuleInvalid     = reasonErr(ReasonRuleInvalid, "the rule's parameters do not fit its kind")
	ErrNoHolderProfile = reasonErr(ReasonNoHolderProfile, "no holder name and address resolves for the card")
	ErrProfileInUse    = reasonErr(ReasonProfileInUse, "the holder data is still in use")
)

// Errors without a reason: transport-level answers and the client's own.
var (
	// ErrUnauthenticated: the key or token is missing, unknown or expired.
	ErrUnauthenticated = sentinel{code: "unauthenticated", text: "vibe-cards did not accept the credential"}
	// ErrPermissionDenied matches every permission_denied answer, with or
	// without a reason (not_allowed, not_shared_for_use).
	ErrPermissionDenied = sentinel{code: "permission_denied", text: "vibe-cards denied the call"}
	// ErrNotConfigured: a nil Client, or a call with no credential.
	ErrNotConfigured = errors.New("vibe-cards client is not configured")
	// ErrNoCardAvailable: TakeFirst found no card it could take.
	ErrNoCardAvailable = errors.New("no card matching the filter could be taken")
)

// ReasonOf is the ErrorInfo reason of err, or "" when err is not a refusal
// from vibe-cards or carries none.
func ReasonOf(err error) Reason {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

// wireError is Connect's JSON error body.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"details"`
}

// decodeError turns a non-200 answer into *Error. A body that is not
// Connect's (a gateway's HTML page) still yields a code, from the status.
func decodeError(status int, raw []byte) error {
	e := &Error{HTTPStatus: status}
	var w wireError
	if json.Unmarshal(raw, &w) == nil && w.Code != "" {
		e.Code, e.Message = w.Code, w.Message
		for _, d := range w.Details {
			if strings.TrimPrefix(d.Type, errorInfoTypeURLPrefix) != errorInfoType {
				continue
			}
			b, err := decodeBase64(d.Value)
			if err != nil {
				continue
			}
			info, err := parseErrorInfo(b)
			if err != nil {
				continue
			}
			e.Reason, e.Domain, e.Metadata = Reason(info.reason), info.domain, info.metadata
			break
		}
		return e
	}
	e.Code = codeFromHTTP(status)
	e.Message = strings.TrimSpace(string(raw))
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

// decodeBase64 accepts padded and unpadded standard base64: Connect allows
// either.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawStdEncoding.DecodeString(s)
}

// codeFromHTTP is Connect's HTTP-to-code table for answers without a body.
func codeFromHTTP(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "internal"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "permission_denied"
	case http.StatusNotFound:
		return "unimplemented"
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "unavailable"
	default:
		return "unknown"
	}
}
