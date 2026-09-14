package vibecards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNilClientAnswersOnTheGroupSurfaceToo(t *testing.T) {
	var c *Client
	ctx := context.Background()
	if _, err := c.ListGroups(ctx, ListGroupsInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ListGroups: want ErrNotConfigured, got %v", err)
	}
	if _, err := c.CreateGroup(ctx, CreateGroupInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("CreateGroup: want ErrNotConfigured, got %v", err)
	}
	if _, err := c.GrantGroupAccess(ctx, GrantGroupAccessInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("GrantGroupAccess: want ErrNotConfigured, got %v", err)
	}
	if err := c.RevokeGroupAccess(ctx, RevokeGroupAccessInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("RevokeGroupAccess: want ErrNotConfigured, got %v", err)
	}
	if _, err := c.ListGroupGrants(ctx, ListGroupGrantsInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ListGroupGrants: want ErrNotConfigured, got %v", err)
	}
}

// capture stands up a server that records the one request it gets and answers
// with a fixed body.
func capture(t *testing.T, reply string) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotPath, &gotBody
}

// The owner filter is what lets a service key ask "which pool is this buyer's"
// — it must go out, and the enum + int64 dialects must survive both ways.
func TestListGroupsSendsOwnerAndDecodesTheServersDialect(t *testing.T) {
	srv, gotPath, gotBody := capture(t, `{"groups":[
		{"id":"g1","name":"buyer pool","ownerSub":"sub-buyer","ownerLabel":"Buyer B.",
		 "sharingMode":1,"defaultLimitCents":"50000","currency":2,"autoIssue":true,
		 "minAvailable":1,"maxTotal":5,"enabled":true},
		{"id":"g2","name":"old","sharingMode":"SHARING_MODE_SHARED","maxHolders":3,
		 "defaultLimitCents":"0","archivedAt":"2026-09-01T14:00:00Z"}
	],"stock":[{"groupId":"g1","free":1},{"groupId":"g2"}]}`)

	got, err := New(srv.URL, "vck_test").ListGroups(context.Background(), ListGroupsInput{
		OwnerSub: "sub-buyer", IncludeArchived: true,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if *gotPath != "/cards.v1.GroupService/ListGroups" {
		t.Fatalf("wrong path: %s", *gotPath)
	}
	if (*gotBody)["ownerSub"] != "sub-buyer" || (*gotBody)["includeArchived"] != true {
		t.Fatalf("wrong body: %#v", *gotBody)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 groups, got %d", len(got))
	}
	g := got[0]
	if g.OwnerSub != "sub-buyer" || g.OwnerLabel != "Buyer B." {
		t.Fatalf("owner did not survive: %+v", g)
	}
	if g.SharingMode != SharingModeExclusive || g.Currency != CurrencyEUR {
		t.Fatalf("numeric enums must decode to words: %+v", g)
	}
	if g.DefaultLimitCents != 50000 || !g.AutoIssue || g.MinAvailable != 1 || g.MaxTotal != 5 {
		t.Fatalf("sweeper policy did not survive: %+v", g)
	}
	if g.Archived() {
		t.Fatal("a live group must not read as archived")
	}
	if got[1].SharingMode != SharingModeShared || !got[1].Archived() {
		t.Fatalf("the named-enum, archived group decoded wrong: %+v", got[1])
	}
}

// Omitting the owner filter must not send an empty one: the server treats an
// absent owner as "whatever the caller may see", an empty string as a filter.
func TestListGroupsOmitsEmptyFilters(t *testing.T) {
	srv, _, gotBody := capture(t, `{"groups":[]}`)
	if _, err := New(srv.URL, "vck_test").ListGroups(context.Background(), ListGroupsInput{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(*gotBody) != 0 {
		t.Fatalf("an empty input must send an empty body, got %#v", *gotBody)
	}
}

// protojson wants int64 as a string and enums by their constant name; a
// request that sent a bare number for either would be refused or, worse for
// the limit, read as zero — and a zero default limit is a card that may spend
// the whole treasury.
func TestCreateGroupSpeaksProtojsonOnTheWayOut(t *testing.T) {
	srv, gotPath, gotBody := capture(t, `{"id":"g1","name":"buyer pool","ownerSub":"sub-buyer",
		"sharingMode":"SHARING_MODE_EXCLUSIVE","defaultLimitCents":"30000","enabled":true}`)

	got, err := New(srv.URL, "vck_test").CreateGroup(context.Background(), CreateGroupInput{
		Name:              "buyer pool",
		OwnerSub:          "sub-buyer",
		SharingMode:       SharingModeExclusive,
		DefaultLimitCents: 30000,
		AutoIssue:         true,
		MinAvailable:      1,
		MaxTotal:          5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if *gotPath != "/cards.v1.GroupService/CreateGroup" {
		t.Fatalf("wrong path: %s", *gotPath)
	}
	b := *gotBody
	if b["name"] != "buyer pool" || b["ownerSub"] != "sub-buyer" {
		t.Fatalf("identity did not go out: %#v", b)
	}
	if b["sharingMode"] != "SHARING_MODE_EXCLUSIVE" {
		t.Fatalf("sharing mode must go out as the proto name, got %#v", b["sharingMode"])
	}
	if b["defaultLimitCents"] != "30000" {
		t.Fatalf("defaultLimitCents must go out as a string, got %#v", b["defaultLimitCents"])
	}
	if b["autoIssue"] != true || b["minAvailable"] != float64(1) || b["maxTotal"] != float64(5) {
		t.Fatalf("sweeper policy did not go out: %#v", b)
	}
	if got.ID != "g1" || got.OwnerSub != "sub-buyer" || got.DefaultLimitCents != 30000 {
		t.Fatalf("created group decoded wrong: %+v", got)
	}
}

// A consumer can only ever grant a PERSON. The kind is fixed on the wire and
// not a field the caller can reach.
func TestGrantGroupAccessPinsTheSubjectKindToUser(t *testing.T) {
	srv, gotPath, gotBody := capture(t, `{"id":"gr1","groupId":"g1","subjectKind":1,
		"subjectId":"sub-assistant","subjectLabel":"Assistant A.","canDraw":false,
		"grantedBy":"sub-buyer","grantedAt":"2026-09-14T10:00:00Z"}`)

	got, err := New(srv.URL, "vck_test").GrantGroupAccess(context.Background(), GrantGroupAccessInput{
		GroupID: "g1", OwnerSub: "sub-buyer", SubjectID: "sub-assistant", CanDraw: false,
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if *gotPath != "/cards.v1.GroupService/GrantGroupAccess" {
		t.Fatalf("wrong path: %s", *gotPath)
	}
	b := *gotBody
	if b["subjectKind"] != "GRANT_SUBJECT_KIND_USER" {
		t.Fatalf("subject kind must be pinned to user, got %#v", b["subjectKind"])
	}
	if b["groupId"] != "g1" || b["ownerSub"] != "sub-buyer" || b["subjectId"] != "sub-assistant" {
		t.Fatalf("wrong body: %#v", b)
	}
	// canDraw=false is a real answer (read-only access), so it must be sent,
	// not omitted.
	if v, ok := b["canDraw"]; !ok || v != false {
		t.Fatalf("canDraw=false must be sent explicitly, got %#v", b)
	}
	if got.SubjectKind != GrantSubjectUser || got.SubjectLabel != "Assistant A." || !got.Live() {
		t.Fatalf("grant decoded wrong: %+v", got)
	}
}

func TestRevokeGroupAccessSendsIdOwnerAndReason(t *testing.T) {
	srv, gotPath, gotBody := capture(t, `{"revoked":true}`)

	err := New(srv.URL, "vck_test").RevokeGroupAccess(context.Background(), RevokeGroupAccessInput{
		GrantID: "gr1", OwnerSub: "sub-buyer", Reason: "assistant left",
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if *gotPath != "/cards.v1.GroupService/RevokeGroupAccess" {
		t.Fatalf("wrong path: %s", *gotPath)
	}
	b := *gotBody
	if b["id"] != "gr1" || b["ownerSub"] != "sub-buyer" || b["reason"] != "assistant left" {
		t.Fatalf("wrong body: %#v", b)
	}
}

func TestListGroupGrantsSendsFiltersAndKeepsRevokedRows(t *testing.T) {
	srv, gotPath, gotBody := capture(t, `{"grants":[
		{"id":"gr1","groupId":"g1","subjectKind":"GRANT_SUBJECT_KIND_USER","subjectId":"sub-a","canDraw":true},
		{"id":"gr2","groupId":"g1","subjectKind":2,"subjectId":"team-x","revokedAt":"2026-09-01T14:00:00Z","revokeReason":"done"}
	]}`)

	got, err := New(srv.URL, "vck_test").ListGroupGrants(context.Background(), ListGroupGrantsInput{
		GroupID: "g1", OwnerSub: "sub-buyer", IncludeRevoked: true,
	})
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if *gotPath != "/cards.v1.GroupService/ListGroupGrants" {
		t.Fatalf("wrong path: %s", *gotPath)
	}
	b := *gotBody
	if b["groupId"] != "g1" || b["ownerSub"] != "sub-buyer" || b["includeRevoked"] != true {
		t.Fatalf("wrong body: %#v", b)
	}
	if len(got) != 2 || !got[0].Live() || got[1].Live() {
		t.Fatalf("liveness decoded wrong: %+v", got)
	}
	if got[1].SubjectKind != GrantSubjectTeam || got[1].RevokeReason != "done" {
		t.Fatalf("revoked team grant decoded wrong: %+v", got[1])
	}
}

// The two answers a consumer branches on: somebody else's pool, and a pool
// that is gone. Both must arrive as the named errors, not as *APIError.
func TestGroupErrorsMapOntoTheNamedErrors(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"permission_denied", ErrForbidden},
		{"not_found", ErrNotFound},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"` + tc.code + `","message":"not your pool"}`))
		}))
		err := New(srv.URL, "vck_test").RevokeGroupAccess(context.Background(), RevokeGroupAccessInput{GrantID: "gr1"})
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.code, tc.want, err)
		}
	}
}
