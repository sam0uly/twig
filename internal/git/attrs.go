package git

import (
	"strings"
)

// attributes is what git would do to a path's bytes on the way from the
// worktree into the index. Partial staging has to know, because the bytes on
// disk are not always the bytes git stores.
type attributes struct {
	// Filtered means a clean/smudge filter is configured for the path. Its
	// transformation is opaque, so twig refuses partial staging for it rather
	// than writing bytes git would never have written.
	Filtered bool
	// Text means git normalises line endings for the path, so the worktree
	// text is converted to the index form before it is compared or blended.
	Text bool
}

// attributesFor returns the path's conversion rules, cached. A file that is
// not under any attribute rule still converts when core.autocrlf is on, which
// is why the config is consulted as a fallback.
func (rc *repoContext) attributesFor(path string) (attributes, error) {
	rc.attrsMu.Lock()
	if a, ok := rc.attrs[path]; ok {
		rc.attrsMu.Unlock()
		return a, nil
	}
	rc.attrsMu.Unlock()

	a, err := rc.readAttributes(path)
	if err != nil {
		return attributes{}, err
	}

	rc.attrsMu.Lock()
	if rc.attrs == nil {
		rc.attrs = make(map[string]attributes)
	}
	rc.attrs[path] = a
	rc.attrsMu.Unlock()
	return a, nil
}

// readAttributes parses git's own answer for one path. The -z form keeps
// attribute values that contain spaces (a filter command often does) intact:
// path, attribute and value arrive as three NUL-terminated fields.
func (rc *repoContext) readAttributes(path string) (attributes, error) {
	out, err := rc.git("check-attr", "-z", "--all", "--", path)
	if err != nil {
		return attributes{}, err
	}

	var a attributes
	var textSet, textUnset bool
	fields := strings.Split(out, "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		name, value := fields[i+1], fields[i+2]
		switch name {
		case "filter":
			if value != "unspecified" && value != "unset" && value != "" {
				a.Filtered = true
			}
		case "text":
			switch value {
			case "set", "true", "auto", "input":
				textSet = true
			case "unset", "false":
				textUnset = true
			}
		case "eol":
			if value == "lf" || value == "crlf" {
				textSet = true
			}
		case "crlf":
			switch value {
			case "set", "true", "input":
				textSet = true
			case "unset", "false":
				textUnset = true
			}
		}
	}
	if textUnset {
		return a, nil
	}
	if textSet {
		a.Text = true
		return a, nil
	}

	// No attribute decided it, so the config does. A repository wide autocrlf
	// turns every text file into a converting one.
	autocrlf := rc.coreAutoCRLF()
	a.Text = autocrlf == "true" || autocrlf == "input"
	return a, nil
}

// coreAutoCRLF reads the config once: it is a per repository constant and the
// answer is needed for every file that has no attributes of its own. A missing
// key reads as an empty string, which means no conversion.
func (rc *repoContext) coreAutoCRLF() string {
	rc.attrsMu.Lock()
	if rc.autocrlfSet {
		v := rc.autocrlf
		rc.attrsMu.Unlock()
		return v
	}
	rc.attrsMu.Unlock()

	out, err := rc.git("config", "--get", "core.autocrlf")
	if err != nil {
		out = ""
	}
	v := strings.TrimSpace(out)

	rc.attrsMu.Lock()
	rc.autocrlf, rc.autocrlfSet = v, true
	rc.attrsMu.Unlock()
	return v
}

// toIndexForm converts worktree bytes into the form the index stores. Only CRLF
// pairs are touched, which is exactly what git's own text conversion does, and
// it is a no-op for a file that already uses LF.
func toIndexForm(s string) string {
	if !strings.Contains(s, "\r\n") {
		return s
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}
