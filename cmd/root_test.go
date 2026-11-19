package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runCLIEnv(t, stdin, nil, args...)
}

func runCLIEnv(t *testing.T, stdin string, env map[string]string, args ...string) (string, error) {
	t.Helper()
	// A config of its own, so the reader's settings never leak in.
	t.Setenv("TWIG_CONFIG", filepath.Join(t.TempDir(), "twig.json"))
	for name, value := range env {
		t.Setenv(name, value)
	}

	var out bytes.Buffer
	c := New()
	c.SetArgs(args)
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	err := c.Execute()
	return out.String(), err
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=twig", "-c", "user.email=twig@example.com"}, args...)
	c := exec.Command("git", full...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// patchRepo commits two files, changes both in the worktree, and chdirs into it.
func patchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "keep.txt", "one\ntwo\nthree\n")
	write(t, dir, "sub/nested.txt", "nested\n")
	runGit(t, dir, "init", "-q", ".")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-qm", "base")

	write(t, dir, "keep.txt", "one\nTWO\nthree\n")
	write(t, dir, "sub/nested.txt", "nested changed\n")
	t.Chdir(dir)
	return dir
}

func TestPatchPrintsTheDiffAndNoEscapes(t *testing.T) {
	patchRepo(t)

	out, err := runCLI(t, "", "--patch")
	if err != nil {
		t.Fatalf("--patch: %v", err)
	}
	for _, want := range []string{
		"diff --git a/keep.txt b/keep.txt",
		"-two",
		"+TWO",
		"diff --git a/sub/nested.txt b/sub/nested.txt",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("a pipe was handed escapes:\n%q", out)
	}
}

func TestExitCodeReportsWhetherThereIsADiff(t *testing.T) {
	patchRepo(t)

	if _, err := runCLI(t, "", "--patch", "--exit-code"); !errors.Is(err, errDiffFound) {
		t.Fatalf("--exit-code with a change = %v, want errDiffFound", err)
	}

	runGit(t, ".", "add", ".")
	if _, err := runCLI(t, "", "--patch", "--exit-code"); err != nil {
		t.Fatalf("--exit-code with nothing to show = %v, want nil", err)
	}
}

func TestRefShowsWhatThatCommitChanged(t *testing.T) {
	dir := patchRepo(t)
	runGit(t, dir, "add", "keep.txt")
	runGit(t, dir, "commit", "-qm", "second")

	out, err := runCLI(t, "", "HEAD", "--patch")
	if err != nil {
		t.Fatalf("twig HEAD: %v", err)
	}
	if !strings.Contains(out, "+TWO") {
		t.Fatalf("HEAD did not show the commit's change:\n%s", out)
	}
	if strings.Contains(out, "nested changed") {
		t.Fatalf("HEAD showed work that is not in the commit:\n%s", out)
	}
}

func TestPathsAfterTheDashNarrowTheDiff(t *testing.T) {
	patchRepo(t)

	out, err := runCLI(t, "", "--patch", "--", "sub/")
	if err != nil {
		t.Fatalf("twig --patch -- sub/: %v", err)
	}
	if !strings.Contains(out, "sub/nested.txt") {
		t.Fatalf("the pathspec dropped the file it named:\n%s", out)
	}
	if strings.Contains(out, "keep.txt") {
		t.Fatalf("the pathspec kept a file outside it:\n%s", out)
	}
}

func TestStagedShowsTheIndexAndNothingElse(t *testing.T) {
	dir := patchRepo(t)
	runGit(t, dir, "add", "keep.txt")

	out, err := runCLI(t, "", "--staged", "--patch")
	if err != nil {
		t.Fatalf("--staged: %v", err)
	}
	if !strings.Contains(out, "keep.txt") {
		t.Fatalf("the staged change is missing:\n%s", out)
	}
	if strings.Contains(out, "nested.txt") {
		t.Fatalf("--staged showed work that is not in the index:\n%s", out)
	}
}

func TestColorAlwaysPaintsTheTextOutput(t *testing.T) {
	patchRepo(t)

	out, err := runCLI(t, "", "--patch", "--color", "always")
	if err != nil {
		t.Fatalf("--color always: %v", err)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("--color always printed no colour:\n%q", out)
	}
}

func TestRefusalsReadLikeAPhrase(t *testing.T) {
	patchRepo(t)

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"one", "two", "three"}, "at most two refs"},
		{[]string{"--staged", "HEAD"}, "a staged view takes no ref"},
		{[]string{"nosuchref"}, "no such ref"},
		{[]string{"--color", "purple", "--patch"}, "unknown color mode"},
		{[]string{"--", ""}, "empty path after --"},
	} {
		_, err := runCLI(t, "", tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v, want it to say %q", tc.args, err, tc.want)
		}
	}
}

func TestOutsideARepositoryTheErrorNamesTheWayOut(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := runCLI(t, "", "--patch")
	if err == nil || !strings.Contains(err.Error(), "-C") {
		t.Fatalf("outside a repository = %v, want it to name -C", err)
	}
}

// TWIG_CONFIG has to name the file that is read, not only the one written.
func TestTwigConfigIsTheFileThatIsRead(t *testing.T) {
	dir := patchRepo(t)
	write(t, dir, "long.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
	runGit(t, dir, "add", "long.txt")
	runGit(t, dir, "commit", "-qm", "long")
	write(t, dir, "long.txt", "l1\nl2\nl3\nl4\nL5\nl6\nl7\nl8\nl9\nl10\n")

	cfgDir := t.TempDir()
	write(t, cfgDir, "twig.json", `{"context": 1}`)
	cfgPath := filepath.Join(cfgDir, "twig.json")

	out, err := runCLIEnv(t, "", map[string]string{"TWIG_CONFIG": cfgPath}, "--patch", "--", "long.txt")
	if err != nil {
		t.Fatalf("--patch with TWIG_CONFIG: %v", err)
	}
	if !strings.Contains(out, "@@ -4,3 +4,3 @@") {
		t.Fatalf("the configured context did not reach the diff:\n%s", out)
	}
}

func TestAConfigPathThatIsNotThereIsReported(t *testing.T) {
	patchRepo(t)

	missing := filepath.Join(t.TempDir(), "gone.json")
	_, err := runCLI(t, "", "--config", missing, "--patch")
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("--config missing = %v, want it to complain", err)
	}
}
