// Package cards is what the assistant knows about a card beyond its Arena id,
// as plain data, and how those details read at a glance. It is shared by the
// source that supplies them (Scryfall), the store that keeps them, and the
// model that shows them, so none of those has to depend on another.
package cards

import (
	"fmt"
	"strings"
)

// Card is one card, keyed by the id Arena's log names it by (decision 15).
// The text fields are as the source gave them: ManaCost in Scryfall's
// notation ("{2}{G}{G}"), Rarity in its lower-case words ("mythic").
type Card struct {
	ArenaID  int
	Set      string // the set code as Arena writes it: "FRA"
	Name     string
	ManaCost string
	TypeLine string
	Rarity   string
	// ImageURL is where the card's picture is, at the size the window
	// shows (decision 12); empty when the source has none for it.
	ImageURL string
}

// Detail is the line under a card's name: its mana cost, type, and rarity,
// "2GG · Creature — Elf Druid · Uncommon", leaving out what it does not
// have (a land has no mana cost).
func (c Card) Detail() string {
	var parts []string
	for _, p := range []string{Mana(c.ManaCost), c.TypeLine, rarityName(c.Rarity)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// Mana is a mana cost in the compact form players write it in: "{2}{G}{G}"
// is "2GG". A symbol of more than one character (hybrid "{G/U}", Phyrexian
// "{G/P}", "{10}") keeps its own brackets so the cost still reads
// unambiguously: "(G/U)(G/U)". The faces of a card with two costs are
// separated as Scryfall separates them: "1G // 3U".
func Mana(cost string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(cost, '{')
		if open < 0 {
			b.WriteString(cost)
			break
		}
		closing := strings.IndexByte(cost[open:], '}')
		if closing < 0 {
			// Not Scryfall's notation after all; better shown as it came
			// than with a symbol cut in half.
			b.WriteString(cost)
			break
		}
		b.WriteString(cost[:open])
		sym := cost[open+1 : open+closing]
		if len(sym) == 1 {
			b.WriteString(sym)
		} else {
			b.WriteString("(" + sym + ")")
		}
		cost = cost[open+closing+1:]
	}
	return b.String()
}

// rarityNames are Scryfall's rarities as Arena's collection filter names them.
var rarityNames = map[string]string{
	"common":   "Common",
	"uncommon": "Uncommon",
	"rare":     "Rare",
	"mythic":   "Mythic",
	"special":  "Special",
	"bonus":    "Bonus",
}

// rarityName is a rarity as a word with a capital; one Scryfall adds later is
// shown as it came, rather than dropped.
func rarityName(r string) string {
	if name, ok := rarityNames[r]; ok {
		return name
	}
	return r
}

// Unnamed is how a card the assistant has no details for is shown: by the id
// Arena's log names it by, said to be one.
func Unnamed(arenaID int) string {
	return fmt.Sprintf("Card %d", arenaID)
}
