package cards

import "testing"

func TestManaCostsReadTheWayPlayersWriteThem(t *testing.T) {
	for cost, want := range map[string]string{
		"{2}{G}{G}":          "2GG",
		"{X}{R}":             "XR",
		"{10}":               "(10)",
		"{G/U}{G/U}":         "(G/U)(G/U)",
		"{2/W}":              "(2/W)",
		"{G/P}":              "(G/P)",
		"{1}{G} // {3}{U}":   "1G // 3U",
		"":                   "",
		"{0}":                "0",
		"not a cost":         "not a cost",
		"{2}{G":              "2{G", // cut off: the rest is shown as it came
		"{C}{C}{S}":          "CCS",
		"{W}{U}{B}{R}{G}{1}": "WUBRG1",
	} {
		if got := Mana(cost); got != want {
			t.Errorf("Mana(%q) = %q, want %q", cost, got, want)
		}
	}
}

func TestACardsDetailIsItsCostTypeAndRarity(t *testing.T) {
	c := Card{ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon"}
	if got, want := c.Detail(), "2GG · Creature — Elf Druid · Uncommon"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
}

func TestALandsDetailLeavesOutTheCostItDoesNotHave(t *testing.T) {
	c := Card{TypeLine: "Basic Land — Forest", Rarity: "common"}
	if got, want := c.Detail(), "Basic Land — Forest · Common"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
}

func TestARarityScryfallAddsLaterIsShownAsItCame(t *testing.T) {
	c := Card{TypeLine: "Artifact", Rarity: "legendary-ish"}
	if got, want := c.Detail(), "Artifact · legendary-ish"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
}

func TestACardWithNoDetailsHasAnEmptyDetailLine(t *testing.T) {
	if got := (Card{Name: "Mystery"}).Detail(); got != "" {
		t.Errorf("Detail() = %q, want empty", got)
	}
}

func TestACardWithNoDetailsIsNamedByItsArenaID(t *testing.T) {
	if got, want := Unnamed(106325), "Card 106325"; got != want {
		t.Errorf("Unnamed = %q, want %q", got, want)
	}
}
