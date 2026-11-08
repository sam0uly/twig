package ui

import (
	"image/color"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/x/ansi"
)

func advancePlayground(p *pixelPlayground, ticks int) {
	for range ticks {
		p.Tick()
	}
}

func TestPlaygroundHarmonicaSpringIntegration(t *testing.T) {
	p := newPixelPlayground()
	if !p.Update(keyPress("l")) {
		t.Fatal("right-wind key was not consumed")
	}

	// Independently integrate Harmonica's documented v0.2 API and compare the
	// actual bonsai state after each welcome tick. This pins both the target and
	// the fact that Tick, rather than View, advances the spring.
	spring := harmonica.NewSpring(welcomeFrameInterval.Seconds(), playgroundWindFrequency, playgroundWindDamping)
	position, velocity := p.trunk.position, p.trunk.velocity
	first := position
	for tick := range 8 {
		position, velocity = spring.Update(position, velocity, 1.15)
		p.Tick()
		if p.trunk.position != position || p.trunk.velocity != velocity {
			t.Fatalf("tick %d trunk = (%0.12f, %0.12f), Harmonica expected (%0.12f, %0.12f)",
				tick+1, p.trunk.position, p.trunk.velocity, position, velocity)
		}
	}
	if position == first || velocity == 0 {
		t.Fatal("Harmonica updates did not move the trunk")
	}
	if p.boughs[0].position == 0 || p.boughs[0].position >= p.trunk.position {
		t.Fatalf("bough spring did not chase the trunk: trunk=%f bough=%f", p.trunk.position, p.boughs[0].position)
	}
	if p.canopies[0].position == 0 || p.canopies[0].position >= p.boughs[0].position {
		t.Fatalf("canopy spring did not chase its bough: bough=%f canopy=%f", p.boughs[0].position, p.canopies[0].position)
	}
	if p.boughs[0].position == p.boughs[1].position || p.canopies[0] == p.canopies[1] {
		t.Fatal("independent bough/canopy springs collapsed into rigid-tree motion")
	}
}

func TestPlaygroundTicksAreDeterministicAndIndependent(t *testing.T) {
	first, second := newPixelPlayground(), newPixelPlayground()
	for _, p := range []*pixelPlayground{&first, &second} {
		for _, key := range []string{"b", "l", "c", "m"} {
			p.Update(keyPress(key))
		}
	}
	for range 90 {
		first.Tick()
		second.Tick()
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("same controls and springs diverged at tick %d\nfirst:  %+v\nsecond: %+v", first.tick, first, second)
		}
	}

	original := newPixelPlayground()
	originalSnapshot := original
	copy := original
	copy.Update(keyPress("b"))
	copy.Update(keyPress("c"))
	copy.Update(keyPress("r")) // deliberately not a bonsai key
	advancePlayground(&copy, 20)
	if !reflect.DeepEqual(original, originalSnapshot) {
		t.Fatal("mutating a model copy changed the original springs or sprite state")
	}
	if copy.sprouts != 1 || copy.palette != 1 || copy.tick != 20 {
		t.Fatalf("copied model did not advance independently: sprouts=%d palette=%d tick=%d", copy.sprouts, copy.palette, copy.tick)
	}
}

func TestPlaygroundWindLeftRightAndBounds(t *testing.T) {
	for _, tt := range []struct {
		name string
		msg  tea.KeyPressMsg
		want float64
	}{
		{name: "h moves left", msg: keyPress("h"), want: -1},
		{name: "left arrow", msg: tea.KeyPressMsg{Code: tea.KeyLeft}, want: -1},
		{name: "l moves right", msg: keyPress("l"), want: 1},
		{name: "right arrow", msg: tea.KeyPressMsg{Code: tea.KeyRight}, want: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPixelPlayground()
			before := ansi.Strip(strings.Join(p.renderRaster(63, 34).rows(0), "\n"))
			if !p.Update(tt.msg) {
				t.Fatal("wind key was not consumed")
			}
			advancePlayground(&p, 7)
			if tt.want < 0 && p.trunk.position >= 0 {
				t.Fatalf("left wind produced trunk position %f", p.trunk.position)
			}
			if tt.want > 0 && p.trunk.position <= 0 {
				t.Fatalf("right wind produced trunk position %f", p.trunk.position)
			}
			after := ansi.Strip(strings.Join(p.renderRaster(63, 34).rows(0), "\n"))
			if before == after {
				t.Fatal("wind spring did not change the visible pixel raster")
			}
		})
	}

	p := newPixelPlayground()
	for range 20 {
		p.Update(keyPress("l"))
	}
	if p.wind != playgroundMaxWind {
		t.Fatalf("right wind = %d, want cap %d", p.wind, playgroundMaxWind)
	}
	for range 40 {
		p.Update(keyPress("h"))
	}
	if p.wind != -playgroundMaxWind {
		t.Fatalf("left wind = %d, want cap %d", p.wind, -playgroundMaxWind)
	}
}

func TestPlaygroundGrowthPalettePruneAndReplant(t *testing.T) {
	p := newPixelPlayground()
	initialArt := ansi.Strip(strings.Join(p.renderRaster(63, 34).rows(p.palette), "\n"))
	p.Update(keyPress("b"))
	if p.sprouts != 1 || p.growth[0].position != 0 {
		t.Fatal("a sprout did not start at zero spring growth")
	}
	advancePlayground(&p, 14)
	if p.growth[0].position <= 0 || p.growth[0].position > 1.1 {
		t.Fatalf("first sprout is not visibly springing toward full size: %f", p.growth[0].position)
	}
	grownArt := ansi.Strip(strings.Join(p.renderRaster(63, 34).rows(p.palette), "\n"))
	if grownArt == initialArt {
		t.Fatal("spring growth did not change the artwork")
	}

	beforePaletteView := p.View(100, 30, 0, "working tree clean · main")
	p.Update(keyPress("c"))
	if p.palette != 1 || p.View(100, 30, 0, "working tree clean · main") == beforePaletteView {
		t.Fatal("c did not immediately change the leaf colors")
	}
	p.Update(keyPress("c"))
	p.Update(keyPress("c"))
	if p.palette != 0 {
		t.Fatalf("palette did not wrap to zero: %d", p.palette)
	}

	for range playgroundMaxSprouts + 4 {
		p.Update(keyPress("b"))
	}
	if p.sprouts != playgroundMaxSprouts {
		t.Fatalf("sprouts = %d, want cap %d", p.sprouts, playgroundMaxSprouts)
	}
	if !strings.Contains(p.status, "crown is full") {
		t.Fatalf("bounded growth has no useful status: %q", p.status)
	}
	p.Update(keyPress("z"))
	if p.sprouts != playgroundMaxSprouts-1 || p.growth[p.sprouts] != (playgroundMotion{}) {
		t.Fatal("z did not prune and clear only the last sprout")
	}
	p.Update(keyPress("x"))
	fresh := newPixelPlayground()
	if p.wind != 0 || p.sprouts != 0 || p.palette != 0 || p.tick != 0 || p.growth != fresh.growth {
		t.Fatal("x did not replant the deterministic initial bonsai")
	}
	replantedArt := ansi.Strip(strings.Join(p.renderRaster(63, 34).rows(p.palette), "\n"))
	if replantedArt != initialArt {
		t.Fatal("replanting did not restore the initial pixel raster")
	}
}

func TestPlaygroundKeysAreIsolatedFromRepositoryKeys(t *testing.T) {
	p := newPixelPlayground()
	for _, key := range []string{"h", "l", "b", "c", "z", "x", "m", " "} {
		if !p.Update(keyPress(key)) {
			t.Errorf("%q was not consumed by the bonsai", key)
		}
	}
	for _, key := range []string{"C", "P", "s", "u", "H", "L", "Z", "V", "q", "r", "?", "1", "2"} {
		if p.Update(keyPress(key)) {
			t.Errorf("repository/global key %q was swallowed by the bonsai", key)
		}
	}
}

func TestPlaygroundPixelRaster(t *testing.T) {
	p := newPixelPlayground()
	raster := p.renderRaster(63, 34)
	if raster.width != 63 || raster.height != 34 || len(raster.cells) != 63*34 {
		t.Fatalf("bad raster geometry: %dx%d, %d cells", raster.width, raster.height, len(raster.cells))
	}
	counts := make(map[playgroundPixel]int)
	leafPixels, barkPixels, soilPixels := 0, 0, 0
	for _, pixel := range raster.cells {
		if pixel >= playgroundPixelKinds {
			t.Fatalf("raster contains invalid pixel kind %d", pixel)
		}
		counts[pixel]++
		switch pixel {
		case pixelLeafDark, pixelLeaf, pixelLeafLight:
			leafPixels++
		case pixelBarkShadow, pixelBark, pixelBarkLight:
			barkPixels++
		case pixelSoil, pixelSoilLight:
			soilPixels++
		}
	}
	if leafPixels < 100 || barkPixels < 35 || soilPixels < 35 {
		t.Fatalf("bonsai silhouette is too sparse: leaves=%d bark=%d soil=%d", leafPixels, barkPixels, soilPixels)
	}
	if counts[pixelAccent] == 0 || counts[pixelFirefly] == 0 {
		t.Fatal("scene is missing its restrained accent or firefly pixels")
	}
	if maxPlaygroundRun(raster, pixelBark, pixelBarkLight) < 3 {
		t.Fatal("trunk has no chunky base; it collapsed into a connector line")
	}

	plain := ansi.Strip(p.View(100, 30, 0, "working tree clean · main"))
	if strings.Count(plain, "▀") < 100 {
		t.Fatalf("normal screen does not expose a substantial half-block pixel raster:\n%s", plain)
	}
	for _, want := range []string{"twig · bonsai", "working tree clean", "h/l wind", "b grow", "c color", "z prune", "x replant", "m bloom", "space gust"} {
		if !strings.Contains(plain, want) {
			t.Errorf("pixel screen missing %q:\n%s", want, plain)
		}
	}
	for _, forbidden := range []string{"git log", "HEAD", "feature/", "●", "│", "╭", "╯", "commit subject"} {
		if strings.Contains(plain, forbidden) {
			t.Errorf("pixel screen retained graph artifact %q:\n%s", forbidden, plain)
		}
	}
	if regexp.MustCompile(`\b[0-9a-f]{7}\b`).MatchString(plain) {
		t.Fatalf("pixel screen retained a hash column:\n%s", plain)
	}
}

func maxPlaygroundRun(raster playgroundRaster, kinds ...playgroundPixel) int {
	wanted := func(pixel playgroundPixel) bool {
		for _, kind := range kinds {
			if pixel == kind {
				return true
			}
		}
		return false
	}
	best := 0
	for y := range raster.height {
		run := 0
		for x := range raster.width {
			if wanted(raster.cells[y*raster.width+x]) {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
	}
	return best
}

func TestPlaygroundCanopyDepthAndStructure(t *testing.T) {
	p := newPixelPlayground()
	advancePlayground(&p, 6)
	raster := p.renderRaster(63, 34)
	counts := make(map[playgroundPixel]int)
	left, right, top, bottom := raster.width, -1, raster.height, -1
	sky := 0
	shadowY, lightY := 0, 0
	edgeHighlights := 0
	for y := range raster.height {
		for x := range raster.width {
			pixel := raster.cells[y*raster.width+x]
			counts[pixel]++
			if pixel == pixelSky {
				sky++
			}
			if playgroundIsFoliage(pixel) {
				left, right = min(left, x), max(right, x)
				top, bottom = min(top, y), max(bottom, y)
			}
			if pixel == pixelLeafShadow {
				shadowY += y
			}
			if pixel == pixelLeafLight {
				lightY += y
				for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					nx, ny := x+d[0], y+d[1]
					if nx < 0 || ny < 0 || nx >= raster.width || ny >= raster.height || raster.cells[ny*raster.width+nx] == pixelSky {
						edgeHighlights++
						break
					}
				}
			}
		}
	}
	t.Logf("canopy bounds=(%d,%d)-(%d,%d), sky=%d, tones=%v", left, top, right, bottom, sky, counts)
	if len(playgroundBaseLimbs) != playgroundBaseBoughs || right-left < 42 || top > 6 || bottom < 18 {
		t.Fatalf("canopy is not a broad, layered hero silhouette: bounds=(%d,%d)-(%d,%d)", left, top, right, bottom)
	}
	if counts[pixelLeafShadow] < 20 || counts[pixelLeafDark] < 35 || counts[pixelLeaf] < 80 || counts[pixelLeafLight] < 10 {
		t.Fatalf("canopy lacks substantial shadow/midtone/highlight layers: %v", counts)
	}
	if shadowY*counts[pixelLeafLight] <= lightY*counts[pixelLeafShadow] {
		t.Fatal("deep shadow is not weighted below the lit canopy edges")
	}
	if edgeHighlights < 4 {
		t.Fatalf("only %d lit cells touch the silhouette edge", edgeHighlights)
	}
	if sky*100/len(raster.cells) < 35 {
		t.Fatalf("fuller canopy destroyed the negative space: only %d%% sky", sky*100/len(raster.cells))
	}
	if counts[pixelSoilRock] == 0 || counts[pixelDistant] == 0 || counts[pixelTrail] == 0 {
		t.Fatalf("scene lacks sculpted ground or atmospheric details: %v", counts)
	}
}

func playgroundIsFoliage(pixel playgroundPixel) bool {
	switch pixel {
	case pixelLeafShadow, pixelLeafDark, pixelLeaf, pixelLeafLight:
		return true
	default:
		return false
	}
}

func TestPlaygroundSpriteMatricesHaveDeliberateDepth(t *testing.T) {
	for variant, matrix := range playgroundLeafSprites {
		width := len(matrix[0])
		if width != 17 || len(matrix) != 9 {
			t.Fatalf("sprite %d is %dx%d, want 17x9", variant, width, len(matrix))
		}
		seen := make(map[byte]bool)
		for y, row := range matrix {
			if len(row) != width {
				t.Fatalf("sprite %d row %d has %d pixels, want %d: %q", variant, y, len(row), width, row)
			}
			for i := range len(row) {
				seen[row[i]] = true
			}
		}
		for _, layer := range []byte{'x', 'd', 'm', 'l', 'a', '.'} {
			if !seen[layer] {
				t.Fatalf("sprite %d lacks %q layer/detail", variant, layer)
			}
		}
	}
}

func TestPlaygroundPaletteCoverageAndContrast(t *testing.T) {
	rendered := make(map[int]string, playgroundPalettes)
	for palette := range playgroundPalettes {
		p := newPixelPlayground()
		p.palette = palette
		advancePlayground(&p, 6)
		raster := p.renderRaster(63, 34)
		counts := make(map[playgroundPixel]int)
		for _, pixel := range raster.cells {
			counts[pixel]++
		}
		for _, layer := range []playgroundPixel{pixelLeafShadow, pixelLeafDark, pixelLeaf, pixelLeafLight, pixelAccent} {
			if counts[layer] == 0 {
				t.Fatalf("palette %d does not expose pixel layer %d", palette, layer)
			}
			if playgroundPaletteColors[palette][layer] == nil {
				t.Fatalf("palette %d has no color for layer %d", palette, layer)
			}
		}
		colors := playgroundPaletteColors[palette]
		shadow := playgroundLuminance(colors[pixelLeafShadow])
		dark := playgroundLuminance(colors[pixelLeafDark])
		mid := playgroundLuminance(colors[pixelLeaf])
		light := playgroundLuminance(colors[pixelLeafLight])
		if !(shadow < dark && dark < mid && mid < light) {
			t.Fatalf("palette %d has unreadable foliage contrast: %.3f %.3f %.3f %.3f", palette, shadow, dark, mid, light)
		}
		rendered[palette] = strings.Join(raster.rows(palette), "\n")
	}
	for palette := range playgroundPalettes {
		for other := palette + 1; other < playgroundPalettes; other++ {
			if rendered[palette] == rendered[other] {
				t.Fatalf("palettes %d and %d render identical ANSI", palette, other)
			}
		}
	}
}

func playgroundLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535
}

func TestPlaygroundBloomPetalsHaveBoundedLifecycle(t *testing.T) {
	p := newPixelPlayground()
	lenBefore := len(p.petals)
	if active := activePlaygroundPetals(p); active != 0 {
		t.Fatalf("new tree starts with %d active petals", active)
	}
	p.Update(keyPress("m"))
	if got := activePlaygroundPetals(p); got != playgroundBloomBurstSize {
		t.Fatalf("bloom activated %d petals, want burst size %d", got, playgroundBloomBurstSize)
	}
	before := p.petals
	maxBloom := p.bloom.position
	for range 8 {
		p.Tick()
		maxBloom = max(maxBloom, p.bloom.position)
	}
	if before == p.petals {
		t.Fatal("petal springs did not move the burst")
	}
	if maxBloom <= 1.02 {
		t.Fatalf("bloom lacks spring overshoot: peak %.4f", maxBloom)
	}

	if len(p.petals) != lenBefore {
		t.Fatal("bloom exceeded the fixed petal slots")
	}
	advancePlayground(&p, 57)
	if got := activePlaygroundPetals(p); got != 3 {
		t.Fatalf("staggered petal fade left %d active at age 65, want 3", got)
	}
	advancePlayground(&p, 20)
	if got := activePlaygroundPetals(p); got != 0 {
		t.Fatalf("%d petals outlived their bounded lifecycle", got)
	}
	if p.petals != ([playgroundMaxPetals]playgroundPetal{}) {
		t.Fatal("expired petals left stale position or target state")
	}
}

func activePlaygroundPetals(p pixelPlayground) int {
	active := 0
	for _, petal := range p.petals {
		if petal.active {
			active++
		}
	}
	return active
}

func TestPlaygroundRepeatBloomPreservesLivePetals(t *testing.T) {
	p := newPixelPlayground()
	p.Update(keyPress("m"))
	advancePlayground(&p, 12)
	p.Update(keyPress(" "))
	p.Tick()

	// A second press immediately launches another burst into the large pool.
	airborne := p.petals
	p.Update(keyPress("m"))
	assertPlaygroundPetalsPreserved(t, airborne, p.petals)
	if got := activePlaygroundPetals(p); got != 2*playgroundBloomBurstSize {
		t.Fatalf("second bloom produced %d active petals, want %d", got, 2*playgroundBloomBurstSize)
	}

	advancePlayground(&p, 52)
	if got := activePlaygroundPetals(p); got != 11 {
		t.Fatalf("independent expiry left %d active at age 65, want 11", got)
	}
	// Grounded/fading remains owned by the old petal until its normal TTL.
	const grounded = 4
	p.petals[grounded].x.position = p.petals[grounded].target.x
	p.petals[grounded].y.position = p.petals[grounded].target.y
	p.petals[grounded].x.velocity = 0
	p.petals[grounded].y.velocity = 0
	before := p.petals
	if !before[grounded].active || before[grounded].age*3 <= before[grounded].ttl*2 {
		t.Fatal("test setup did not produce a live, fading grounded petal")
	}
	previousModel := p

	p.Update(keyPress("m"))
	assertPlaygroundPetalsPreserved(t, before, p.petals)
	if got := activePlaygroundPetals(p); got != 19 {
		t.Fatalf("free-capacity bloom produced %d active petals, want 19", got)
	}
	fresh := 0
	for i, old := range before {
		if !old.active && p.petals[i].active {
			fresh++
			if p.petals[i].age != 0 {
				t.Fatalf("slot %d was re-aged instead of newly launched", i)
			}
		}
	}
	if fresh != playgroundBloomBurstSize {
		t.Fatalf("bloom launched %d petals into free slots, want %d", fresh, playgroundBloomBurstSize)
	}
	if previousModel.petals != before || activePlaygroundPetals(previousModel) != 11 {
		t.Fatal("repeat bloom mutated the pre-update model's petals or queue")
	}

	// Keep pressing until the 64-slot pool is full and one burst must wait.
	for presses := 0; p.queuedBursts == 0 && presses < 16; presses++ {
		old := p.petals
		p.Update(keyPress("m"))
		assertPlaygroundPetalsPreserved(t, old, p.petals)
	}
	if p.queuedBursts == 0 || activePlaygroundPetals(p) != playgroundMaxPetals {
		t.Fatal("rapid refill did not transition from a full pool to queued launches")
	}
	full := p.petals
	p.Update(keyPress("m"))
	if p.petals != full {
		t.Fatal("queued repeat bloom reset an active petal")
	}

	advancePlayground(&p, 512)
	assertPlaygroundBloomQuiescent(t, p)
	if previousModel.petals != before {
		t.Fatal("ticking the updated copy changed the earlier model's petals")
	}
}

func assertPlaygroundPetalsPreserved(t *testing.T, before, after [playgroundMaxPetals]playgroundPetal) {
	t.Helper()
	for i, old := range before {
		if old.active && after[i] != old {
			t.Fatalf("bloom changed pre-existing petal %d:\nbefore: %+v\nafter:  %+v", i, old, after[i])
		}
	}
}

func TestPlaygroundRapidBloomFountainIsDenseBoundedAndFinite(t *testing.T) {
	p := newPixelPlayground()
	for press := range 40 {
		before := p.petals
		p.Update(keyPress("m"))
		assertPlaygroundPetalsPreserved(t, before, p.petals)
		if p.queuedBursts < 0 || p.queuedBursts > playgroundMaxPendingBursts {
			t.Fatalf("press %d produced invalid queue depth %d", press, p.queuedBursts)
		}
		if activePlaygroundPetals(p) > playgroundMaxPetals || p.pendingPetalCount() > playgroundMaxPendingBursts*playgroundBloomBurstSize {
			t.Fatalf("press %d exceeded bounded fountain state", press)
		}
	}
	if got := activePlaygroundPetals(p); got != playgroundMaxPetals {
		t.Fatalf("rapid bloom has %d live petals, want a dense pool of %d", got, playgroundMaxPetals)
	}
	if p.queuedBursts != playgroundMaxPendingBursts || p.pendingPetalCount() != playgroundMaxPendingBursts*playgroundBloomBurstSize {
		t.Fatalf("spam did not fill the bounded FIFO: bursts=%d petals=%d", p.queuedBursts, p.pendingPetalCount())
	}
	if p.nextBloom != 16 {
		t.Fatalf("accepted generation count = %d, want 16 bounded bursts", p.nextBloom)
	}
	if !strings.Contains(p.status, "fountain full") {
		t.Fatalf("saturated spam is silent instead of explicit: %q", p.status)
	}
	identities := make(map[uint64]int, playgroundMaxPetals)
	for _, petal := range p.petals {
		if petal.active {
			identities[petal.id]++
		}
	}
	if len(identities) != playgroundMaxPetals {
		t.Fatalf("rapid bloom produced %d distinct live identities, want far more than six (%d)", len(identities), playgroundMaxPetals)
	}
	for id, count := range identities {
		if count != 1 {
			t.Fatalf("petal identity %d occurs %d times", id, count)
		}
	}

	quiesced := 0
	for tick := 1; tick <= 512; tick++ {
		p.Tick()
		if activePlaygroundPetals(p) > playgroundMaxPetals || p.queuedBursts > playgroundMaxPendingBursts {
			t.Fatalf("tick %d exceeded pool or queue bounds", tick)
		}
		if activePlaygroundPetals(p) == 0 && p.queuedBursts == 0 {
			quiesced = tick
			break
		}
	}
	if quiesced == 0 {
		t.Fatal("fountain did not reach quiescence in 512 ticks without more input")
	}
	assertPlaygroundBloomQuiescent(t, p)
}

func assertPlaygroundBloomQuiescent(t *testing.T, p pixelPlayground) {
	t.Helper()
	if p.petals != ([playgroundMaxPetals]playgroundPetal{}) {
		t.Fatal("live petals remained after their bounded lifetime")
	}
	if p.queuedBursts != 0 || p.pendingPetalCount() != 0 || p.pendingHead != 0 {
		t.Fatalf("fountain retained pending launches: bursts=%d petals=%d head=%d", p.queuedBursts, p.pendingPetalCount(), p.pendingHead)
	}
	if p.petalQueue != ([playgroundMaxPendingBursts]playgroundPendingBurst{}) {
		t.Fatal("drained fountain left stale FIFO entries")
	}
}

func TestPlaygroundQueuedBloomDrainsIntoNaturalExpiries(t *testing.T) {
	p := newPixelPlayground()
	for range 9 {
		p.Update(keyPress("m"))
	}
	if activePlaygroundPetals(p) != playgroundMaxPetals || p.queuedBursts != 1 {
		t.Fatalf("test setup did not fill the pool and queue one burst")
	}
	queuedIDs := make(map[uint64]bool, playgroundBloomBurstSize)
	for tick := 1; tick <= 73; tick++ {
		before := p.petals
		var expected [playgroundMaxPetals]playgroundPetal
		var survives [playgroundMaxPetals]bool
		for i, petal := range before {
			if !petal.active || petal.age+1 >= petal.ttl {
				continue
			}
			petal.age++
			petal.x.step(p.springs.petal, petal.target.x)
			petal.y.step(p.springs.petal, petal.target.y)
			expected[i], survives[i] = petal, true
		}
		p.Tick()
		for i, want := range expected {
			if survives[i] && p.petals[i] != want {
				t.Fatalf("tick %d changed an older survivor beyond its ordinary spring integration at slot %d", tick, i)
			}
		}
		for _, petal := range p.petals {
			if petal.active && petal.id >= playgroundMaxPetals {
				queuedIDs[petal.id] = true
			}
		}
	}
	if len(queuedIDs) != playgroundBloomBurstSize {
		t.Fatalf("FIFO launched %d distinct queued petals, want %d", len(queuedIDs), playgroundBloomBurstSize)
	}
	for id := playgroundMaxPetals; id < playgroundMaxPetals+playgroundBloomBurstSize; id++ {
		if !queuedIDs[uint64(id)] {
			t.Fatalf("queued launch id %d never appeared", id)
		}
	}
	if p.queuedBursts != 0 || p.pendingPetalCount() != 0 || activePlaygroundPetals(p) != playgroundBloomBurstSize {
		t.Fatalf("queued burst did not drain by natural expiry: queue=%d pending=%d live=%d", p.queuedBursts, p.pendingPetalCount(), activePlaygroundPetals(p))
	}
	advancePlayground(&p, 80)
	assertPlaygroundBloomQuiescent(t, p)
}

func TestPlaygroundDenseBloomSnapshotReadability(t *testing.T) {
	p := newPixelPlayground()
	for range 32 {
		p.Update(keyPress("m"))
	}
	advancePlayground(&p, 16)
	p.Update(keyPress(" "))
	advancePlayground(&p, 4)
	if activePlaygroundPetals(p) != playgroundMaxPetals {
		t.Fatal("dense snapshot did not retain the full live fountain during gust")
	}
	raster := p.renderRaster(63, 34)
	visiblePetals := 0
	for _, pixel := range raster.cells {
		if pixel == pixelPetal || pixel == pixelPetalDim {
			visiblePetals++
		}
	}
	if visiblePetals < 12 {
		t.Fatalf("dense bloom left only %d readable petal pixels", visiblePetals)
	}
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "80x24", width: 80, height: 24},
		{name: "100x30", width: 100, height: 30},
	} {
		t.Run(size.name, func(t *testing.T) {
			plain := ansi.Strip(p.View(size.width, size.height, p.tick, "working tree clean · main"))
			rows := strings.Split(plain, "\n")
			if len(rows) != size.height {
				t.Fatalf("dense bloom rendered %d rows, want %d", len(rows), size.height)
			}
			for i, row := range rows {
				if lipgloss.Width(row) != size.width {
					t.Fatalf("dense bloom row %d has width %d", i, lipgloss.Width(row))
				}
			}
			for _, want := range []string{"twig · bonsai", "working tree clean", "b grow", "space gust"} {
				if !strings.Contains(plain, want) {
					t.Errorf("dense snapshot lacks readable text %q", want)
				}
			}
			for i := range rows {
				rows[i] = strings.TrimRight(rows[i], " ")
			}
			t.Logf("%s dense bloom fountain with gust (ANSI stripped, %d live petals):\n%s", size.name, activePlaygroundPetals(p), strings.Join(rows, "\n"))
		})
	}
}

func TestPlaygroundSpaceGustFollowThrough(t *testing.T) {
	p := newPixelPlayground()
	advancePlayground(&p, 60)
	if !p.Update(keyPress(" ")) {
		t.Fatal("space did not trigger a gust")
	}
	if p.gust.position <= 0 || !strings.Contains(p.status, "quick gust") {
		t.Fatalf("space made no visible gust impulse: %+v", p.gust)
	}
	p.Tick()
	if !(p.canopies[0].position < p.boughs[0].position && p.boughs[0].position < p.trunk.position) {
		t.Fatalf("gust does not proceed trunk -> bough -> canopy: trunk=%f bough=%f canopy=%f",
			p.trunk.position, p.boughs[0].position, p.canopies[0].position)
	}
	if p.boughs[0].position == p.boughs[1].position || p.canopies[0].position == p.canopies[1].position {
		t.Fatal("gust moved every canopy identically")
	}
	advancePlayground(&p, 240)
	if p.gust.position > 0.01 || p.trunk.position > 0.05 {
		t.Fatalf("gust never settled: gust=%f trunk=%f", p.gust.position, p.trunk.position)
	}

	left := newPixelPlayground()
	left.Update(keyPress("h"))
	left.Update(keyPress(" "))
	if left.gust.position >= 0 {
		t.Fatal("space did not follow the current leftward wind")
	}
}

func TestPlaygroundSproutOvershootsAndSettles(t *testing.T) {
	p := newPixelPlayground()
	p.Update(keyPress("b"))
	peak := 0.0
	for range 80 {
		p.Tick()
		peak = max(peak, p.growth[0].position)
	}
	if peak <= 1.03 {
		t.Fatalf("sprout growth never overshoots: peak %.4f", peak)
	}
	advancePlayground(&p, 180)
	if p.growth[0].position < 0.98 || p.growth[0].position > 1.02 {
		t.Fatalf("sprout did not settle near full size: %.4f", p.growth[0].position)
	}
}

func TestPlaygroundRasterRejectsOutOfBoundsPixels(t *testing.T) {
	raster := newPlaygroundRaster(4, 3)
	for _, point := range [][2]int{{-1, 0}, {0, -1}, {4, 0}, {0, 3}, {-100, 100}} {
		if raster.set(point[0], point[1], pixelLeaf) {
			t.Fatalf("set(%d, %d) unexpectedly succeeded", point[0], point[1])
		}
	}
	if !reflect.DeepEqual(raster.cells, make([]playgroundPixel, 12)) {
		t.Fatal("out-of-bounds drawing changed the raster")
	}
	if !raster.set(3, 2, pixelLeafLight) || raster.cells[len(raster.cells)-1] != pixelLeafLight {
		t.Fatal("last valid pixel could not be drawn")
	}
}

func TestPlaygroundViewGeometryAndPurity(t *testing.T) {
	tests := []struct {
		width, height int
		edited        bool
	}{
		{width: 0, height: 0},
		{width: 0, height: 4},
		{width: 10, height: 0},
		{width: 1, height: 1},
		{width: 1, height: 9},
		{width: 30, height: 4},
		{width: 40, height: 8},
		{width: 80, height: 24},
		{width: 100, height: 30},
		{width: 30, height: 8, edited: true},
		{width: 50, height: 10, edited: true},
	}
	for _, size := range tests {
		p := newPixelPlayground()
		before := p
		if size.edited {
			for range playgroundMaxSprouts {
				p.Update(keyPress("b"))
			}
			p.Update(keyPress("l"))
			p.Update(keyPress("c"))
			advancePlayground(&p, 20)
			before = p
		}
		output := p.View(size.width, size.height, 0, "no commits yet · unborn\nignored")
		if !reflect.DeepEqual(p, before) {
			t.Fatalf("View(%d, %d) mutated the playground", size.width, size.height)
		}
		if size.height == 0 {
			if output != "" {
				t.Fatalf("View(%d, 0) rendered %q", size.width, output)
			}
			continue
		}
		rows := strings.Split(output, "\n")
		if len(rows) != size.height {
			t.Fatalf("View(%d, %d) rendered %d rows", size.width, size.height, len(rows))
		}
		for i, row := range rows {
			if width := lipgloss.Width(row); width != size.width {
				t.Fatalf("View(%d, %d) row %d has width %d: %q", size.width, size.height, i, width, row)
			}
		}
	}

	p := newPixelPlayground()
	if p.View(80, 24, 0, "clean") != p.View(80, 24, 999999, "clean") {
		t.Fatal("View is deriving motion from a frame number instead of spring state")
	}
	m := testModel(100, 30)
	m.width, m.height = 0, 0
	m.setSize()
	if got := m.render(); got != "" {
		t.Fatalf("zero-sized model rendered %q", got)
	}
}

func TestPlaygroundResizeTransitions(t *testing.T) {
	m := testModel(100, 30)
	for _, size := range [][2]int{{100, 30}, {40, 8}, {80, 24}, {30, 4}, {1, 1}, {100, 30}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(model)
		output := m.welcomeView()
		height := max(1, m.height-2)
		rows := strings.Split(output, "\n")
		if len(rows) != height {
			t.Fatalf("resize to %dx%d produced %d body rows, want %d", size[0], size[1], len(rows), height)
		}
		for i, row := range rows {
			if width := lipgloss.Width(row); width != max(1, size[0]) {
				t.Fatalf("resize to %dx%d row %d has width %d", size[0], size[1], i, width)
			}
		}
	}
}

func TestPlaygroundVisualSnapshot(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "80x24", width: 80, height: 24},
		{name: "100x30", width: 100, height: 30},
	} {
		t.Run(size.name, func(t *testing.T) {
			p := newPixelPlayground()
			advancePlayground(&p, 6)
			plain := ansi.Strip(p.View(size.width, size.height, p.tick, "working tree clean · main"))
			if strings.Count(plain, "▀") < 180 {
				t.Fatalf("%s canopy is too sparse to read as a rich bonsai:\n%s", size.name, plain)
			}
			for _, want := range []string{"twig · bonsai", "working tree clean", "b grow", "space gust"} {
				if !strings.Contains(plain, want) {
					t.Errorf("%s snapshot lacks readable text %q", size.name, want)
				}
			}
			rows := strings.Split(plain, "\n")
			for i := range rows {
				rows[i] = strings.TrimRight(rows[i], " ")
			}
			t.Logf("%s layered spring bonsai (ANSI stripped):\n%s", size.name, strings.Join(rows, "\n"))
		})
	}
}
