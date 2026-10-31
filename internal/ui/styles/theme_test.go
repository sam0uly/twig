package styles

import (
	"fmt"
	"image/color"
	"math"
	"testing"

	"charm.land/tone"

	"samouly.fun/twig/internal/syntax"
)

// relativeLuminance is the sRGB luminance the contrast ratio is built on.
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is the WCAG ratio between two colors.
func contrast(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := max(la, lb), min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// reference is the author's charmtone colorscheme, read across from
// ~/.config/nvim/colors/charmtone.lua. Each class takes the colour of the vim
// or treesitter group that means the same thing, and this table is what keeps
// the TUI and the editor painting the same picture.
var reference = map[syntax.Class]string{
	syntax.Keyword: "#7272FF", // Statement, @keyword
	// The colorscheme says #F1EFEF and charm.land/tone ships #F7F6FB for the
	// same Salt step; the Go palette wins, one shade brighter, no other name.
	syntax.Type:     "#F7F6FB", // Type, Structure, @type
	syntax.Function: "#4FBEFE", // Function, @function.call
	syntax.String:   "#FFB587", // String, @string
	syntax.Escape:   "#68FFD6", // @string.escape
	syntax.Number:   "#68FFD6", // Number, Float, @number
	syntax.Constant: "#68FFD6", // Constant, @constant
	syntax.Boolean:  "#4FBEFE", // Boolean, @boolean
	syntax.Key:      "#7272FF", // @property, @attribute, @tag.attribute
	syntax.Tag:      "#AD6EFF", // Tag, @tag
	syntax.Comment:  "#605F6B", // Comment, @comment
	syntax.Doc:      "#605F6B", // @comment.documentation
	syntax.Operator: "#FF7F90", // Operator, @operator
	syntax.Punct:    "#E8FE96", // Delimiter, punctuation
}

// TestSyntaxPaletteMatchesReference holds the theme to the colourscheme it
// comes from, so a change in either place shows up as a failing test rather
// than as two terminals that disagree.
func TestSyntaxPaletteMatchesReference(t *testing.T) {
	s := New()
	for class, want := range reference {
		got := s.Syntax(SyntaxContext, class).GetForeground()
		r, g, b, _ := got.RGBA()
		if hex := fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8); hex != want {
			t.Errorf("class %d is %s, the reference is %s", class, hex, want)
		}
	}
	if !s.Syntax(SyntaxContext, syntax.Boolean).GetBold() {
		t.Error("a boolean is not bold, and the reference sets @boolean in bold")
	}
}

// TestSyntaxContrast measures every token against the three backgrounds it
// lands on. The floor is 3:1, the level where nothing is invisible; the
// reference scheme deliberately keeps comments dimmer than that, so the
// measured pairs are logged and the table is left as designed.
func TestSyntaxContrast(t *testing.T) {
	s := New()
	backgrounds := map[string]color.Color{
		"context": tone.Pepper,
		"added":   tone.Spinach,
		"deleted": tone.Toast,
	}
	for class := syntax.Plain + 1; class < syntax.ClassCount; class++ {
		fg := s.Syntax(SyntaxContext, class).GetForeground()
		if fg == nil {
			continue
		}
		// A comment is decoration the scheme dims on purpose, so it is held to
		// the 2:1 floor of "visible" rather than the 3:1 floor of "readable".
		floor := 3.0
		if class == syntax.Comment || class == syntax.Doc {
			floor = 2
		}
		for name, bg := range backgrounds {
			got := contrast(fg, bg)
			t.Logf("class %2d on %-8s %.2f:1", class, name, got)
			if got < floor {
				t.Errorf("class %d on %s: %.2f:1 is below %.0f:1", class, name, got, floor)
			}
		}
	}
}

// TestWordStylesKeepTheDiffWash checks that the emphasised variant of a token
// keeps its own foreground and only adds the stronger background, which is what
// makes a changed word still read as a keyword.
func TestWordStylesKeepTheDiffWash(t *testing.T) {
	s := New()
	for _, kind := range []int{SyntaxContext, SyntaxAdd, SyntaxDel} {
		for class := syntax.Plain + 1; class < syntax.ClassCount; class++ {
			word := s.SyntaxWord(kind, class)
			if word.GetForeground() != s.Syntax(kind, class).GetForeground() {
				t.Fatalf("kind %d class %d: emphasis changed the foreground", kind, class)
			}
			// Word emphasis only exists on changed lines; a context row has
			// nothing to emphasise.
			if kind != SyntaxContext && !word.GetBold() {
				t.Fatalf("kind %d class %d: emphasis is not bold", kind, class)
			}
		}
	}

	// A word with no token of its own keeps the style the renderer used before
	// there was syntax colouring at all.
	if s.SyntaxWord(SyntaxAdd, syntax.Plain).GetForeground() != s.DiffAddWord().GetForeground() {
		t.Fatal("an unowned word lost the added-word colour")
	}
	if s.SyntaxWord(SyntaxDel, syntax.Plain).GetForeground() != s.DiffDelWord().GetForeground() {
		t.Fatal("an unowned word lost the deleted-word colour")
	}
}
