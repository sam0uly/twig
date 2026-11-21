package git

import "fmt"

// RepoRoot is the work tree root of the repository twig was started in.
func RepoRoot() (string, error) {
	rc, err := open()
	if err != nil {
		return "", err
	}
	return rc.root, nil
}

// GitDir is the repository's own directory.
func GitDir() (string, error) {
	rc, err := open()
	if err != nil {
		return "", err
	}
	if rc.gitDir == "" {
		return "", fmt.Errorf("%w: no git directory", ErrNotARepository)
	}
	return rc.gitDir, nil
}
