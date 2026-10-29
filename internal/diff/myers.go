package diff

// Myers' O(ND) difference algorithm in its linear-space form.
//
// The greedy variant finds a shortest edit script in O((N+M)·D) time but has
// to remember every edit-distance frontier, which costs O(D²) memory. The
// linear-space refinement locates a "middle snake" that a shortest path must
// pass through, splits there and recurses: memory becomes O(N+M) per level and
// the result is still minimal.
//
// Lines are interned to dense int32 ids first, so the inner loop compares
// integers instead of strings (the same trick git's xdiff uses). Regions whose
// edit distance exceeds the caller's cap are split on anchored lines by the
// caller instead of degrading to one giant replacement.

// maxMyersD caps the edit distance of a single region. Past it the caller
// splits on unique lines, which keeps worst-case time and memory bounded.
const maxMyersD = 1024

// myersBudget bounds the O((N+M)·D) work a single run may spend before the
// caller falls back to anchoring. Real edits of real files sit far below it;
// random or wholesale-rewritten input hits the cap and is split instead.
const myersBudget = 300_000

// myersCap scales the allowed edit distance with the region size, the way
// xdiff limits its own cost: small regions get the full minimal script, huge
// ones switch to anchoring early instead of burning quadratic time.
func myersCap(n, m int) int {
	return max(64, min(maxMyersD, myersBudget/(n+m+1)))
}

// lineIDs interns the lines of both inputs into dense integer ids.
func lineIDs(a, b []string) ([]int32, []int32) {
	ids := make(map[string]int32, len(a)+len(b))
	get := func(s string) int32 {
		if id, ok := ids[s]; ok {
			return id
		}
		id := int32(len(ids))
		ids[s] = id
		return id
	}
	x := make([]int32, len(a))
	for i, s := range a {
		x[i] = get(s)
	}
	y := make([]int32, len(b))
	for i, s := range b {
		y[i] = get(s)
	}
	return x, y
}

// myersEdits returns the minimal edit script between a and b, or false when the
// edit distance exceeds maxD. Indexes are relative to a and b.
func myersEdits(a, b []int32, maxD int) ([]edit, bool) {
	if len(a) == 0 && len(b) == 0 {
		return nil, true
	}
	es := make([]edit, 0, len(a)+len(b))
	if !diffRange(a, b, 0, len(a), 0, len(b), maxD, &es) {
		return nil, false
	}
	return es, true
}

// diffRange appends the edit script of a[alo:ahi] against b[blo:bhi], splitting
// on the middle snake and recursing. It reports false when the region needs
// more than maxD edits, leaving the caller to anchor.
func diffRange(a, b []int32, alo, ahi, blo, bhi, maxD int, es *[]edit) bool {
	for alo < ahi && blo < bhi && a[alo] == b[blo] {
		*es = append(*es, edit{kind: editEqual, oldIdx: int32(alo), newIdx: int32(blo)})
		alo++
		blo++
	}
	suffix := 0
	for alo < ahi && blo < bhi && a[ahi-1] == b[bhi-1] {
		ahi--
		bhi--
		suffix++
	}

	switch {
	case alo == ahi:
		for ; blo < bhi; blo++ {
			*es = append(*es, edit{kind: editInsert, oldIdx: int32(alo), newIdx: int32(blo)})
		}
	case blo == bhi:
		for ; alo < ahi; alo++ {
			*es = append(*es, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
		}
	default:
		x, y, ok := middleSnake(a, b, alo, ahi, blo, bhi, maxD)
		if !ok {
			return false
		}
		if (x == alo && y == blo) || (x == ahi && y == bhi) {
			// A split that does not shrink the region would loop forever.
			// Once the common prefix and suffix are trimmed this cannot
			// happen, but the guard keeps the recursion safe.
			for ; alo < ahi; alo++ {
				*es = append(*es, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
			}
			for ; blo < bhi; blo++ {
				*es = append(*es, edit{kind: editInsert, oldIdx: int32(ahi), newIdx: int32(blo)})
			}
			break
		}
		if !diffRange(a, b, alo, x, blo, y, maxD, es) {
			return false
		}
		if !diffRange(a, b, x, ahi, y, bhi, maxD, es) {
			return false
		}
	}

	for i := 0; i < suffix; i++ {
		*es = append(*es, edit{kind: editEqual, oldIdx: int32(ahi + i), newIdx: int32(bhi + i)})
	}
	return true
}

// middleSnake runs the bidirectional search and returns a point on a shortest
// edit path for the region: the start of the snake the forward path matched
// last. Both subproblems are then strictly smaller than the region.
func middleSnake(a, b []int32, alo, ahi, blo, bhi, maxD int) (int, int, bool) {
	n := ahi - alo
	m := bhi - blo
	delta := n - m
	odd := delta&1 != 0

	limit := (n + m + 1) / 2
	if limit > maxD {
		limit = maxD
	}
	size := 2*limit + 3
	off := limit + 1

	vf := make([]int, size)
	vb := make([]int, size)
	for i := range vf {
		vf[i] = -1
		vb[i] = -1
	}
	vf[off+1] = 0
	vb[off+1] = 0

	for d := 0; d <= limit; d++ {
		// Forward: furthest reaching d-path from the start, per diagonal.
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vf[off+k-1] < vf[off+k+1]) {
				x = vf[off+k+1]
			} else {
				x = vf[off+k-1] + 1
			}
			if x < 0 {
				continue
			}
			y := x - k
			for x < n && y < m && a[alo+x] == b[blo+y] {
				x++
				y++
			}
			vf[off+k] = x

			if odd {
				// The backward frontier is one round behind, on the mirrored
				// diagonal delta-k. Overlap means the two halves meet.
				kb := delta - k
				if kb >= -(d-1) && kb <= d-1 && vb[off+kb] >= 0 && x+vb[off+kb] >= n {
					sx, sy := snakeStart(a, b, alo, blo, x, y)
					return sx, sy, true
				}
			}
		}

		// Backward: furthest reaching d-path from the end, run on the reversed
		// sequences, which is the same recurrence mirrored.
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && vb[off+k-1] < vb[off+k+1]) {
				x = vb[off+k+1]
			} else {
				x = vb[off+k-1] + 1
			}
			if x < 0 {
				continue
			}
			y := x - k
			for x < n && y < m && a[ahi-1-x] == b[bhi-1-y] {
				x++
				y++
			}
			vb[off+k] = x

			if !odd {
				kf := delta - k
				if kf >= -d && kf <= d && vf[off+kf] >= 0 && vf[off+kf]+x >= n {
					fx := vf[off+kf]
					sx, sy := snakeStart(a, b, alo, blo, fx, fx-kf)
					return sx, sy, true
				}
			}
		}
	}

	return 0, 0, false
}

// snakeStart walks back over the diagonal run ending at (x, y), so the returned
// point sits at the start of a snake that a shortest path must contain.
func snakeStart(a, b []int32, alo, blo, x, y int) (int, int) {
	for x > 0 && y > 0 && a[alo+x-1] == b[blo+y-1] {
		x--
		y--
	}
	return alo + x, blo + y
}
