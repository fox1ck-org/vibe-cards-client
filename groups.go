package vibecards

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SharingMode is the one policy a group exists to carry: how many people may
// hold one of its cards at once.
type SharingMode string

const (
	SharingModeUnspecified SharingMode = ""
	// SharingModeExclusive — at most one live holder per card. The card returns
	// to stock when released, which is what makes reuse cheaper than issuing.
	SharingModeExclusive SharingMode = "exclusive"
	// SharingModeShared — several holders on one credit line. Cheapest per
	// subject, and a block or a chargeback hits all of them together.
	SharingModeShared SharingMode = "shared"
)

var sharingModeByNumber = map[int64]string{0: "", 1: "exclusive", 2: "shared"}

func (m *SharingMode) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "SHARING_MODE_", sharingModeByNumber)
	if err != nil {
		return fmt.Errorf("sharing mode: %w", err)
	}
	*m = SharingMode(v)
	return nil
}

// MarshalJSON sends the proto constant name, which protojson accepts on the way
// in regardless of how the server chooses to render enums on the way out.
func (m SharingMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(encodeEnum("SHARING_MODE_", string(m)))
}

// GrantSubjectKind is who a grant is made out to.
type GrantSubjectKind string

const (
	GrantSubjectUnspecified GrantSubjectKind = ""
	GrantSubjectUser        GrantSubjectKind = "user"
	GrantSubjectTeam        GrantSubjectKind = "team"
)

var grantSubjectKindByNumber = map[int64]string{0: "", 1: "user", 2: "team"}

func (k *GrantSubjectKind) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "GRANT_SUBJECT_KIND_", grantSubjectKindByNumber)
	if err != nil {
		return fmt.Errorf("grant subject kind: %w", err)
	}
	*k = GrantSubjectKind(v)
	return nil
}

func (k GrantSubjectKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(encodeEnum("GRANT_SUBJECT_KIND_", string(k)))
}

// encodeEnum is decodeEnum's mirror: the bare word becomes the proto constant
// name. "" is the UNSPECIFIED member, so a zero value goes out as a real name
// rather than an empty string protojson would refuse.
func encodeEnum(prefix, word string) string {
	if word == "" {
		word = "unspecified"
	}
	return prefix + strings.ToUpper(word)
}

// Group is a pool of cards: a bucket with a policy about sharing, which the
// sweeper keeps stocked and which people draw from by explicit grant.
//
// A group is not a tenant. There is one estate; ownership (OwnerSub) says
// whose pool this is — a buyer's own, funded from their provider link — as
// opposed to the estate's shared stock, where OwnerSub is empty.
type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// OwnerSub is the Keycloak sub of the person whose pool this is, empty
	// for an estate-wide pool.
	OwnerSub string `json:"ownerSub"`
	// OwnerLabel is the owner in words — the member roster's name or e-mail.
	OwnerLabel string `json:"ownerLabel"`

	SharingMode SharingMode `json:"sharingMode"`
	MaxHolders  int32       `json:"maxHolders"`

	// What the sweeper asks the provider for when it stocks this group.
	DefaultLimitCents int64    `json:"defaultLimitCents,string"`
	Currency          Currency `json:"currency"`
	PreferredBIN      string   `json:"preferredBin"`
	ProviderID        string   `json:"providerId"`
	BillingProfileID  string   `json:"billingProfileId"`
	// FundingExternalID is the provider-side account the pool spends from.
	// Empty falls back to the owner's own provider link.
	FundingExternalID string `json:"fundingExternalId"`

	// AutoIssue is whether the sweeper may spend to keep the group stocked;
	// MinAvailable how much free capacity it keeps, MaxTotal the ceiling that
	// stops a typo from becoming a fleet (0 means no ceiling).
	AutoIssue    bool  `json:"autoIssue"`
	MinAvailable int32 `json:"minAvailable"`
	MaxTotal     int32 `json:"maxTotal"`

	Enabled    bool       `json:"enabled"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  *time.Time `json:"createdAt,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// Archived reports whether the pool has been retired.
func (g *Group) Archived() bool { return g != nil && g.ArchivedAt != nil }

// GroupStock is what the pool board shows and what the sweeper decides on.
// Free counts CAPACITY, not cards: in a shared group one card with room for
// three holders and one live holder contributes two.
type GroupStock struct {
	GroupID  string `json:"groupId"`
	Cards    int32  `json:"cards"`
	Free     int32  `json:"free"`
	Held     int32  `json:"held"`
	Unusable int32  `json:"unusable"`
}

// GroupGrant is one subject's access to one pool. Drawing implies seeing; a
// grant with CanDraw off is read-only access to somebody else's pool.
type GroupGrant struct {
	ID          string           `json:"id"`
	GroupID     string           `json:"groupId"`
	SubjectKind GrantSubjectKind `json:"subjectKind"`
	// SubjectID is a Keycloak sub for a user, a team slug for a team.
	SubjectID string `json:"subjectId"`
	// SubjectLabel is the subject in words, filled on read.
	SubjectLabel string     `json:"subjectLabel"`
	CanDraw      bool       `json:"canDraw"`
	GrantedBy    string     `json:"grantedBy"`
	GrantedAt    *time.Time `json:"grantedAt,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	RevokedBy    string     `json:"revokedBy"`
	RevokeReason string     `json:"revokeReason"`
}

// Live reports whether the grant still opens the pool.
func (g *GroupGrant) Live() bool { return g != nil && g.RevokedAt == nil }

// ListGroupsInput narrows a groups listing. All fields are optional.
type ListGroupsInput struct {
	IncludeArchived bool `json:"includeArchived,omitempty"`
	// OwnerSub restricts the answer to one person's own pools. A service key
	// acting for a buyer passes the buyer's sub here.
	OwnerSub string `json:"ownerSub,omitempty"`
}

// ListGroups returns the pools the caller may see: an admin all of them, a
// service key those of the owner it names, anybody else the ones they hold a
// grant on or own.
func (c *Client) ListGroups(ctx context.Context, in ListGroupsInput) ([]Group, error) {
	var out struct {
		Groups []Group `json:"groups"`
	}
	if err := c.call(ctx, "/cards.v1.GroupService/ListGroups", in, &out, false); err != nil {
		return nil, err
	}
	return out.Groups, nil
}

// CreateGroupInput opens a pool.
type CreateGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// OwnerSub makes this somebody's own pool rather than the estate's. A
	// service key creating a pool on a buyer's behalf names the buyer here.
	OwnerSub    string      `json:"ownerSub,omitempty"`
	SharingMode SharingMode `json:"sharingMode"`
	// MaxHolders is ignored unless SharingMode is shared, where it must be
	// at least 2.
	MaxHolders        int32 `json:"maxHolders,omitempty"`
	DefaultLimitCents int64 `json:"defaultLimitCents,omitempty,string"`
	AutoIssue         bool  `json:"autoIssue,omitempty"`
	MinAvailable      int32 `json:"minAvailable,omitempty"`
	MaxTotal          int32 `json:"maxTotal,omitempty"`
}

// CreateGroup opens a pool. Provider, billing profile and funding source are
// deliberately not exposed here: an owner's pool is funded from the owner's
// own provider link, and the estate's pools are set up in the console.
func (c *Client) CreateGroup(ctx context.Context, in CreateGroupInput) (*Group, error) {
	var out Group
	if err := c.call(ctx, "/cards.v1.GroupService/CreateGroup", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// GrantGroupAccessInput lets one person into a pool.
type GrantGroupAccessInput struct {
	GroupID string `json:"groupId"`
	// OwnerSub is whose pool this is, for a service key acting on the owner's
	// behalf. The server checks the pool is theirs.
	OwnerSub string `json:"ownerSub,omitempty"`
	// SubjectID is the Keycloak sub of the person let in.
	SubjectID string `json:"subjectId"`
	CanDraw   bool   `json:"canDraw"`
}

// grantGroupAccessRequest is GrantGroupAccessInput on the wire. The subject
// kind is fixed to a user: a team grant is a console decision, not a
// consumer's.
type grantGroupAccessRequest struct {
	GrantGroupAccessInput
	SubjectKind GrantSubjectKind `json:"subjectKind"`
}

// GrantGroupAccess lets one person into a pool. Re-granting an existing live
// grant updates it rather than adding a second.
func (c *Client) GrantGroupAccess(ctx context.Context, in GrantGroupAccessInput) (*GroupGrant, error) {
	var out GroupGrant
	req := grantGroupAccessRequest{GrantGroupAccessInput: in, SubjectKind: GrantSubjectUser}
	if err := c.call(ctx, "/cards.v1.GroupService/GrantGroupAccess", req, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeGroupAccessInput ends a grant. The row is kept: "who could draw this
// card in March" is asked after money goes missing.
type RevokeGroupAccessInput struct {
	GrantID  string `json:"id"`
	OwnerSub string `json:"ownerSub,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// RevokeGroupAccess ends a grant.
func (c *Client) RevokeGroupAccess(ctx context.Context, in RevokeGroupAccessInput) error {
	return c.call(ctx, "/cards.v1.GroupService/RevokeGroupAccess", in, nil, false)
}

// ListGroupGrantsInput asks who may use a pool.
type ListGroupGrantsInput struct {
	GroupID        string `json:"groupId"`
	OwnerSub       string `json:"ownerSub,omitempty"`
	IncludeRevoked bool   `json:"includeRevoked,omitempty"`
}

// ListGroupGrants answers "who may draw from this pool". Revoked grants are a
// deliberate read, off by default.
func (c *Client) ListGroupGrants(ctx context.Context, in ListGroupGrantsInput) ([]GroupGrant, error) {
	var out struct {
		Grants []GroupGrant `json:"grants"`
	}
	if err := c.call(ctx, "/cards.v1.GroupService/ListGroupGrants", in, &out, false); err != nil {
		return nil, err
	}
	return out.Grants, nil
}
