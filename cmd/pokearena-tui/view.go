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

	header := stTitle.Render(fmt.Sprintf("PokéArena · battle %s · turn %d · %s",
		short(m.battleID), v.Turn, battlePhase(v)))

	// Gen-1 diagonal arena: foe stat box (top-left) faces its sprite (top-
	// right); your sprite (bottom-left) faces your stat box (bottom-right).
	front, back := m.spriteSizes()
	top := lipgloss.JoinHorizontal(lipgloss.Top, m.foeStatBox(v), "  ", m.foeSpriteBlock(v, front))
	bottom := lipgloss.JoinHorizontal(lipgloss.Bottom, m.selfSpriteBlock(v, back), "  ", m.selfStatBox(v))
	arena := lipgloss.JoinVertical(lipgloss.Left, top, m.fieldLine(v), bottom)

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

func battlePhase(v *battleView) string {
	if v.Replace {
		return "replace"
	}
	return string(v.Phase)
}

// foeStatBox renders the opponent's name plate + HP (percentage only — the wire
// redacts the exact count) + bench count + revealed moves, in an LCD panel.
func (m model) foeStatBox(v *battleView) string {
	head := fmt.Sprintf("%s  %s%s%s",
		stOpp.Render(v.Foe.Name), stDim.Render(typeLabel(v.Foe.Type1, v.Foe.Type2)),
		statusTag(v.Foe.Status), boostTag(v.Foe.Stages))
	bar := fmt.Sprintf("HP %s ~%d%%", hpBar(float64(v.Foe.HPPct)/100, 14), v.Foe.HPPct)
	parts := []string{head, bar, stDim.Render("bench " + benchCount(v.FoeBenchAlive))}
	if moves := revealedMoves(m.dex, v.Foe.Moves); moves != "" {
		parts = append(parts, stDim.Render(moves))
	}
	return stPanel.Render(strings.Join(parts, "\n"))
}

// selfStatBox renders your active's name plate + exact HP + bench, in a panel.
func (m model) selfStatBox(v *battleView) string {
	me := v.Self.Team[v.Self.Active]
	head := fmt.Sprintf("%s  %s%s%s",
		stYou.Render(me.Name), stDim.Render(typeLabel(me.Type1, me.Type2)),
		statusTag(me.Status), boostTag(me.Stages))
	frac := 0.0
	if me.MaxHP > 0 {
		frac = float64(me.HP) / float64(me.MaxHP)
	}
	bar := fmt.Sprintf("HP %s %d/%d", hpBar(frac, 14), me.HP, me.MaxHP)
	meta := stDim.Render("bench " + benchDots(v.Self.Team, v.Self.Active))
	return stPanel.Render(strings.Join([]string{head, bar, meta}, "\n"))
}

func (m model) foeSpriteBlock(v *battleView, px int) string {
	var sp *sprite
	if dexNo, ok := dexNoByName(m.dex, v.Foe.Name); ok {
		sp = foeSprite(dexNo, px)
	}
	return spriteBlock(sp, px, px/2, m.spriteFrame)
}

func (m model) selfSpriteBlock(v *battleView, px int) string {
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
	lines := m.log
	if len(lines) > logTail {
		lines = lines[len(lines)-logTail:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(fmt.Sprintf("%s  %s\n", logSideTag(l.Side, m.meSide), l.Text))
	}
	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		content = stDim.Render("(the battle log will appear here)")
	}
	return stDim.Render("  log") + "\n" + stDialog.Width(58).Render(content)
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
