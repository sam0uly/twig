package git

import "testing"

func TestSourceMatchesPathspec(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		path  string
		want  bool
	}{
		{name: "no pathspec takes everything", path: "src/a.go", want: true},
		{name: "an exact path", paths: []string{"src/a.go"}, path: "src/a.go", want: true},
		{name: "a different file", paths: []string{"src/a.go"}, path: "src/b.go", want: false},
		{name: "a directory takes its children", paths: []string{"src"}, path: "src/deep/b.go", want: true},
		{name: "a trailing slash is the same path", paths: []string{"src/"}, path: "src/a.go", want: true},
		{name: "a prefix that is not a directory", paths: []string{"src"}, path: "srcsibling.go", want: false},
		{name: "one of several", paths: []string{"docs", "src"}, path: "src/a.go", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := Source{Paths: tt.paths}
			if got := src.Matches(tt.path); got != tt.want {
				t.Fatalf("Matches(%q) with paths %v = %v, want %v", tt.path, tt.paths, got, tt.want)
			}
		})
	}
}
