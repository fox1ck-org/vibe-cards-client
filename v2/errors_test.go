package vibecards

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// protoString appends field `num` as a length-delimited string.
func protoString(b []byte, num int, s string) []byte {
	b = append(b, byte(num<<3|wireLen), byte(len(s)))
	return append(b, s...)
}

func errorInfoBytes(reason, domain string, meta map[string]string) []byte {
	var b []byte
	b = protoString(b, 1, reason)
	b = protoString(b, 2, domain)
	for k, v := range meta {
		var e []byte
		e = protoString(e, 1, k)
		e = protoString(e, 2, v)
		b = append(b, byte(3<<3|wireLen), byte(len(e)))
		b = append(b, e...)
	}
	return b
}

func connectBody(t *testing.T, code, msg, detailType, value string) []byte {
	t.Helper()
	body := map[string]any{"code": code, "message": msg}
	if detailType != "" {
		body["details"] = []map[string]any{{"type": detailType, "value": value, "debug": map[string]any{"x": 1}}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReasonsMapToTheirErrors(t *testing.T) {
	cases := []struct {
		reason Reason
		code   string
		want   error
	}{
		{ReasonNotFound, "not_found", ErrNotFound},
		{ReasonNotAllowed, "permission_denied", ErrNotAllowed},
		{ReasonLimitReached, "failed_precondition", ErrLimitReached},
		{ReasonInsufficientBalance, "failed_precondition", ErrInsufficientBalance},
		{ReasonIssuerUnsupported, "unimplemented", ErrIssuerUnsupported},
		{ReasonIssuerFailed, "unavailable", ErrIssuerFailed},
		{ReasonCardNotActive, "failed_precondition", ErrCardNotActive},
		{ReasonHeldByOther, "failed_precondition", ErrHeldByOther},
		{ReasonNotSharedForUse, "permission_denied", ErrNotSharedForUse},
		{ReasonReasonRequired, "failed_precondition", ErrReasonRequired},
		{ReasonCooldown, "failed_precondition", ErrCooldown},
		{ReasonGrantSpent, "not_found", ErrGrantSpent},
		{ReasonCardUnreadable, "failed_precondition", ErrCardUnreadable},
		{ReasonInProgress, "aborted", ErrInProgress},
		{ReasonNotInPlan, "failed_precondition", ErrNotInPlan},
		{ReasonTopUpDisabled, "failed_precondition", ErrTopUpDisabled},
		{ReasonLabelExists, "already_exists", ErrLabelExists},
		{ReasonRuleInvalid, "invalid_argument", ErrRuleInvalid},
		{ReasonNoHolderProfile, "failed_precondition", ErrNoHolderProfile},
		{ReasonProfileInUse, "failed_precondition", ErrProfileInUse},
	}
	all := make([]error, 0, len(cases))
	for _, c := range cases {
		all = append(all, c.want)
	}
	for _, c := range cases {
		t.Run(string(c.reason), func(t *testing.T) {
			// The message deliberately says something else: only the
			// reason decides.
			value := base64.StdEncoding.EncodeToString(errorInfoBytes(string(c.reason), "cards.v2", nil))
			err := decodeError(400, connectBody(t, c.code, "card is not bindable: unreadable billing profile", "google.rpc.ErrorInfo", value))
			if !errors.Is(err, c.want) {
				t.Fatalf("errors.Is(%v, %v) = false", err, c.want)
			}
			for _, other := range all {
				if other != c.want && errors.Is(err, other) {
					t.Fatalf("%s also matches %v", c.reason, other)
				}
			}
			if ReasonOf(err) != c.reason {
				t.Fatalf("ReasonOf = %q", ReasonOf(err))
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != c.code || e.Domain != "cards.v2" {
				t.Fatalf("got %#v", err)
			}
		})
	}
}

func TestErrorDecodingEdges(t *testing.T) {
	info := errorInfoBytes("issuer_unsupported", "cards.v2", map[string]string{"capability": "limit_per_txn"})

	t.Run("unpadded base64 and a type URL", func(t *testing.T) {
		v := base64.RawStdEncoding.EncodeToString(info)
		err := decodeError(501, connectBody(t, "unimplemented", "x", "type.googleapis.com/google.rpc.ErrorInfo", v))
		var e *Error
		if !errors.As(err, &e) || !errors.Is(err, ErrIssuerUnsupported) || e.MissingCapability() != "limit_per_txn" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("no detail: the code still matches, no reason does", func(t *testing.T) {
		err := decodeError(403, connectBody(t, "permission_denied", "no", "", ""))
		if !errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrNotAllowed) || ReasonOf(err) != "" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("a reason also matches its code sentinel", func(t *testing.T) {
		v := base64.StdEncoding.EncodeToString(errorInfoBytes("not_shared_for_use", "cards.v2", nil))
		err := decodeError(403, connectBody(t, "permission_denied", "x", "google.rpc.ErrorInfo", v))
		if !errors.Is(err, ErrNotSharedForUse) || !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("other detail types are skipped", func(t *testing.T) {
		v := base64.StdEncoding.EncodeToString(info)
		err := decodeError(400, connectBody(t, "internal", "x", "google.rpc.DebugInfo", v))
		if ReasonOf(err) != "" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("a malformed detail is ignored, not fatal", func(t *testing.T) {
		v := base64.StdEncoding.EncodeToString([]byte{0x0a, 0x7f, 'x'})
		err := decodeError(400, connectBody(t, "internal", "boom", "google.rpc.ErrorInfo", v))
		var e *Error
		if !errors.As(err, &e) || e.Reason != "" || e.Message != "boom" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("a gateway page is classified by status", func(t *testing.T) {
		err := decodeError(http.StatusUnauthorized, []byte("<html>401</html>"))
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("got %#v", err)
		}
		err = decodeError(http.StatusBadGateway, nil)
		var e *Error
		if !errors.As(err, &e) || e.Code != "unavailable" || e.Message != "Bad Gateway" {
			t.Fatalf("got %#v", err)
		}
	})
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
