package main

import (
	"fmt"
	"strings"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

const logTail = 8 // log lines shown at the bottom of the battle screen

func (m model) View() string {
	switch m.screen {
	case screenConnecting:
		return "\n  " + stTitle.Render("PokéArena") + "  " + stDim.Render(m.status) +
			"\n\n  " + stDim.Render("press q to quit") + "\n"
	case screenRoom:
		return m.viewRoom()
	case screenBattle:
		return m.viewBattle()
	case screenEnded:
		return m.viewEnded()
	}
	return ""
}

// ---- picker room ----

func (m model) viewRoom() string {
	var b strings.Builder
	title := fmt.Sprintf("PokéArena · picker · battle %s", short(m.battleID))
	b.WriteString(stTitle.Render(title) + "\n\n")

	if m.room != nil {
		b.WriteString("  " + slotLine("you", m.room.You) + "\n")
		b.WriteString("  " + slotLine("opponent", m.room.Them) + "\n\n")
		if rem := time.Until(m.deadlineAt); rem > 0 && !m.submitted {
			b.WriteString("  " + stDim.Render(fmt.Sprintf("submit within %ds", int(rem.Seconds()))) + "\n\n")
		}
		if m.inviteURL != "" && !m.room.Them.Attached {
			b.WriteString("  " + stStatus.Render("opponent joins with:") + "\n  " + stKey.Render(m.inviteURL) + "\n\n")
		}
	}

	b.WriteString(stTitle.Render("  your team") + "\n")
	for i, mon := range m.teamView {
		names := make([]string, 0, len(mon.moveIDs))
		for _, id := range mon.moveIDs {
			if mv, ok := m.dex.Moves[id]; ok {
				names = append(names, mv.Name)
			} else {
				names = append(names, id)
			}
		}
		slot := stKey.Render(fmt.Sprintf("[%d]", i+1))
		b.WriteString(fmt.Sprintf("  %s %-12s %-12s %s\n",
			slot, mon.name, stDim.Render(typeLabel(mon.t1, mon.t2)), stDim.Render(strings.Join(names, ", "))))
	}

	b.WriteString("\n")
	if m.submitted {
		b.WriteString("  " + stWin.Render("team submitted — waiting for opponent…") + "\n")
	} else if m.submitting {
		b.WriteString("  " + stStatus.Render("submitting…") + "\n")
	} else {
		b.WriteString("  " + controls(
			kv("1-6", "re-roll slot"), kv("r", "re-roll all"), kv("enter", "submit"), kv("q", "quit")) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n  " + stStatus.Render(m.status) + "\n")
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
	return fmt.Sprintf("%-10s attached %s  submitted %s", name, tick(s.Attached), tick(s.Submitted))
}

// ---- active battle ----

func (m model) viewBattle() string {
	v := m.view
	width := 64

	// Foe (top).
	foeName := stOpp.Render(v.Foe.Name)
	foeHead := fmt.Sprintf("%s  %s%s%s",
		foeName, stDim.Render(typeLabel(v.Foe.Type1, v.Foe.Type2)),
		statusTag(v.Foe.Status), boostTag(v.Foe.Stages))
	foeBar := fmt.Sprintf("HP %s ~%d%%", hpBar(float64(v.Foe.HPPct)/100, 20), v.Foe.HPPct)
	foeMeta := stDim.Render(fmt.Sprintf("bench %s · %s",
		benchCount(v.FoeBenchAlive), revealedMoves(m.dex, v.Foe.Moves)))
	foe := strings.Join([]string{foeHead, foeBar, foeMeta}, "\n")

	// Field strip.
	field := fieldStrip(v)
	youCond := sideCondTag(v.Self.Conditions)
	foeCond := sideCondTag(v.FoeConditions)
	fieldParts := []string{}
	if field != "" {
		fieldParts = append(fieldParts, field)
	}
	if youCond != "" {
		fieldParts = append(fieldParts, "you: "+youCond)
	}
	if foeCond != "" {
		fieldParts = append(fieldParts, "foe: "+foeCond)
	}
	fieldLine := stDim.Render("field: clear")
	if len(fieldParts) > 0 {
		fieldLine = stDim.Render("field: " + strings.Join(fieldParts, "  ·  "))
	}

	// Self (bottom).
	me := v.Self.Team[v.Self.Active]
	selfHead := fmt.Sprintf("%s  %s%s%s",
		stYou.Render(me.Name), stDim.Render(typeLabel(me.Type1, me.Type2)),
		statusTag(me.Status), boostTag(me.Stages))
	frac := 0.0
	if me.MaxHP > 0 {
		frac = float64(me.HP) / float64(me.MaxHP)
	}
	selfBar := fmt.Sprintf("HP %s %d/%d", hpBar(frac, 20), me.HP, me.MaxHP)
	selfMeta := stDim.Render("bench " + benchDots(v.Self.Team, v.Self.Active))
	self := strings.Join([]string{selfHead, selfBar, selfMeta}, "\n")

	arena := stPanel.Width(width).Render(foe) + "\n" +
		"  " + fieldLine + "\n" +
		stPanel.Width(width).Render(self)

	// Header + log + controls.
	phase := string(v.Phase)
	if v.Replace {
		phase = "replace"
	}
	header := stTitle.Render(fmt.Sprintf("PokéArena · battle %s · turn %d · %s",
		short(m.battleID), v.Turn, phase))

	var b strings.Builder
	b.WriteString(header + "\n\n")
	b.WriteString(arena + "\n\n")
	b.WriteString(m.viewLog() + "\n")
	b.WriteString(m.viewControls(v) + "\n")
	if m.status != "" {
		b.WriteString("\n  " + stStatus.Render(m.status))
	}
	return b.String()
}

func (m model) viewLog() string {
	lines := m.log
	if len(lines) > logTail {
		lines = lines[len(lines)-logTail:]
	}
	var b strings.Builder
	b.WriteString(stDim.Render("  ── log ──") + "\n")
	for _, l := range lines {
		b.WriteString(fmt.Sprintf("  %s  %s\n", logSideTag(l.Side, m.meSide), l.Text))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) viewControls(v *battleView) string {
	if !m.needsAction {
		return "  " + stDim.Render("waiting for opponent…")
	}
	acts := ai.LegalActions(v.toAIView())
	me := v.Self.Team[v.Self.Active]

	if v.Replace {
		return "  " + stTitle.Render("choose a replacement:") + "\n  " + m.switchLine(v, acts)
	}

	var b strings.Builder
	b.WriteString("  " + stTitle.Render("your move:") + "\n")
	// Four fixed move slots, dimming illegal/empty/no-PP ones.
	cells := []string{}
	for i := 0; i < len(me.Moves) && i < 4; i++ {
		slot := me.Moves[i]
		if slot.MoveID == "" {
			continue
		}
		mv := m.dex.Moves[slot.MoveID]
		label := fmt.Sprintf("[%d] %-13s %-3s %d/%dpp", i+1, mv.Name, mv.Type, slot.PP, slot.MaxPP)
		if _, ok := findAction(acts, engine.ActionMove, i); ok {
			label = stKey.Render(fmt.Sprintf("[%d]", i+1)) + label[3:]
		} else {
			label = stDim.Render(label)
		}
		cells = append(cells, label)
	}
	// Two per row.
	for i := 0; i < len(cells); i += 2 {
		b.WriteString("  " + cells[i])
		if i+1 < len(cells) {
			b.WriteString("   " + cells[i+1])
		}
		b.WriteString("\n")
	}
	if _, ok := findAction(acts, engine.ActionMove, -1); ok {
		b.WriteString("  " + stKey.Render("[s]") + " Struggle (no moves with PP)\n")
	}
	b.WriteString("  " + m.switchLine(v, acts))
	return b.String()
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
		parts = append(parts, fmt.Sprintf("%s %s", stKey.Render("["+letter+"]"), p.Name))
	}
	if len(parts) == 0 {
		return stDim.Render("switch: (none available)")
	}
	return "switch: " + strings.Join(parts, "  ")
}

// ---- end ----

func (m model) viewEnded() string {
	var b strings.Builder
	b.WriteString(stTitle.Render("PokéArena · battle "+short(m.battleID)) + "\n\n")
	switch {
	case m.disconnErr != nil && m.winner == nil:
		b.WriteString("  " + stWarn.Render("⚠ "+m.status) + "\n")
	case m.winner == nil:
		b.WriteString("  " + stStatus.Render("battle ended — no winner (abandoned)") + "\n")
	case *m.winner == m.meSide:
		b.WriteString("  " + stWin.Render("🏆 you won!") + "\n")
	default:
		b.WriteString("  " + stLose.Render("💀 you lost.") + "\n")
	}
	if m.view != nil {
		b.WriteString("\n" + m.viewLog() + "\n")
	}
	b.WriteString("\n  " + stDim.Render("press q to quit") + "\n")
	return b.String()
}

// ---- small helpers ----

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func kv(key, desc string) string { return stKey.Render(key) + " " + stDim.Render(desc) }

func controls(parts ...string) string { return strings.Join(parts, stDim.Render("  ·  ")) }
