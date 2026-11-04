package git

// writeBlob stores data as a loose object and returns the id git knows it by.
// The bytes are content addressed, so the write is safe by construction; git
// performs it, which keeps the object directory, the repository's hash and any
// configured alternates in one pair of hands, and keeps the deflate encoder out
// of the binary. No --path is passed, so no clean filter is applied: callers
// hand over bytes that are already in the form the index stores.
func (rc *repoContext) writeBlob(data []byte) (string, error) {
	out, _, err := runGitWrite(rc.root, data, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return string(trimSpace(out)), nil
}

// trimSpace trims ASCII whitespace without dragging strings into a file that
// otherwise only needs bytes.
func trimSpace(s string) []byte {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\r' || s[j-1] == '\t') {
		j--
	}
	return []byte(s[i:j])
}
