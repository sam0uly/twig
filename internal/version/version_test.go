package version

import (
	"strings"
	"testing"
)

func TestStringAlwaysHasName(t *testing.T) {
	if s := String(); !strings.HasPrefix(s, "twig ") {
		t.Errorf("String() = %q, want a twig prefix", s)
	}
}

func TestStringUsesInjectedVersion(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })

	Version, Commit = "1.2.3", ""
	if got, want := String(), "twig 1.2.3"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	Version, Commit = "1.2.3", "0000000000000000000000000000000000000000"
	got := String()
	if !strings.HasPrefix(got, "twig 1.2.3 (") || !strings.Contains(got, "000000") {
		t.Errorf("String() = %q, want the short commit next to the version", got)
	}
}

func TestStringFallsBackToDev(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })

	Version, Commit = "", "none"
	if got, want := String(), "twig dev"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
