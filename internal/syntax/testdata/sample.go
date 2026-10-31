// Package sample exists to give the syntax tests something real to chew on:
// a block comment, a raw string and a keyword all crossing a line boundary.
package sample

const greeting = `hello
world`

/* a block comment
   that survives two lines */

func Handle(name string, count int) error {
	if count < 0 {
		return fmt.Errorf("bad count %d for %q", count, name)
	}
	total := 0x1f + 3.5e2 + 'x'
	_ = total
	// done
	return nil
}

var shout = "a // not a comment"

var quoted = "she said \"hi\""

type Thing struct {
	Name  string
	Value int32
}
