package buildinfo

import "testing"

func TestStringWithInjectedValues(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = originalVersion, originalCommit })

	tests := []struct {
		version string
		commit  string
		want    string
	}{
		{"v1.2.3", "abc1234", "v1.2.3+abc1234"},
		{"v1.2.3", "", "v1.2.3"},
		{"", "abc1234", "abc1234"},
	}
	for _, test := range tests {
		Version = test.version
		Commit = test.commit
		if got := String(); got != test.want {
			t.Errorf("String() with %q/%q = %q, want %q", test.version, test.commit, got, test.want)
		}
	}
}

func TestIsPseudoVersion(t *testing.T) {
	if !isPseudoVersion("v0.0.0-20260101120000-abcdef123456") {
		t.Error("pseudo-version not recognized")
	}
	if isPseudoVersion("v1.2.3") {
		t.Error("release version recognized as pseudo-version")
	}
}

func TestShortCommit(t *testing.T) {
	if got := shortCommit("abcdef1234567890"); got != "abcdef1" {
		t.Errorf("shortCommit() = %q, want abcdef1", got)
	}
	if got := shortCommit("abc"); got != "abc" {
		t.Errorf("shortCommit(abc) = %q", got)
	}
}
