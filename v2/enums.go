package vibecards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Enums go out as numbers and are read back from either form: vibe-cards
// answers with numbers today (its JSON codec), while the canonical protojson
// form is the value's name. A number this client does not know yet is kept
// as it is — a new status is an addition, not an error.

func encodeEnum(n int32) ([]byte, error) { return []byte(strconv.FormatInt(int64(n), 10)), nil }

func decodeEnum(b []byte, names map[string]int32) (int32, error) {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return 0, nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return 0, err
		}
		if n, ok := names[s]; ok {
			return n, nil
		}
		// An unknown name is a value this client predates.
		return 0, nil
	}
	n, err := strconv.ParseInt(string(b), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("enum: %w", err)
	}
	return int32(n), nil
}

func enumName(n int32, names map[string]int32) string {
	for k, v := range names {
		if v == n {
			return k
		}
	}
	return strconv.FormatInt(int64(n), 10)
}

// CardStatus is cards.v2.CardStatus.
type CardStatus int32

const (
	CardStatusUnspecified CardStatus = 0
	// CardStatusCreating: the issuer has not finished; it turns ACTIVE by itself.
	CardStatusCreating CardStatus = 1
	CardStatusActive   CardStatus = 2
	// CardStatusPaused: stopped (or held by the platform when the issuer
	// cannot stop it — Card.StatusNote says so).
	CardStatusPaused CardStatus = 3
	// CardStatusQuarantine: the issuer did not show usable details; not
	// handed out until an administrator resumes it.
	CardStatusQuarantine CardStatus = 4
	CardStatusClosed     CardStatus = 5
	// CardStatusFailed: the issuer never finished issuing. Terminal.
	CardStatusFailed CardStatus = 6
)

var cardStatusNames = map[string]int32{
	"CARD_STATUS_UNSPECIFIED": 0, "CARD_STATUS_CREATING": 1, "CARD_STATUS_ACTIVE": 2,
	"CARD_STATUS_PAUSED": 3, "CARD_STATUS_QUARANTINE": 4, "CARD_STATUS_CLOSED": 5, "CARD_STATUS_FAILED": 6,
}

func (s CardStatus) String() string               { return enumName(int32(s), cardStatusNames) }
func (s CardStatus) MarshalJSON() ([]byte, error) { return encodeEnum(int32(s)) }
func (s *CardStatus) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, cardStatusNames)
	*s = CardStatus(n)
	return err
}

// Terminal reports a card that will never spend again.
func (s CardStatus) Terminal() bool { return s == CardStatusClosed || s == CardStatusFailed }

// CardStatusReason is cards.v2.CardStatusReason: WHY a card has its status —
// who stopped it and who can start it again.
type CardStatusReason int32

const (
	// CardStatusReasonUnspecified: nothing to explain (ACTIVE, CREATING,
	// FAILED, or a card somebody closed).
	CardStatusReasonUnspecified CardStatusReason = 0
	// CardStatusReasonUserPaused: PAUSED by a person; the issuer stopped it
	// and Resume starts it again.
	CardStatusReasonUserPaused CardStatusReason = 1
	// CardStatusReasonPlatformHold: PAUSED by a person, but the issuer
	// cannot stop cards — the platform holds it while the issuer STILL lets
	// it spend (StatusNote says what to do in the issuer's cabinet).
	CardStatusReasonPlatformHold CardStatusReason = 2
	// CardStatusReasonRulePaused: PAUSED by a rule's breach; lifted by itself
	// when the breach passes or its alert is resolved.
	CardStatusReasonRulePaused CardStatusReason = 3
	// CardStatusReasonIssuerBlocked: PAUSED by the issuer's own block or
	// hold. Nothing in the platform lifts it — replace the card.
	CardStatusReasonIssuerBlocked CardStatusReason = 4
	// CardStatusReasonQuarantine: the issuer's page hid the details.
	CardStatusReasonQuarantine CardStatusReason = 5
	// CardStatusReasonReplaced: CLOSED because the issuer reissued it as
	// Card.ReplacedBy.
	CardStatusReasonReplaced CardStatusReason = 6
)

var cardStatusReasonNames = map[string]int32{
	"CARD_STATUS_REASON_UNSPECIFIED": 0, "CARD_STATUS_REASON_USER_PAUSED": 1,
	"CARD_STATUS_REASON_PLATFORM_HOLD": 2, "CARD_STATUS_REASON_RULE_PAUSED": 3,
	"CARD_STATUS_REASON_ISSUER_BLOCKED": 4, "CARD_STATUS_REASON_QUARANTINE": 5,
	"CARD_STATUS_REASON_REPLACED": 6,
}

func (r CardStatusReason) String() string               { return enumName(int32(r), cardStatusReasonNames) }
func (r CardStatusReason) MarshalJSON() ([]byte, error) { return encodeEnum(int32(r)) }
func (r *CardStatusReason) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, cardStatusReasonNames)
	*r = CardStatusReason(n)
	return err
}

// PrincipalKind is cards.v2.Principal.Kind.
type PrincipalKind int32

const (
	PrincipalKindUnspecified PrincipalKind = 0
	PrincipalPerson          PrincipalKind = 1
	PrincipalService         PrincipalKind = 2
	PrincipalOrg             PrincipalKind = 3
)

var principalKindNames = map[string]int32{
	"KIND_UNSPECIFIED": 0, "KIND_PERSON": 1, "KIND_SERVICE": 2, "KIND_ORG": 3,
}

func (k PrincipalKind) String() string               { return enumName(int32(k), principalKindNames) }
func (k PrincipalKind) MarshalJSON() ([]byte, error) { return encodeEnum(int32(k)) }
func (k *PrincipalKind) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, principalKindNames)
	*k = PrincipalKind(n)
	return err
}

// Right is cards.v2.Right: what a share lets its recipient do.
type Right int32

const (
	RightUnspecified Right = 0
	// RightView: see the card, its operations, and its number with a reason.
	RightView Right = 1
	// RightUse: VIEW plus take it, pause it and receive its 3DS codes.
	RightUse Right = 2
)

var rightNames = map[string]int32{"RIGHT_UNSPECIFIED": 0, "RIGHT_VIEW": 1, "RIGHT_USE": 2}

func (r Right) String() string               { return enumName(int32(r), rightNames) }
func (r Right) MarshalJSON() ([]byte, error) { return encodeEnum(int32(r)) }
func (r *Right) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, rightNames)
	*r = Right(n)
	return err
}

// TxStatus is cards.v2.TxStatus.
type TxStatus int32

const (
	TxStatusUnspecified TxStatus = 0
	TxStatusPending     TxStatus = 1
	TxStatusCompleted   TxStatus = 2
	TxStatusDeclined    TxStatus = 3
	TxStatusRefunded    TxStatus = 4
)

var txStatusNames = map[string]int32{
	"TX_STATUS_UNSPECIFIED": 0, "TX_STATUS_PENDING": 1, "TX_STATUS_COMPLETED": 2,
	"TX_STATUS_DECLINED": 3, "TX_STATUS_REFUNDED": 4,
}

func (s TxStatus) String() string               { return enumName(int32(s), txStatusNames) }
func (s TxStatus) MarshalJSON() ([]byte, error) { return encodeEnum(int32(s)) }
func (s *TxStatus) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, txStatusNames)
	*s = TxStatus(n)
	return err
}

// BulkAction is cards.v2.BulkAction.
type BulkAction int32

const (
	BulkActionUnspecified BulkAction = 0
	BulkPause             BulkAction = 1
	BulkResume            BulkAction = 2
	BulkClose             BulkAction = 3
	// BulkSetLimits uses BulkInput.Limits.
	BulkSetLimits BulkAction = 4
	// BulkAddLabels and BulkRemoveLabels use BulkInput.Labels.
	BulkAddLabels    BulkAction = 5
	BulkRemoveLabels BulkAction = 6
)

var bulkActionNames = map[string]int32{
	"BULK_ACTION_UNSPECIFIED": 0, "BULK_ACTION_PAUSE": 1, "BULK_ACTION_RESUME": 2, "BULK_ACTION_CLOSE": 3,
	"BULK_ACTION_SET_LIMITS": 4, "BULK_ACTION_ADD_LABELS": 5, "BULK_ACTION_REMOVE_LABELS": 6,
}

func (a BulkAction) String() string               { return enumName(int32(a), bulkActionNames) }
func (a BulkAction) MarshalJSON() ([]byte, error) { return encodeEnum(int32(a)) }
func (a *BulkAction) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, bulkActionNames)
	*a = BulkAction(n)
	return err
}

// GroupBy is cards.v2.GroupBy. Unspecified: no grouping, the operations
// themselves come back, paged.
type GroupBy int32

const (
	GroupByNone     GroupBy = 0
	GroupByCard     GroupBy = 1
	GroupByPerson   GroupBy = 2
	GroupByIssuer   GroupBy = 3
	GroupByMerchant GroupBy = 4
	GroupByLabel    GroupBy = 5
	GroupByUsage    GroupBy = 6
)

var groupByNames = map[string]int32{
	"GROUP_BY_UNSPECIFIED": 0, "GROUP_BY_CARD": 1, "GROUP_BY_PERSON": 2, "GROUP_BY_ISSUER": 3,
	"GROUP_BY_MERCHANT": 4, "GROUP_BY_LABEL": 5, "GROUP_BY_USAGE": 6,
}

func (g GroupBy) String() string               { return enumName(int32(g), groupByNames) }
func (g GroupBy) MarshalJSON() ([]byte, error) { return encodeEnum(int32(g)) }
func (g *GroupBy) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, groupByNames)
	*g = GroupBy(n)
	return err
}

// Bucket is cards.v2.Bucket. Unspecified: no series.
type Bucket int32

const (
	BucketNone Bucket = 0
	BucketHour Bucket = 1
	BucketDay  Bucket = 2
)

var bucketNames = map[string]int32{"BUCKET_UNSPECIFIED": 0, "BUCKET_HOUR": 1, "BUCKET_DAY": 2}

func (b Bucket) String() string               { return enumName(int32(b), bucketNames) }
func (b Bucket) MarshalJSON() ([]byte, error) { return encodeEnum(int32(b)) }
func (b *Bucket) UnmarshalJSON(raw []byte) error {
	n, err := decodeEnum(raw, bucketNames)
	*b = Bucket(n)
	return err
}

// BulkOperationState is cards.v2.BulkOperationState.
type BulkOperationState int32

const (
	BulkOperationUnspecified BulkOperationState = 0
	BulkOperationRunning     BulkOperationState = 1
	// BulkOperationDone: every selected card was tried; Failures says which
	// were refused.
	BulkOperationDone BulkOperationState = 2
	// BulkOperationFailed: the operation stopped before trying every card;
	// the cards it did not reach are unchanged.
	BulkOperationFailed BulkOperationState = 3
)

var bulkOperationStateNames = map[string]int32{
	"BULK_OPERATION_STATE_UNSPECIFIED": 0, "BULK_OPERATION_STATE_RUNNING": 1,
	"BULK_OPERATION_STATE_DONE": 2, "BULK_OPERATION_STATE_FAILED": 3,
}

func (s BulkOperationState) String() string               { return enumName(int32(s), bulkOperationStateNames) }
func (s BulkOperationState) MarshalJSON() ([]byte, error) { return encodeEnum(int32(s)) }
func (s *BulkOperationState) UnmarshalJSON(b []byte) error {
	n, err := decodeEnum(b, bulkOperationStateNames)
	*s = BulkOperationState(n)
	return err
}

// Finished reports whether the operation will not change any more.
func (s BulkOperationState) Finished() bool {
	return s == BulkOperationDone || s == BulkOperationFailed
}
