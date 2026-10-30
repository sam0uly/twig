package git

import (
	"os"
	"path/filepath"
)

// WatchDirs lists the directories whose changes can alter the files view: the
// git directory's own state, the work tree root, the directory holding every
// listed path, and the directory of the file on screen. Events anywhere else
// cannot change what twig shows, and watching the whole tree would cost one
// watch per directory.
func WatchDirs(paths []string) []string {
	rc, err := open()
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(paths)+8)
	dirs := make([]string, 0, len(paths)+8)
	add := func(dir string) {
		if dir == "" || dir == "." {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	add(rc.root)
	add(rc.gitDir)
	// refs and logs nest (refs/heads/topic/x), and a ref update that touches
	// neither the index nor HEAD would otherwise go unnoticed until a poll
	for _, sub := range []string{"refs", "logs"} {
		walkDirs(filepath.Join(rc.gitDir, sub), 3, add)
	}
	if common := rc.commonDir(); common != "" {
		add(common)
		walkDirs(filepath.Join(common, "refs"), 3, add)
		walkDirs(filepath.Join(common, "logs"), 3, add)
	}
	for _, p := range paths {
		add(filepath.Dir(filepath.Join(rc.root, filepath.FromSlash(p))))
	}
	return dirs
}

// WatchFiles lists the files whose contents matter: every listed path plus the
// git files that move when the repository does. Only platforms where a
// directory watch cannot see a content write use these; the rest ignore the
// list, which is why callers can pass it everywhere.
func WatchFiles(paths []string) []string {
	rc, err := open()
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(paths)+8)
	files := make([]string, 0, len(paths)+8)
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	// the names git itself rewrites during the operations a viewer cares about
	for _, dir := range []string{rc.gitDir, rc.commonDir()} {
		if dir == "" {
			continue
		}
		for _, name := range []string{
			"index", "HEAD", "packed-refs", "ORIG_HEAD",
			"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG",
		} {
			add(filepath.Join(dir, name))
		}
	}
	for _, p := range paths {
		add(filepath.Join(rc.root, filepath.FromSlash(p)))
	}
	return files
}

// walkDirs adds every directory under root up to depth levels, stopping at the
// first level that does not exist. Missing subtrees are normal: a fresh
// repository has no logs, and a packed repository has no refs/heads.
func walkDirs(root string, depth int, add func(string)) {
	if depth < 0 {
		return
	}
	add(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		walkDirs(filepath.Join(root, name), depth-1, add)
	}
}
