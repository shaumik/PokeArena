package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

const logTail = 8 // max log lines shown at the bottom of the battle screen

// View renders the current screen and fills the terminal with the LCD field so
// the Game Boy green reaches every edge. Plain literals inside a screen go
// through g() (see render.go) so no separator falls back to the terminal's
// default background mid-line.
func (m model) View() string {
	var s string
	switch m.screen {
	case screenConnecting:
		s = "\n" + g("  ") + stTitle.Render("PokéArena") + g("  ") + stDim.Render(m.status) +
			"\n\n" + g("  ") + stDim.Render("press q to quit")
	case screenRoom:
		s = m.viewRoom()
	case screenBattle:
		s = m.viewBattle()
	case screenEnded:
		s = m.viewEnded()
	}
	return lcd(s, m.width, m.height)
}

// ---- picker room ----

func (m model) viewRoom() string {
	var b strings.Builder
	b.WriteString(stTitle.Render(fmt.Sprintf("PokéArena · picker · battle %s", short(m.battleID))) + "\n\n")

	if m.room != nil {
		b.WriteString(g("  ") + slotLine("you", m.room.You) + "\n")
		b.WriteString(g("  ") + slotLine("opponent", m.room.Them) + "\n\n")
		if rem := time.Until(m.deadlineAt); rem > 0 && !m.submitted {
			b.WriteString(g("  ") + stDim.Render(fmt.Sprintf("submit within %ds", int(rem.Seconds()))) + "\n\n")
		}
		if m.inviteURL != "" && !m.room.Them.Attached {
			b.WriteString(g("  ") + stStatus.Render("opponent joins with:") + "\n" + g("  ") + stKey.Render(m.inviteURL) + "\n\n")
		}
	}

	b.WriteString(stTitle.Render("your team") + "\n")
	for i, mon := range m.teamView {
		names := make([]string, 0, len(mon.moveIDs))
		for _, id := range mon.moveIDs {
			if mv, ok := m.dex.Moves[id]; ok {
				names = append(names, mv.Name)
			} else {
				names = append(names, id)
			}
		}
		b.WriteString(g("  ") + stKey.Render(fmt.Sprintf("[%d]", i+1)) + g(" ") +
			stDim.Render(fmt.Sprintf("%-12s %-12s %s", mon.name, typeLabel(mon.t1, mon.t2), strings.Join(names, ", "))) + "\n")
	}

	b.WriteString("\n")
	switch {
	case m.submitted:
		b.WriteString(g("  ") + stWin.Render("team submitted — waiting for opponent…") + "\n")
	case m.submitting:
		b.WriteString(g("  ") + stStatus.Render("submitting…") + "\n")
	default:
		b.WriteString(g("  ") + controls(
			kv("1-6", "re-roll slot"), kv("r", "re-roll all"), kv("enter", "submit"), kv("q", "quit")) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + g("  ") + stStatus.Render(m.status) + "\n")
	}
	return b.String()
}

// slotLine renders one side's picker-room progress: attach + submit ticks.
func slotLine(label string, s protocol.RoomSlot) string {
	tick := func(ok bool) string {
		if ok {
			return stWin.Render("✓")
		}
		return stDim.Render("…")
	}
	name := s.Trainer
	if name == "" {
		name = label
	}
	return stDim.Render(fmt.Sprintf("%-10s attached ", name)) + tick(s.Attached) +
		stDim.Render("  submitted ") + tick(s.Submitted)
}

// ---- active battle ----

func (m model) viewBattle() string {
	v := m.view

	header := stTitle.Render(fmt.Sprintf("PokéArena · battle %s · turn %d · %s",
		short(m.battleID), v.Turn, battlePhase(v)))

	// Gen-1 diagonal arena: foe stat box (top-left) faces its sprite (top-
	// right); your sprite (bottom-left) faces your stat box (bottom-right). On a
	// terminal too narrow for a sprite beside the box, spriteSizes returns 0 and
	// the stat box stands alone. greenRow/greenStack keep the join padding on the
	// LCD field rather than the terminal default.
	front, back := m.spriteSizes()
	top := m.foeStatBox(v)
	if fs := m.foeSpriteBlock(v, front); fs != "" {
		top = greenRow(lipgloss.Top, top, g("  "), fs)
	}
	bottom := m.selfStatBox(v)
	if ss := m.selfSpriteBlock(v, back); ss != "" {
		bottom = greenRow(lipgloss.Bottom, ss, g("  "), bottom)
	}
	arena := greenStack(top, m.fieldLine(v), bottom)

	var b strings.Builder
	b.WriteString(header + "\n\n")
	b.WriteString(arena + "\n\n")
	b.WriteString(m.viewLog() + "\n")
	b.WriteString(m.viewControls(v))
	if m.status != "" {
		b.WriteString("\n" + g("  ") + stStatus.Render(m.status))
	}
	return b.String()
}

func battlePhase(v *battleView) string {
	if v.Replace {
		return "replace"
	}
	return string(v.Phase)
}

// foeStatBox renders the opponent's name plate + HP (percentage only — the wire
// redacts the exact count) + bench count + revealed moves, in an LCD panel.
func (m model) foeStatBox(v *battleView) string {
	head := stOpp.Render(v.Foe.Name) + g("  ") + stDim.Render(typeLabel(v.Foe.Type1, v.Foe.Type2)) +
		statusTag(v.Foe.Status) + boostTag(v.Foe.Stages)
	bar := g("HP ") + hpBar(float64(v.Foe.HPPct)/100, 14) + g(fmt.Sprintf(" ~%d%%", v.Foe.HPPct))
	parts := []string{head, bar, stDim.Render("bench " + benchCount(v.FoeBenchAlive))}
	if moves := revealedMoves(m.dex, v.Foe.Moves); moves != "" {
		parts = append(parts, stDim.Render(moves))
	}
	return stPanel.Render(strings.Join(parts, "\n"))
}

// selfStatBox renders your active's name plate + exact HP + bench, in a panel.
func (m model) selfStatBox(v *battleView) string {
	me := v.Self.Team[v.Self.Active]
	head := stYou.Render(me.Name) + g("  ") + stDim.Render(typeLabel(me.Type1, me.Type2)) +
		statusTag(me.Status) + boostTag(me.Stages)
	frac := 0.0
	if me.MaxHP > 0 {
		frac = float64(me.HP) / float64(me.MaxHP)
	}
	bar := g("HP ") + hpBar(frac, 14) + g(fmt.Sprintf(" %d/%d", me.HP, me.MaxHP))
	meta := stDim.Render("bench " + benchDots(v.Self.Team, v.Self.Active))
	return stPanel.Render(strings.Join([]string{head, bar, meta}, "\n"))
}

func (m model) foeSpriteBlock(v *battleView, px int) string {
	if px <= 0 {
		return ""
	}
	var sp *sprite
	if m.foeDexNo > 0 {
		sp = foeSprite(m.foeDexNo, px)
	}
	return spriteBlock(sp, px, px/2, m.spriteFrame)
}

func (m model) selfSpriteBlock(v *battleView, px int) string {
	if px <= 0 {
		return ""
	}
	sp := selfSprite(v.Self.Team[v.Self.Active].DexNo, px)
	return spriteBlock(sp, px, px/2, 0)
}

// spriteBlock joins a sprite frame's lines, or paints an LCD-green rectangle of
// the same footprint when the sprite is missing, so the layout never shifts.
func spriteBlock(sp *sprite, cols, rows, frame int) string {
	if lines := sp.frame(frame); lines != nil {
		return strings.Join(lines, "\n")
	}
	blank := stScreen.Render(strings.Repeat(" ", cols))
	out := make([]string, rows)
	for i := range out {
		out[i] = blank
	}
	return strings.Join(out, "\n")
}

func (m model) fieldLine(v *battleView) string {
	parts := []string{}
	if field := fieldStrip(v); field != "" {
		parts = append(parts, field)
	}
	if c := sideCondTag(v.Self.Conditions); c != "" {
		parts = append(parts, "you: "+c)
	}
	if c := sideCondTag(v.FoeConditions); c != "" {
		parts = append(parts, "foe: "+c)
	}
	if len(parts) == 0 {
		return stDim.Render("  field: clear")
	}
	return stDim.Render("  field: " + strings.Join(parts, "  ·  "))
}

func (m model) viewLog() string {
	n := m.logLines()
	lines := m.log
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(logSideTag(l.Side, m.meSide) + g("  "+l.Text) + "\n")
	}
	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		content = stDim.Render("(the battle log will appear here)")
	}
	return stDim.Render("  log") + "\n" + stDialog.Width(m.boxWidth()).Render(content)
}

// boxWidth fits the dialog boxes (log, action menu) to the terminal so they
// never overflow a narrow window (content + 2 border cols); capped at the
// comfortable default otherwise.
func (m model) boxWidth() int {
	if m.width > 0 {
		return min(max(m.width-2, 10), 58)
	}
	return 58
}

// viewControls renders the action menu as a Gen-1 dialog box: the four move
// slots in fixed columns (key chip, name, type tag, PP) with the switch row
// underneath. Unusable slots render dim. Forced turns (charge, rampage,
// recharge) normally never reach this menu — maybeAutoAct plays them first —
// so an [s] row here is almost always genuine Struggle.
func (m model) viewControls(v *battleView) string {
	if !m.needsAction {
		return g("  ") + stDim.Render("waiting for opponent…")
	}
	acts := ai.LegalActions(v.toAIView())

	if v.Replace {
		return stDim.Render("  choose a replacement") + "\n" +
			stDialog.Width(m.boxWidth()).Render(m.switchLine(v, acts))
	}

	me := v.Self.Team[v.Self.Active]
	cells := []string{}
	for i := 0; i < len(me.Moves) && i < 4; i++ {
		if me.Moves[i].MoveID == "" {
			continue
		}
		cells = append(cells, m.moveCell(me.Moves[i], i, acts))
	}
	rows := []string{}
	for i := 0; i < len(cells); i += 2 {
		row := cells[i]
		if i+1 < len(cells) {
			row += g("  ") + cells[i+1]
		}
		rows = append(rows, row)
	}
	if _, ok := findAction(acts, engine.ActionMove, -1); ok {
		rows = append(rows, m.struggleRow(me))
	}
	rows = append(rows, m.switchLine(v, acts))
	return stDim.Render("  your move") + "\n" +
		stDialog.Width(m.boxWidth()).Render(strings.Join(rows, "\n"))
}

// moveCell renders one move slot in fixed columns — chip, 12-char name,
// 3-letter type, PP — so the grid stays aligned (an untruncated full type
// name like "electric" used to blow the columns out and made the menu hard
// to scan). Usable slots get an inverse key chip and a bold name; unusable
// ones render entirely dim.
func (m model) moveCell(slot engine.MoveSlot, i int, acts []engine.Action) string {
	name, typ := slot.MoveID, ""
	if mv, ok := m.dex.Moves[slot.MoveID]; ok {
		name, typ = mv.Name, typeAbbr(mv.Type)
	}
	if len(name) > 12 {
		name = name[:12]
	}
	meta := fmt.Sprintf(" %-3s %2d/%-2d", typ, slot.PP, slot.MaxPP)
	if _, ok := findAction(acts, engine.ActionMove, i); ok {
		return stKey.Render(fmt.Sprintf("[%d]", i+1)) +
			stMove.Render(fmt.Sprintf(" %-12s", name)) + stDim.Render(meta)
	}
	return stDim.Render(fmt.Sprintf("[%d] %-12s%s", i+1, name, meta))
}

// struggleRow labels the engine's index -1 sentinel correctly: Struggle when
// the user is out of usable moves. The other -1 producer — a Hyper Beam
// recharge — is normally auto-played before the menu renders; if it does
// reach the menu (e.g. after a rejected send), say "Recharge", not
// "Struggle".
func (m model) struggleRow(me engine.Pokemon) string {
	if me.Volatiles.MustRecharge {
		return stKey.Render("[s]") + stMove.Render(" Recharge") + stDim.Render("  ("+me.Name+" must recharge)")
	}
	return stKey.Render("[s]") + stMove.Render(" Struggle") + stDim.Render("  (no usable moves)")
}

// switchLine renders the legal switch targets as lettered options keyed by
// team index (a=slot0, b=slot1, …).
func (m model) switchLine(v *battleView, acts []engine.Action) string {
	parts := []string{}
	for i, p := range v.Self.Team {
		if _, ok := findAction(acts, engine.ActionSwitch, i); !ok {
			continue
		}
		letter := string(rune('a' + i))
		parts = append(parts, stKey.Render("["+letter+"]")+stMove.Render(" "+p.Name))
	}
	if len(parts) == 0 {
		return stDim.Render("switch  (none available)")
	}
	return stDim.Render("switch  ") + strings.Join(parts, g("  "))
}

// ---- end ----

func (m model) viewEnded() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("PokéArena · battle "+short(m.battleID)) + "\n\n")
	switch {
	case m.disconnErr != nil && m.winner == nil:
		b.WriteString(g("  ") + stWarn.Render("⚠ "+m.status) + "\n")
	case m.winner == nil:
		b.WriteString(g("  ") + stStatus.Render("battle ended — no winner (abandoned)") + "\n")
	case *m.winner == m.meSide:
		b.WriteString(g("  ") + stWin.Render("🏆 you won!") + "\n")
	default:
		b.WriteString(g("  ") + stLose.Render("💀 you lost.") + "\n")
	}
	if m.view != nil {
		b.WriteString("\n" + m.viewLog() + "\n")
	}
	b.WriteString("\n" + g("  ") + stDim.Render("press q to quit"))
	return b.String()
}

// ---- small helpers ----

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func kv(key, desc string) string { return stKey.Render(key) + g(" ") + stDim.Render(desc) }

func controls(parts ...string) string { return strings.Join(parts, stDim.Render("  ·  ")) }
