package vibecards

import "testing"

func TestAssignmentRemaining(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		a     *Assignment
		want  string
		wantK bool
	}{
		{"nil", nil, "", false},
		{"no ceiling", &Assignment{CardSpent: "12.00"}, "", false},
		{"unspent", &Assignment{CardLimit: "150", CardSpent: ""}, "150.00", true},
		{"partly spent", &Assignment{CardLimit: "150.00", CardSpent: "84.20"}, "65.80", true},
		{"overspent clamps", &Assignment{CardLimit: "150.00", CardSpent: "151.10"}, "0.00", true},
		{"unparseable spend", &Assignment{CardLimit: "150.00", CardSpent: "n/a"}, "", false},
	}
	for _, tc := range cases {
		got, ok := tc.a.Remaining()
		if got != tc.want || ok != tc.wantK {
			t.Errorf("%s: Remaining() = %q,%v want %q,%v", tc.name, got, ok, tc.want, tc.wantK)
		}
	}
}
