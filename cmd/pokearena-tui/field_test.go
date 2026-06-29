package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"pokearena"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// sgrSetsBg folds one SGR parameter list into the running background state:
// 0/49 clear it; 48;5;n, 48;2;r;g;b, 40-47 and 100-107 set it.
func sgrSetsBg(bg bool, params string) bool {
	toks := strings.Split(params, ";")
	for k := 0; k < len(toks); k++ {
		t := toks[k]
		switch {
		case t == "" || t == "0":
			bg = false
		case t == "49":
			bg = false
		case t == "48":
			bg = true
			if k+1 < len(toks) && toks[k+1] == "5" {
				k += 2
			} else if k+1 < len(toks) && toks[k+1] == "2" {
				k += 4
			}
		default:
			if (len(t) == 2 && t[0] == '4' && t[1] >= '0' && t[1] <= '7') ||
				(len(t) == 3 && t[0] == '1' && t[1] == '0' && t[2] >= '0' && t[2] <= '7') {
				bg = true
			}
		}
	}
	return bg
}

// bareCells counts printable cells rendered while no background is active —
// i.e. dark slivers in the LCD field. Newlines and escape bytes don't count.
func bareCells(s string) int {
	rs := []rune(s)
	bg, count, i := false, 0, 0
	for i < len(rs) {
		if rs[i] == '\x1b' && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && !(rs[j] >= 0x40 && rs[j] <= 0x7e) {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				bg = sgrSetsBg(bg, string(rs[i+2:j]))
			}
			i = j + 1
			continue
		}
		if rs[i] != '\n' {
			if !bg {
				count++
			}
		}
		i++
	}
	return count
}

// TestGreenFieldHasNoBareCells is the objective guard for the continuous LCD
// field: every screen, once filled, must have zero cells on the terminal's
// default background. It catches a separator or span that forgot to carry the
// screen background (the dark-sliver bug the review found).
func TestGreenFieldHasNoBareCells(t *testing.T) {
	// lipgloss strips colour when it can't detect a TTY (the test default), which
	// would make every cell read "bare". Force truecolor so the check sees the
	// backgrounds the real iTerm2 session renders, and restore it after.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	const w, h = 100, 44

	battle := newModel(nil, dex, "battle123", "p1")
	battle.setView(decodeBattleFrame(t, dex)) // resolves foeDexNo so the foe sprite renders
	battle.screen = screenBattle
	battle.needsAction = true
	battle.width, battle.height = w, h
	battle.status = "your move"
	battle.log = []engine.LogLine{{Side: -1, Text: "Battle started!"}, {Side: 0, Text: "Venusaur used Razor Leaf!"}, {Side: 1, Text: "Charizard fainted!"}}

	room := newModel(nil, dex, "battle123", "p1")
	room.team, room.teamView = autoTeam(dex)
	room.screen = screenRoom
	room.width, room.height = w, h
	room.inviteURL = "http://localhost:8080/?battle=x&slot=p2&token=t"
	room.room = &protocol.RoomUpdate{You: protocol.RoomSlot{Attached: true, Trainer: "Blue"}, Them: protocol.RoomSlot{}, DeadlineMS: 300000}
	room.deadlineAt = time.Now().Add(5 * time.Minute)

	ended := newModel(nil, dex, "battle123", "p1")
	ended.view = decodeBattleFrame(t, dex)
	ended.screen = screenEnded
	ended.width, ended.height = w, h
	win := 0
	ended.winner = &win

	for _, c := range []struct {
		name string
		m    model
	}{{"battle", battle}, {"room", room}, {"ended", ended}} {
		if n := bareCells(c.m.View()); n != 0 {
			t.Errorf("%s screen has %d bare (non-green) cells; the LCD field is not continuous", c.name, n)
		}
	}
}
