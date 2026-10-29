package diff

import (
	"math/rand"
	"strings"
	"testing"
)

func lcsLen(a, b []string) int {
	prev := make([]int, len(b)+1)
	for i := len(a) - 1; i >= 0; i-- {
		cur := make([]int, len(b)+1)
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				cur[j] = prev[j+1] + 1
			} else {
				cur[j] = max(prev[j], cur[j+1])
			}
		}
		prev = cur
	}
	return prev[0]
}

func TestMyersMinimal(t *testing.T) {
	pool := []string{"a", "b", "c", "", "x", "y", "z"}
	r := rand.New(rand.NewSource(3))
	for it := 0; it < 4000; it++ {
		gen := func() []string {
			n := r.Intn(30)
			out := make([]string, n)
			for i := range out {
				out[i] = pool[r.Intn(len(pool))]
			}
			return out
		}
		a, b := gen(), gen()
		ia, ib := lineIDs(a, b)
		es, ok := myersEdits(ia, ib, maxMyersD)
		if !ok {
			t.Fatalf("myers gave up on %d/%d lines", len(a), len(b))
		}
		changed := 0
		for _, e := range es {
			if e.kind != editEqual {
				changed++
			}
		}
		want := len(a) + len(b) - 2*lcsLen(a, b)
		if changed != want {
			t.Fatalf("it=%d: changed=%d, minimal=%d (a=%d b=%d)", it, changed, want, len(a), len(b))
		}
	}
}

func BenchmarkGenerateLarge(b *testing.B) {
	var oldB strings.Builder
	for i := 0; i < 5000; i++ {
		oldB.WriteString("func line")
		oldB.WriteString(strings.Repeat("x", i%5))
		oldB.WriteString("() { return nil }\n")
	}
	oldText := oldB.String()
	newText := strings.Replace(oldText, "return nil", "return err", 400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Generate("f", Modified, oldText, newText, GenerateOptions{Context: 3})
	}
}
