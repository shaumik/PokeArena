package engine

import "testing"

// TestInertReasonsAnswerForRealSlugs: the audit `royale validate` runs on a
// roster. Legality and soundness are different questions, and this is the one
// nothing asked before a tournament team built its whole strategy on Harvest
// and discovered mid-match that the slug did nothing.
func TestInertReasonsAnswerForRealSlugs(t *testing.T) {
	cases := []struct {
		slug  string
		inert bool
		why   string
	}{
		// Hooked, and obviously so.
		{"drought", false, "sets weather on entry"},
		{"harvest", false, "an end-of-turn hook since the follow-up work"},
		{"own-tempo", false, "BlocksConfusion, since the same work"},
		// Hookless but implemented by a layer that reads the slug.
		{string(AbilityGluttony), false, "pinchThresholdFor"},
		{"sticky-hold", false, "itemIsRemovable"},
		{"arena-trap", false, "the switch-blocking check"},
		// Genuinely inert.
		{"neutralizing-gas", true, "no suppression code anywhere"},
		{"unnerve", true, "no berry suppression"},
		{"forewarn", true, "no dex in the switch-in hook"},
		{"illuminate", true, "wild-encounter rates only"},
		// Not an ability at all.
		{"not-an-ability", true, "unregistered: every lookup passes it by"},
		// The empty slot is not a complaint.
		{"", false, "no ability declared"},
	}
	for _, c := range cases {
		got := AbilityInertReason(c.slug)
		if (got != "") != c.inert {
			t.Errorf("AbilityInertReason(%q) = %q, want inert=%v (%s)",
				c.slug, got, c.inert, c.why)
		}
	}
}

// TestItemInertReasonTracksTheRegistry: an item the catalog lists and the
// engine does not model leaves its holder playing bare, which a roster
// depending on it deserves to be told.
func TestItemInertReasonTracksTheRegistry(t *testing.T) {
	if got := ItemInertReason(string(ItemSitrusBerry)); got != "" {
		t.Errorf("a modeled item was called inert: %q", got)
	}
	if got := ItemInertReason(""); got != "" {
		t.Errorf("holding nothing is not an inert item: %q", got)
	}
	if got := ItemInertReason("not-an-item"); got == "" {
		t.Error("an unmodeled item slug must report as inert")
	}
	// Whatever the catalog currently lists unmodeled must agree with the audit
	// AuditItems already publishes, or the two disagree about the same fact.
	for _, gap := range AuditItems(loadDex(t)) {
		if ItemInertReason(gap.ID) == "" {
			t.Errorf("%s is an AuditItems gap but ItemInertReason calls it modeled", gap.ID)
		}
	}
}
