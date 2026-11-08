package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/tone"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/x/ansi"
)

const (
	playgroundMaxSprouts       = 5
	playgroundBaseBoughs       = 6
	playgroundBoughCount       = playgroundBaseBoughs + playgroundMaxSprouts
	playgroundMaxWind          = 3
	playgroundPalettes         = 3
	playgroundMaxPetals        = 64
	playgroundBloomBurstSize   = 8
	playgroundMaxPendingBursts = 8
	playgroundPetalLifetime    = 58

	playgroundContentWidth = 72
	playgroundRasterWidth  = 63
	playgroundRasterRows   = 17
	playgroundCanvasWidth  = 64
	playgroundCanvasHeight = 38
	playgroundGroundY      = 33

	playgroundWindFrequency   = 6.2
	playgroundWindDamping     = 0.62
	playgroundGustFrequency   = 3.2
	playgroundGustDamping     = 0.74
	playgroundGrowthFrequency = 5.0
	playgroundGrowthDamping   = 0.46
	playgroundBloomFrequency  = 6.4
	playgroundBloomDamping    = 0.42
	playgroundSeedFrequency   = 2.4
	playgroundSeedDamping     = 0.82
	playgroundPetalFrequency  = 1.65
	playgroundPetalDamping    = 0.78
)

var pixelPlaygroundKeys = struct {
	left, right    key.Binding
	grow, color    key.Binding
	prune, replant key.Binding
	bloom, gust    key.Binding
}{
	left:    key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "wind left")),
	right:   key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "wind right")),
	grow:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "sprout")),
	color:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "leaf palette")),
	prune:   key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "prune")),
	replant: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "replant")),
	bloom:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "bloom spring")),
	gust:    key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "gust")),
}

// playgroundMotion is the mutable part of one scalar spring. Harmonica's
// Spring is a bundle of cached, read-only coefficients; these positions and
// velocities are deliberately values so Tea model copies stay independent.
type playgroundMotion struct {
	position float64
	velocity float64
}

func (m *playgroundMotion) step(spring harmonica.Spring, target float64) {
	m.position, m.velocity = spring.Update(m.position, m.velocity, target)
}

type playgroundSprings struct {
	trunk, gust   harmonica.Spring
	growth, bloom harmonica.Spring
	seed, petal   harmonica.Spring
	bough, canopy [playgroundBoughCount]harmonica.Spring
}

type playgroundSeed struct {
	x, y playgroundMotion
}

type playgroundPetal struct {
	x, y      playgroundMotion
	target    playgroundPoint
	age, ttl  int
	id, burst uint64
	active    bool
}

type playgroundPendingBurst struct {
	remaining uint8
	burst     uint64
}

// pixelPlayground is a deterministic bonsai, not a repository model. Every bit
// of mutable state is a scalar or a fixed array: there are no shared slices,
// pointers, clocks, or random sources to leak across value-copied Tea models.
type pixelPlayground struct {
	tick         int
	wind         int
	sprouts      int
	palette      int
	status       string
	trunk        playgroundMotion
	gust         playgroundMotion
	bloom        playgroundMotion
	boughs       [playgroundBoughCount]playgroundMotion
	canopies     [playgroundBoughCount]playgroundMotion
	growth       [playgroundMaxSprouts]playgroundMotion
	petals       [playgroundMaxPetals]playgroundPetal
	petalQueue   [playgroundMaxPendingBursts]playgroundPendingBurst
	pendingHead  int
	queuedBursts int
	nextBloom    uint64
	seeds        [3]playgroundSeed
	springs      playgroundSprings
	styles       playgroundStyles
}

type playgroundStyles struct {
	title, repository, action lipgloss.Style
	hintKey, hintText         lipgloss.Style
}

var defaultPlaygroundStyles = playgroundStyles{
	title:      lipgloss.NewStyle().Foreground(tone.Dolly).Bold(true),
	repository: lipgloss.NewStyle().Foreground(tone.Steep),
	action:     lipgloss.NewStyle().Foreground(tone.Guac),
	hintKey:    lipgloss.NewStyle().Foreground(tone.Butter).Bold(true),
	hintText:   lipgloss.NewStyle().Foreground(tone.Steep),
}

func newPixelPlayground() pixelPlayground {
	dt := welcomeFrameInterval.Seconds()
	p := pixelPlayground{
		trunk: playgroundMotion{position: 0.25},
		bloom: playgroundMotion{position: 0.72, velocity: 0.6},
		springs: playgroundSprings{
			trunk:  harmonica.NewSpring(dt, playgroundWindFrequency, playgroundWindDamping),
			gust:   harmonica.NewSpring(dt, playgroundGustFrequency, playgroundGustDamping),
			growth: harmonica.NewSpring(dt, playgroundGrowthFrequency, playgroundGrowthDamping),
			bloom:  harmonica.NewSpring(dt, playgroundBloomFrequency, playgroundBloomDamping),
			seed:   harmonica.NewSpring(dt, playgroundSeedFrequency, playgroundSeedDamping),
			petal:  harmonica.NewSpring(dt, playgroundPetalFrequency, playgroundPetalDamping),
		},
		styles: defaultPlaygroundStyles,
		status: "a small tree, patiently grown · repository untouched",
	}
	for i := range playgroundBoughCount {
		// Slightly different coefficients keep each bough from moving as a rigid
		// cutout. Both stages still use Harmonica's cached value-type springs.
		variation := float64(i%4) * 0.24
		p.springs.bough[i] = harmonica.NewSpring(dt, 4.15+variation, 0.66+float64(i%3)*0.04)
		p.springs.canopy[i] = harmonica.NewSpring(dt, 2.85+variation, 0.54+float64(i%3)*0.035)
	}
	for i := range p.seeds {
		target := playgroundSeedTracks[i][0]
		p.seeds[i].x = playgroundMotion{
			position: target.x + playgroundSeedOffsets[i].x,
			velocity: playgroundSeedOffsets[i].velocity,
		}
		p.seeds[i].y = playgroundMotion{position: target.y + playgroundSeedOffsets[i].y}
	}
	return p
}

// Update applies one bonsai control and reports whether it was consumed. None
// of these operations knows about a git repository.
func (p *pixelPlayground) Update(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, pixelPlaygroundKeys.left):
		p.changeWind(-1)
	case key.Matches(msg, pixelPlaygroundKeys.right):
		p.changeWind(1)
	case key.Matches(msg, pixelPlaygroundKeys.grow):
		p.grow()
	case key.Matches(msg, pixelPlaygroundKeys.color):
		p.palette = (p.palette + 1) % playgroundPalettes
		p.status = playgroundPaletteNames[p.palette] + " leaves · color changed"
	case key.Matches(msg, pixelPlaygroundKeys.prune):
		p.prune()
	case key.Matches(msg, pixelPlaygroundKeys.replant):
		*p = newPixelPlayground()
		p.status = "replanted · the same roots, a fresh spring"
	case key.Matches(msg, pixelPlaygroundKeys.bloom):
		p.bloomBurst()
	case key.Matches(msg, pixelPlaygroundKeys.gust):
		p.kickGust()
	default:
		return false
	}
	return true
}

func (p *pixelPlayground) changeWind(delta int) {
	next := min(playgroundMaxWind, max(-playgroundMaxWind, p.wind+delta))
	if next == p.wind {
		p.status = "the wind is already at its gentle limit"
		return
	}
	p.wind = next
	direction := "calm"
	if next < 0 {
		direction = "left"
	} else if next > 0 {
		direction = "right"
	}
	strength := next
	if strength < 0 {
		strength = -strength
	}
	p.status = fmt.Sprintf("wind %s · strength %d/%d · limbs follow", direction, strength, playgroundMaxWind)
}

func (p *pixelPlayground) grow() {
	if p.sprouts >= playgroundMaxSprouts {
		p.status = "the crown is full · z prunes the last sprout"
		return
	}
	// A new limb has no size on the keypress itself. Successive welcome ticks
	// integrate this state toward one, unfurling both its wood and leaf pad.
	p.growth[p.sprouts] = playgroundMotion{velocity: 2.5}
	p.sprouts++
	p.status = fmt.Sprintf("sprout %d/%d · a new limb unfurls", p.sprouts, playgroundMaxSprouts)
}

func (p *pixelPlayground) prune() {
	if p.sprouts == 0 {
		p.status = "nothing to prune · the original bonsai remains"
		return
	}
	p.sprouts--
	p.growth[p.sprouts] = playgroundMotion{}
	p.status = fmt.Sprintf("pruned the last sprout · %d additions remain", p.sprouts)
}

func (p *pixelPlayground) bloomBurst() {
	p.bloom.position = min(p.bloom.position, 0.62)
	p.bloom.velocity = 6.5
	if p.queuedBursts == playgroundMaxPendingBursts {
		// Saturation is explicit rather than silently discarding rapid presses.
		p.status = fmt.Sprintf("fountain full · %d petals aloft · %d bursts waiting", p.livePetalCount(), p.queuedBursts)
		return
	}
	// Every accepted press gets its own generation. Appending before draining
	// gives live slots to this burst immediately, while FIFO order is preserved
	// once older presses are already waiting.
	tail := (p.pendingHead + p.queuedBursts) % len(p.petalQueue)
	p.petalQueue[tail] = playgroundPendingBurst{remaining: playgroundBloomBurstSize, burst: p.nextBloom}
	p.nextBloom++
	p.queuedBursts++
	launched := p.drainBloom()
	switch {
	case launched == 1 && p.queuedBursts == 0:
		p.status = "bloom fountain · one new petal takes flight"
	case launched > 0 && p.queuedBursts == 0:
		p.status = fmt.Sprintf("bloom fountain · %d new petals take flight", launched)
	case launched > 0:
		p.status = fmt.Sprintf("bloom fountain · %d launched · %d bursts waiting", launched, p.queuedBursts)
	default:
		p.status = fmt.Sprintf("bloom queued · %d/%d bursts waiting", p.queuedBursts, playgroundMaxPendingBursts)
	}
}

// drainBloom launches from the oldest pending generation into free slots only.
// The fixed ring and fixed petal pool keep both memory and spam bounded.
func (p *pixelPlayground) drainBloom() int {
	launched := 0
	for p.queuedBursts > 0 {
		slot := -1
		for i := range p.petals {
			if !p.petals[i].active {
				slot = i
				break
			}
		}
		if slot < 0 {
			break
		}
		pending := &p.petalQueue[p.pendingHead]
		withinBurst := playgroundBloomBurstSize - int(pending.remaining)
		id := pending.burst*playgroundBloomBurstSize + uint64(withinBurst)
		p.launchPetal(slot, pending.burst, id)
		pending.remaining--
		launched++
		if pending.remaining == 0 {
			p.petalQueue[p.pendingHead] = playgroundPendingBurst{}
			p.pendingHead = (p.pendingHead + 1) % len(p.petalQueue)
			p.queuedBursts--
		}
	}
	if p.queuedBursts == 0 {
		p.pendingHead = 0
	}
	return launched
}

func (p *pixelPlayground) launchPetal(slot int, burst, id uint64) {
	// Callers reserve only inactive slots. A live slot always retains its state,
	// including identity, target and remaining life, regardless of bloom spam.
	spec := playgroundPetalSpecs[id%uint64(len(playgroundPetalSpecs))]
	limbIndex := int((id + id/uint64(len(playgroundPetalSpecs))) % playgroundBaseBoughs)
	limb := playgroundBaseLimbs[limbIndex]
	wave := float64(int((id/uint64(len(playgroundPetalSpecs)))%5) - 2)
	p.petals[slot] = playgroundPetal{
		x: playgroundMotion{
			position: limb.cluster.x + spec.startX + 0.25*wave,
			velocity: spec.vx + 0.18*p.canopies[limbIndex].position + 0.2*wave,
		},
		y: playgroundMotion{
			position: limb.cluster.y + spec.startY,
			velocity: spec.vy + 0.12*float64(id%3),
		},
		target: playgroundPoint{
			x: min(58, max(6, spec.targetX+1.4*wave)),
			y: 32 + float64(id%3),
		},
		ttl:    playgroundPetalLifetime + int(id%6)*3,
		id:     id,
		burst:  burst,
		active: true,
	}
}

func (p pixelPlayground) livePetalCount() int {
	active := 0
	for _, petal := range p.petals {
		if petal.active {
			active++
		}
	}
	return active
}

func (p pixelPlayground) pendingPetalCount() int {
	pending := 0
	for _, burst := range p.petalQueue {
		pending += int(burst.remaining)
	}
	return pending
}

func (p *pixelPlayground) kickGust() {
	direction := 1.0
	if p.wind < 0 {
		direction = -1
	}
	p.gust.position = 2.2 * direction
	p.gust.velocity = 5.5 * direction
	for i := range p.petals {
		if p.petals[i].active {
			p.petals[i].x.velocity += 1.8 * direction
			p.petals[i].target.x = min(58, max(6, p.petals[i].target.x+3*direction))
		}
	}
	p.status = "quick gust · boughs bow, then leaf tips catch up"
}

var playgroundPetalSpecs = [...]struct {
	startX, startY, vx, vy, targetX float64
}{
	{startX: -3, startY: -1, vx: -2.8, vy: -1.8, targetX: 15},
	{startX: 3, startY: -1, vx: 3.2, vy: -1.6, targetX: 51},
	{startX: -2, startY: -2, vx: -1.5, vy: -2.2, targetX: 10},
	{startX: 2, startY: -2, vx: 2.2, vy: -1.4, targetX: 45},
	{startX: -1, startY: -3, vx: -0.8, vy: -2.4, targetX: 25},
	{startX: 1, startY: -2, vx: 1.4, vy: -1.7, targetX: 34},
}

// Tick is the only place animation advances. View never integrates a spring.
func (p *pixelPlayground) Tick() {
	p.tick++
	p.gust.step(p.springs.gust, 0)
	p.trunk.step(p.springs.trunk, float64(p.wind)*1.15+p.gust.position)

	// Every bough consumes the trunk's actual position; every leaf pad then
	// consumes its own bough. Distinct coefficients give each canopy a slightly
	// different follow-through instead of translating the whole tree rigidly.
	for i := range playgroundBoughCount {
		scale := 0.68 + float64(i%3)*0.045
		p.boughs[i].step(p.springs.bough[i], p.trunk.position*scale)
		p.canopies[i].step(p.springs.canopy[i], p.boughs[i].position*(0.8+float64(i%2)*0.035))
	}
	p.bloom.step(p.springs.bloom, 1)
	for i := range p.sprouts {
		p.growth[i].step(p.springs.growth, 1)
	}
	for i := range p.petals {
		petal := &p.petals[i]
		if !petal.active {
			continue
		}
		petal.age++
		petal.x.step(p.springs.petal, petal.target.x)
		petal.y.step(p.springs.petal, petal.target.y)
		if petal.age >= petal.ttl {
			p.petals[i] = playgroundPetal{}
		}
	}
	// Launches refill only slots made free by natural expiry. Queued bursts
	// retain their generation while every older petal continues independently.
	p.drainBloom()
	for i := range p.seeds {
		// Waypoints, not sine waves or a clock: each firefly coasts toward a new
		// deterministic resting place under the same Harmonica integrator.
		target := playgroundSeedTracks[i][(p.tick/32+i)%len(playgroundSeedTracks[i])]
		p.seeds[i].x.step(p.springs.seed, target.x)
		p.seeds[i].y.step(p.springs.seed, target.y)
	}
}

type playgroundPoint struct {
	x, y float64
}

type playgroundLimb struct {
	points  [4]playgroundPoint
	count   int
	cluster playgroundPoint
	sprite  int
}

var playgroundTrunk = [...]playgroundPoint{
	{x: 32, y: 33},
	{x: 30, y: 30},
	{x: 31, y: 27},
	{x: 29, y: 24},
	{x: 30, y: 21},
	{x: 27, y: 17},
	{x: 29, y: 12},
	{x: 27, y: 7},
}

var playgroundRoots = [4][5]playgroundPoint{
	{{x: 32, y: 33}, {x: 29, y: 32}, {x: 26, y: 33}, {x: 22, y: 34}, {x: 18, y: 34}},
	{{x: 31, y: 33}, {x: 35, y: 32}, {x: 38, y: 33}, {x: 42, y: 33.5}, {x: 46, y: 34}},
	{{x: 31, y: 33}, {x: 30, y: 34}, {x: 27, y: 35}, {x: 23, y: 35}},
	{{x: 31, y: 32}, {x: 34, y: 31}, {x: 37, y: 31.5}, {x: 40, y: 32}},
}

var playgroundRootCounts = [4]int{5, 5, 4, 4}

var playgroundBaseLimbs = [playgroundBaseBoughs]playgroundLimb{
	{
		points: [4]playgroundPoint{{x: 30, y: 27}, {x: 25, y: 27}, {x: 19, y: 24}, {x: 14, y: 23}},
		count:  4, cluster: playgroundPoint{x: 13.5, y: 20.8}, sprite: 0,
	},
	{
		points: [4]playgroundPoint{{x: 30, y: 24}, {x: 36, y: 25}, {x: 42, y: 21}, {x: 47, y: 21}},
		count:  4, cluster: playgroundPoint{x: 48, y: 19.2}, sprite: 1,
	},
	{
		points: [4]playgroundPoint{{x: 29, y: 21}, {x: 23, y: 18}, {x: 21, y: 15}, {x: 16, y: 14}},
		count:  4, cluster: playgroundPoint{x: 15.5, y: 11.9}, sprite: 0,
	},
	{
		points: [4]playgroundPoint{{x: 29, y: 17}, {x: 35, y: 16}, {x: 38, y: 12}, {x: 43, y: 11}},
		count:  4, cluster: playgroundPoint{x: 43, y: 8.4}, sprite: 1,
	},
	{
		points: [4]playgroundPoint{{x: 28, y: 12}, {x: 27, y: 9}, {x: 29, y: 5}},
		count:  3, cluster: playgroundPoint{x: 28.5, y: 4.3}, sprite: 2,
	},
	{
		points: [4]playgroundPoint{{x: 29, y: 18}, {x: 31, y: 15}, {x: 30, y: 12}},
		count:  3, cluster: playgroundPoint{x: 30.5, y: 11.5}, sprite: 3,
	},
}

var playgroundSproutLimbs = [playgroundMaxSprouts]playgroundLimb{
	{
		points: [4]playgroundPoint{{x: 30, y: 25}, {x: 22, y: 23}, {x: 15, y: 19}, {x: 10, y: 18}},
		count:  4, cluster: playgroundPoint{x: 11, y: 16.5}, sprite: 2,
	},
	{
		points: [4]playgroundPoint{{x: 29, y: 18}, {x: 38, y: 17}, {x: 46, y: 15}, {x: 52, y: 15}},
		count:  4, cluster: playgroundPoint{x: 52, y: 13.5}, sprite: 0,
	},
	{
		points: [4]playgroundPoint{{x: 31, y: 29}, {x: 27, y: 28}, {x: 24, y: 25}},
		count:  3, cluster: playgroundPoint{x: 22.5, y: 24}, sprite: 1,
	},
	{
		points: [4]playgroundPoint{{x: 28, y: 21}, {x: 33, y: 20}, {x: 36, y: 17}},
		count:  3, cluster: playgroundPoint{x: 36, y: 15.5}, sprite: 2,
	},
	{
		points: [4]playgroundPoint{{x: 28, y: 12}, {x: 24, y: 9}, {x: 22, y: 6}},
		count:  3, cluster: playgroundPoint{x: 21, y: 5}, sprite: 1,
	},
}

var playgroundSeedTracks = [3][4]playgroundPoint{
	{{x: 8, y: 8}, {x: 5, y: 13}, {x: 10, y: 17}, {x: 6, y: 11}},
	{{x: 55, y: 6}, {x: 58, y: 11}, {x: 54, y: 16}, {x: 57, y: 9}},
	{{x: 46, y: 3}, {x: 50, y: 5}, {x: 47, y: 9}, {x: 44, y: 6}},
}

var playgroundSeedOffsets = [3]struct {
	x, y, velocity float64
}{
	{x: -2.4, velocity: 3.2},
	{x: 1.8, y: -1.2, velocity: -2.4},
	{x: -1.2, y: 1.4, velocity: 2.1},
}

var playgroundPaletteNames = [playgroundPalettes]string{"mint", "lime", "rose"}

type playgroundPixel uint8

const (
	pixelSky playgroundPixel = iota
	pixelStar
	pixelDistant
	pixelBarkShadow
	pixelBark
	pixelBarkLight
	pixelSoil
	pixelSoilLight
	pixelSoilRock
	pixelLeafShadow
	pixelLeafDark
	pixelLeaf
	pixelLeafLight
	pixelAccent
	pixelFirefly
	pixelTrail
	pixelPetal
	pixelPetalDim
	playgroundPixelKinds
)

var playgroundPaletteColors = [playgroundPalettes][playgroundPixelKinds]color.Color{
	{
		pixelSky: tone.Pepper, pixelStar: tone.Squid, pixelDistant: tone.Char,
		pixelBarkShadow: tone.Toast, pixelBark: tone.Paprika, pixelBarkLight: tone.Salmon,
		pixelSoil: tone.Toast, pixelSoilLight: tone.Cumin, pixelSoilRock: tone.Oyster,
		pixelLeafShadow: tone.Spinach, pixelLeafDark: tone.Gator, pixelLeaf: tone.Guac, pixelLeafLight: tone.Bok,
		pixelAccent: tone.Zest, pixelFirefly: tone.Butter, pixelTrail: tone.Iron,
		pixelPetal: tone.Salmon, pixelPetalDim: tone.Paprika,
	},
	{
		pixelSky: tone.Pepper, pixelStar: tone.Squid, pixelDistant: tone.Char,
		pixelBarkShadow: tone.Toast, pixelBark: tone.Paprika, pixelBarkLight: tone.Salmon,
		pixelSoil: tone.Toast, pixelSoilLight: tone.Cumin, pixelSoilRock: tone.Oyster,
		pixelLeafShadow: tone.Spinach, pixelLeafDark: tone.Pickle, pixelLeaf: tone.Citron, pixelLeafLight: tone.Zest,
		pixelAccent: tone.Dolly, pixelFirefly: tone.Butter, pixelTrail: tone.Iron,
		pixelPetal: tone.Dolly, pixelPetalDim: tone.Pom,
	},
	{
		pixelSky: tone.Pepper, pixelStar: tone.Squid, pixelDistant: tone.Char,
		pixelBarkShadow: tone.Toast, pixelBark: tone.Paprika, pixelBarkLight: tone.Salmon,
		pixelSoil: tone.Toast, pixelSoilLight: tone.Cumin, pixelSoilRock: tone.Oyster,
		pixelLeafShadow: tone.Toast, pixelLeafDark: tone.Pom, pixelLeaf: tone.Sriracha, pixelLeafLight: tone.Tuna,
		pixelAccent: tone.Zest, pixelFirefly: tone.Butter, pixelTrail: tone.Iron,
		pixelPetal: tone.Yam, pixelPetalDim: tone.Paprika,
	},
}

// Half-block cells are rendered once for every possible pair of vertical
// pixels. View only concatenates these immutable strings; it never constructs
// a Lip Gloss style for an individual cell.
var playgroundPixelGlyphs = makePlaygroundPixelGlyphs()

func makePlaygroundPixelGlyphs() [playgroundPalettes][playgroundPixelKinds][playgroundPixelKinds]string {
	var glyphs [playgroundPalettes][playgroundPixelKinds][playgroundPixelKinds]string
	for palette := range playgroundPalettes {
		colors := playgroundPaletteColors[palette]
		for top := range playgroundPixelKinds {
			for bottom := range playgroundPixelKinds {
				glyphs[palette][top][bottom] = lipgloss.NewStyle().
					Foreground(colors[top]).Background(colors[bottom]).Render("▀")
			}
		}
	}
	return glyphs
}

// These sprite matrices are source pixels, not terminal output. x/d/m/l form
// deep-shadow, dark, midtone and lit pixels. Ordered transition dithering is
// added only at those boundaries; dots preserve deliberate pockets of sky.
var playgroundLeafSprites = [4][9]string{
	{
		"......mmm........",
		"....mmllmmm......",
		"..dlmmmmmmmll....",
		".ddmmmmmmmmmmma..",
		"ddmmmmmmmmmmmmmd.",
		"xddmmmm.mmmmmddx.",
		"xxdddmmmmmmdddxx.",
		".xxxdddddddddxxx.",
		"...xxxdddddxxx...",
	},
	{
		"........mm.......",
		"......mmmmll.....",
		"....dmmmmmmmll...",
		"..ddmmmmmmmmmla..",
		".ddmmmmmmmmmmlll.",
		"xxdmmmm.mmmmmmddx",
		"xxxdddmmmmmmdddxx",
		".xxxxddddddxxxx..",
		"...xxxxdddxxxx...",
	},
	{
		".......mm........",
		"......mmlm.......",
		".....mmmmml......",
		"....dmmmmmmll....",
		"...ddmmmmmmma....",
		"..xddmmmmmmddx...",
		"...xxdmmmmdxx....",
		"....xxxdddxxx....",
		".....xxxxxxx.....",
	},
	{
		".....mmmmm.......",
		"...mmmllmmmmm....",
		"..dmmmmmmmmmall..",
		".ddmmmmmmmmmmmll.",
		"dddmmmmmmmmmmmddd",
		"xddmm.mmmmmmm.ddx",
		"xxdddmmmmmmmdddxx",
		".xxxxddddddxxxx..",
		"....xxxxxxxxx....",
	},
}

var playgroundDither = [4][4]uint8{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

type playgroundRaster struct {
	width, height int
	cells         []playgroundPixel
}

func newPlaygroundRaster(width, height int) playgroundRaster {
	if width <= 0 || height <= 0 {
		return playgroundRaster{}
	}
	return playgroundRaster{width: width, height: height, cells: make([]playgroundPixel, width*height)}
}

func (r *playgroundRaster) set(x, y int, pixel playgroundPixel) bool {
	if r == nil || x < 0 || y < 0 || x >= r.width || y >= r.height {
		return false
	}
	r.cells[y*r.width+x] = pixel
	return true
}

func (r playgroundRaster) rows(palette int) []string {
	if r.width <= 0 || r.height <= 0 {
		return nil
	}
	if palette < 0 || palette >= playgroundPalettes {
		palette = 0
	}
	rows := make([]string, 0, (r.height+1)/2)
	for y := 0; y < r.height; y += 2 {
		var row strings.Builder
		row.Grow(r.width * 3)
		for x := range r.width {
			top := r.cells[y*r.width+x]
			bottom := pixelSky
			if y+1 < r.height {
				bottom = r.cells[(y+1)*r.width+x]
			}
			if top == pixelSky && bottom == pixelSky {
				row.WriteByte(' ')
				continue
			}
			row.WriteString(playgroundPixelGlyphs[palette][top][bottom])
		}
		rows = append(rows, row.String())
	}
	return rows
}

type playgroundScene struct {
	raster           *playgroundRaster
	trunk            float64
	boughs, canopies [playgroundBoughCount]float64
}

func (s playgroundScene) fixedPoint(point playgroundPoint) (int, int) {
	x := int(math.Round(point.x * float64(s.raster.width) / playgroundCanvasWidth))
	y := int(math.Round(point.y * float64(s.raster.height) / playgroundCanvasHeight))
	return x, y
}

func (s playgroundScene) point(point playgroundPoint) (int, int) {
	flex := min(1, max(0, (playgroundGroundY-point.y)/(playgroundGroundY-2)))
	// The root is pinned. Flex increases quadratically toward the crown.
	point.x += s.trunk * flex * flex
	return s.fixedPoint(point)
}

func (s playgroundScene) boughPath(limb playgroundLimb, index int) [4]playgroundPoint {
	points := limb.points
	bough := s.boughs[index]
	canopy := s.canopies[index]
	for i := 1; i < limb.count; i++ {
		along := float64(i) / float64(limb.count-1)
		points[i].x += bough * 0.68 * along * along
		// A small vertical bow makes the canopy feel woody rather than like a
		// collection of independent horizontal sliders.
		points[i].y += (canopy - bough) * 0.055 * along
	}
	return points
}

func (s playgroundScene) canopyPoint(point playgroundPoint, index int) (int, int) {
	x, y := s.point(point)
	drift := s.boughs[index]*0.68 + s.canopies[index]*0.76
	x += int(math.Round(drift * float64(s.raster.width) / playgroundCanvasWidth))
	return x, y
}

func (s playgroundScene) plotFixed(point playgroundPoint, pixel playgroundPixel) {
	x, y := s.fixedPoint(point)
	s.raster.set(x, y, pixel)
}

func (s playgroundScene) disk(point playgroundPoint, radius float64, pixel playgroundPixel) {
	cx, cy := s.point(point)
	scale := min(
		float64(s.raster.width)/playgroundCanvasWidth,
		float64(s.raster.height)/playgroundCanvasHeight,
	)
	r := int(math.Round(radius * scale))
	if r <= 0 {
		s.raster.set(cx, cy, pixel)
		return
	}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r+r/2 {
				s.raster.set(cx+dx, cy+dy, pixel)
			}
		}
	}
}

// stroke draws a tapered path, stopping at growth along its arc length. The
// final radius is zero: limbs end in single pixels rather than box connectors.
func (s playgroundScene) stroke(points []playgroundPoint, growth, startRadius, offsetX float64, pixel playgroundPixel) {
	if len(points) < 2 || growth <= 0 {
		return
	}
	total := 0.0
	for i := 1; i < len(points); i++ {
		total += playgroundDistance(points[i-1], points[i])
	}
	if total <= 0 {
		return
	}
	growth = min(1, growth)
	limit := total * growth
	walked := 0.0
	for i := 1; i < len(points) && walked < limit; i++ {
		from, to := points[i-1], points[i]
		from.x += offsetX
		to.x += offsetX
		segment := playgroundDistance(points[i-1], points[i])
		visible := min(segment, limit-walked)
		fraction := visible / segment
		end := playgroundPoint{x: from.x + (to.x-from.x)*fraction, y: from.y + (to.y-from.y)*fraction}
		x0, y0 := s.point(from)
		x1, y1 := s.point(end)
		steps := max(1, int(math.Ceil(max(math.Abs(float64(x1-x0)), math.Abs(float64(y1-y0)))*2)))
		for step := range steps + 1 {
			t := float64(step) / float64(steps)
			point := playgroundPoint{x: from.x + (end.x-from.x)*t, y: from.y + (end.y-from.y)*t}
			along := (walked + visible*t) / total
			radius := startRadius * (1 - along)
			s.disk(point, radius, pixel)
		}
		walked += segment
	}
}

func playgroundDistance(a, b playgroundPoint) float64 {
	return math.Hypot(a.x-b.x, a.y-b.y)
}

// renderRaster is a pure scene constructor. Harmonica positions were advanced
// earlier by Tick; this function only quantizes them into pixels.
func (p pixelPlayground) renderRaster(width, height int) playgroundRaster {
	raster := newPlaygroundRaster(width, height)
	if raster.width == 0 {
		return raster
	}
	scene := playgroundScene{raster: &raster, trunk: p.trunk.position}
	for i := range playgroundBoughCount {
		scene.boughs[i] = p.boughs[i].position
		scene.canopies[i] = p.canopies[i].position
	}

	// Sparse fixed sky pixels and two tiny, shaded distant saplings add depth
	// without competing with the hero canopy.
	for _, star := range []playgroundPoint{{x: 6, y: 4}, {x: 57, y: 3}, {x: 52, y: 25}, {x: 10, y: 26}} {
		scene.plotFixed(star, pixelStar)
	}
	for _, plant := range []struct {
		x, y, direction float64
	}{{x: 4.5, y: 32, direction: -1}, {x: 58.5, y: 32, direction: 1}} {
		for dy := 0.0; dy < 3; dy++ {
			scene.plotFixed(playgroundPoint{x: plant.x, y: plant.y - dy}, pixelDistant)
		}
		scene.plotFixed(playgroundPoint{x: plant.x + plant.direction, y: plant.y - 2}, pixelLeafShadow)
		scene.plotFixed(playgroundPoint{x: plant.x - plant.direction, y: plant.y - 3}, pixelLeafShadow)
		scene.plotFixed(playgroundPoint{x: plant.x + plant.direction, y: plant.y - 4}, pixelDistant)
	}

	// A wider, asymmetrical soil mound has a warm broken rim, a dark body, and
	// only a handful of rock pixels. The ground stays quiet under the roots.
	for y := 31.0; y <= 36; y++ {
		for x := 11.0; x <= 52; x++ {
			dx, dy := (x-31.5)/20.0, (y-34)/2.55
			if dx*dx+dy*dy > 1 {
				continue
			}
			pixel := pixelSoil
			if y <= 32 || (y == 33 && (int(x)+int(y))%7 < 2) {
				pixel = pixelSoilLight
			}
			scene.plotFixed(playgroundPoint{x: x, y: y}, pixel)
		}
	}
	for _, rock := range []playgroundPoint{{x: 20, y: 33}, {x: 39, y: 34}, {x: 47, y: 33}} {
		scene.plotFixed(rock, pixelSoilRock)
	}

	// Grass and one tiny new shoot are placed beside, not across, the root flare.
	for _, blade := range []playgroundPoint{
		{x: 18, y: 32},
		{x: 18, y: 31},
		{x: 17, y: 31},
		{x: 19, y: 30},
		{x: 45, y: 32},
		{x: 45, y: 31},
		{x: 46, y: 31},
		{x: 44, y: 30},
		{x: 24, y: 32},
		{x: 23, y: 31},
	} {
		scene.plotFixed(blade, pixelLeafDark)
	}
	for _, tip := range []playgroundPoint{{x: 17, y: 30}, {x: 46, y: 30}} {
		scene.plotFixed(tip, pixelLeaf)
	}

	// Four irregular roots spread over the mound. Two-tone strokes expose the
	// flare while tapering to single pixels that appear to grip the soil.
	for i, count := range playgroundRootCounts {
		root := playgroundRoots[i]
		scene.stroke(root[:count], 1, 1.45, 0, pixelBarkShadow)
		scene.stroke(root[:count], 1, 0.8, -0.18, pixelBark)
	}
	scene.stroke(playgroundTrunk[:], 1, 2.1, 0.55, pixelBarkShadow)
	scene.stroke(playgroundTrunk[:], 1, 1.9, 0, pixelBark)
	scene.stroke(playgroundTrunk[:], 1, 0, -0.72, pixelBarkLight)

	// Bough endpoints receive their own lagged displacement before rasterizing.
	// A smaller trailing twig remains visible through the intentional leaf gaps.
	for i, limb := range playgroundBaseLimbs {
		path := scene.boughPath(limb, i)
		scene.stroke(path[:limb.count], 1, 1.12, 0.2, pixelBarkShadow)
		scene.stroke(path[:limb.count], 1, 0.78, -0.18, pixelBark)
		tip := path[limb.count-1]
		direction := 1.0
		if limb.cluster.x < 32 {
			direction = -1
		}
		twig := []playgroundPoint{path[limb.count-2], {x: tip.x + 2*direction, y: tip.y - 2}, {x: tip.x + 4*direction, y: tip.y - 1.5}}
		scene.stroke(twig, 1, 0, 0, pixelBarkLight)
	}
	for i := range p.sprouts {
		index := playgroundBaseBoughs + i
		limb := playgroundSproutLimbs[i]
		path := scene.boughPath(limb, index)
		scene.stroke(path[:limb.count], p.growth[i].position, 1.08, 0.18, pixelBarkShadow)
		scene.stroke(path[:limb.count], p.growth[i].position, 0.75, -0.15, pixelBark)
	}

	bloomScale := 0.72 + 0.28*min(1.2, max(0, p.bloom.position))
	// Back and upper masses go down first; broad lower pads overlap them. This
	// makes shadow layers read as canopy depth instead of unrelated blobs.
	for _, i := range [...]int{5, 2, 3, 0, 1, 4} {
		limb := playgroundBaseLimbs[i]
		scene.leafCluster(limb.cluster, limb.sprite, bloomScale, i)
	}
	for i := range p.sprouts {
		index := playgroundBaseBoughs + i
		limb := playgroundSproutLimbs[i]
		growth := min(1.18, max(0, p.growth[i].position))
		scene.leafCluster(limb.cluster, limb.sprite, bloomScale*growth, index)
	}

	for _, petal := range p.petals {
		if !petal.active {
			continue
		}
		pixel := pixelPetal
		if petal.age*3 > petal.ttl*2 {
			pixel = pixelPetalDim
		}
		scene.plotFixed(playgroundPoint{x: petal.x.position, y: petal.y.position}, pixel)
	}
	for i, seed := range p.seeds {
		// Two low-contrast cells retain a hint of velocity without adding a new
		// particle system or filling the empty sky with noise.
		for trail := 1.0; trail <= 2; trail++ {
			scene.plotFixed(playgroundPoint{
				x: seed.x.position - seed.x.velocity*0.12*trail,
				y: seed.y.position - seed.y.velocity*0.12*trail,
			}, pixelTrail)
		}
		pixel := pixelFirefly
		if i == 1 {
			pixel = pixelAccent
		}
		scene.plotFixed(playgroundPoint{x: seed.x.position, y: seed.y.position}, pixel)
	}
	return raster
}

func (s playgroundScene) leafCluster(center playgroundPoint, sprite int, scale float64, bough int) {
	if scale <= 0.04 {
		return
	}
	matrix := playgroundLeafSprites[sprite%len(playgroundLeafSprites)]
	sourceWidth, sourceHeight := len(matrix[0]), len(matrix)
	width := max(2, int(math.Round(float64(sourceWidth)*float64(s.raster.width)/playgroundCanvasWidth*scale)))
	height := max(1, int(math.Round(float64(sourceHeight)*float64(s.raster.height)/playgroundCanvasHeight*scale)))
	cx, cy := s.canopyPoint(center, bough)
	left, top := cx-width/2, cy-height/2
	chain := s.canopies[bough] - s.boughs[bough]
	for y := range height {
		sourceY := min(sourceHeight-1, y*sourceHeight/height)
		row := matrix[sourceY]
		for x := range width {
			sourceX := min(sourceWidth-1, x*sourceWidth/width)
			if sourceX >= len(row) || row[sourceX] == '.' {
				continue
			}
			pixel := pixelLeaf
			switch row[sourceX] {
			case 'x':
				pixel = pixelLeafShadow
				if playgroundDither[sourceY%4][sourceX%4] < 3 {
					pixel = pixelLeafDark
				}
			case 'd':
				pixel = pixelLeafDark
				if sourceY <= 3 && playgroundDither[sourceY%4][sourceX%4] < 2 {
					pixel = pixelLeaf
				}
			case 'm':
				if sourceY >= 4 && playgroundDither[sourceY%4][sourceX%4] < 3 {
					pixel = pixelLeafDark
				}
			case 'l':
				pixel = pixelLeafLight
			case 'a':
				pixel = pixelAccent
			}
			// Only the outer leaf pixels receive this last, fractional link in the
			// chain. It is enough to make edges flutter without deforming the pad.
			relative := float64(x-width/2) / float64(max(1, width))
			shift := int(math.Round(chain * 0.32 * relative))
			s.raster.set(left+x+shift, top+y, pixel)
		}
	}
}

// View draws a complete body. It has no timers, randomness, or spring updates;
// frame remains in the signature for the welcome lifecycle, while Tick owns
// the simulation's actual frame state.
func (p pixelPlayground) View(width, height, frame int, status string) string {
	_ = frame
	if height <= 0 {
		return ""
	}
	if width <= 0 {
		return strings.Repeat("\n", height-1)
	}
	contentWidth := min(width, playgroundContentWidth)
	repository := strings.NewReplacer("\r", " ", "\n", " ").Replace(status)
	if repository == "" {
		repository = "a quiet patch of sky"
	}
	title := centerPlaygroundRow(p.styles.title.Render("twig · bonsai"), contentWidth)
	repository = centerPlaygroundRow(p.styles.repository.Render(repository), contentWidth)
	action := centerPlaygroundRow(p.styles.action.Render(p.status), contentWidth)
	hints := p.renderHints([][2]string{{"h/l", "wind"}, {"b", "grow"}, {"c", "color"}, {"z", "prune"}, {"x", "replant"}, {"m", "bloom"}, {"space", "gust"}})
	hints = centerPlaygroundRow(hints, contentWidth)

	var before, after, art []string
	switch {
	case height >= 5:
		before = []string{title, repository}
		after = []string{hints, action}
		artRows := min(playgroundRasterRows, height-4)
		rasterWidth := min(playgroundRasterWidth, max(1, contentWidth-8))
		art = p.renderRaster(rasterWidth, artRows*2).rows(p.palette)
	case height == 4:
		before = []string{title}
		after = []string{hints, action}
		rasterWidth := min(playgroundRasterWidth, max(1, contentWidth-8))
		art = p.renderRaster(rasterWidth, 2).rows(p.palette)
	case height == 3:
		before = []string{title}
		after = []string{hints, action}
	case height == 2:
		before = []string{title}
		after = []string{hints}
	default:
		before = []string{title}
	}

	rows := make([]string, 0, height)
	rows = append(rows, before...)
	for _, row := range art {
		rows = append(rows, centerPlaygroundRow(row, contentWidth))
	}
	rows = append(rows, after...)
	if len(rows) > height {
		rows = rows[:height]
	}

	left := (width - contentWidth) / 2
	top := max(0, (height-len(rows))/2)
	var out strings.Builder
	for row := range height {
		if row > 0 {
			out.WriteByte('\n')
		}
		index := row - top
		if index < 0 || index >= len(rows) {
			out.WriteString(strings.Repeat(" ", width))
			continue
		}
		line := fitPlaygroundRow(rows[index], contentWidth)
		out.WriteString(strings.Repeat(" ", left))
		out.WriteString(line)
		out.WriteString(strings.Repeat(" ", max(0, width-left-lipgloss.Width(line))))
	}
	return out.String()
}

func (p pixelPlayground) renderHints(pairs [][2]string) string {
	var b strings.Builder
	for i, pair := range pairs {
		if i > 0 {
			b.WriteString(p.styles.hintText.Render(" · "))
		}
		b.WriteString(p.styles.hintKey.Render(pair[0]))
		b.WriteByte(' ')
		b.WriteString(p.styles.hintText.Render(pair[1]))
	}
	return b.String()
}

func centerPlaygroundRow(row string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(row) > width {
		row = ansi.Truncate(row, width, "…")
	}
	padding := max(0, width-lipgloss.Width(row))
	left := padding / 2
	return strings.Repeat(" ", left) + row + strings.Repeat(" ", padding-left)
}

func fitPlaygroundRow(row string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(row) > width {
		row = ansi.Truncate(row, width, "…")
	}
	if padding := width - lipgloss.Width(row); padding > 0 {
		row += strings.Repeat(" ", padding)
	}
	return row
}
