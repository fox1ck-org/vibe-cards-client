package vibecards

import "testing"

// The account console kept an account waiting on "the farmer adds the card"
// while the card behind its claim had been blocked and replaced. Unusable is
// the one word the holder acts on, and the replacement outranks everything.
func TestUnusableNamesTheReasonAndTheReplacementFirst(t *testing.T) {
	cases := []struct {
		name string
		a    *Assignment
		want string
	}{
		{"nil", nil, ""},
		{"active", &Assignment{CardStatus: CardStatusActive}, ""},
		{"paused is still usable later", &Assignment{CardStatus: CardStatusPaused}, ""},
		{"blocked", &Assignment{CardStatus: CardStatusBlocked}, "blocked"},
		{"closed", &Assignment{CardStatus: CardStatusClosed}, "closed"},
		{"unreadable outranks blocked", &Assignment{CardStatus: CardStatusBlocked, CardUnreadable: true}, "unreadable"},
		{"replaced outranks all", &Assignment{CardStatus: CardStatusBlocked, CardUnreadable: true, CardReplacedByCardID: "x"}, "replaced"},
	}
	for _, c := range cases {
		if got := c.a.Unusable(); got != c.want {
			t.Errorf("%s: Unusable() = %q, want %q", c.name, got, c.want)
		}
	}
}
