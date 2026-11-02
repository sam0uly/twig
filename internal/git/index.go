package git

import "errors"

// indexRecord is one path's entry as the index holds it.
type indexRecord struct {
	// Path is relative to the work tree root, slash separated.
	Path string
	// Mode is the octal file mode, e.g. "100644" or "100755". A symlink is
	// "120000"; a submodule is "160000".
	Mode string
	// OID is the blob the entry points at.
	OID string
}

// setIndexBlob records a path at a blob in the index. This is the one
// byte-level write in partial staging that twig does not implement itself:
// git takes the index lock, writes, and replaces the file atomically, so a
// reader never sees a half written index and a second writer fails fast
// instead of clobbering. Everything that decides *what* to write is ours.
func setIndexBlob(rec indexRecord, add bool) error {
	rc, err := open()
	if err != nil {
		return err
	}
	args := []string{"update-index"}
	if add {
		args = append(args, "--add")
	}
	args = append(args, "--cacheinfo", rec.Mode+","+rec.OID+","+rec.Path)
	if _, _, err := runGitWrite(rc.root, nil, args...); err != nil {
		return err
	}
	invalidate()
	return nil
}

// removeIndexPath drops a path from the index without touching the worktree.
// --force-remove is what makes it work for a path that still exists on disk,
// which is the state an undo of a newly added file needs to reach.
func removeIndexPath(path string) error {
	rc, err := open()
	if err != nil {
		return err
	}
	if _, _, err := runGitWrite(rc.root, nil, "update-index", "--force-remove", "--", path); err != nil {
		return err
	}
	invalidate()
	return nil
}

// ErrRefusedPartial is wrapped by every refusal to stage part of a file, so
// callers can tell "this file is not eligible" from "the write failed".
var ErrRefusedPartial = errors.New("partial staging not available for this file")

// ErrNoChange is wrapped when a path has no diff on the side that was asked
// for. It is not a failure: the last change on that side is gone, which is what
// staging the last line of a file leaves behind.
var ErrNoChange = errors.New("no change")
