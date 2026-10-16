package ui

import "github.com/go-gui-org/go-gui/gui"

// Theme is the entire palette and type scale. Every colour a view paints comes
// from here, never from a literal in a widget, so a palette change is a change
// to one file and a contrast check has one place to look (decision 7).
type Theme struct {
	Background gui.Color // the window behind everything
	Text       gui.Color // primary text: card names, headlines
	TextMuted  gui.Color // secondary text: hints, counts, sources
	Subtle     gui.Color // quiet structure: the scrollbar thumb

	HeadlineSize float32
	BodySize     float32
	SmallSize    float32
}

// Dark is the default theme. Arena is dark, and a companion window that
// flashes white beside it is the first thing a user would turn off.
var Dark = Theme{
	Background: gui.Hex(0x15171c),
	Text:       gui.Hex(0xe8e6e3),
	TextMuted:  gui.Hex(0x9a9ea6),
	Subtle:     gui.Hex(0x3a3e47),

	HeadlineSize: 22,
	BodySize:     14,
	SmallSize:    12,
}

// headline is the text style for the one most important line in a view.
func (t Theme) headline() gui.TextStyle {
	return gui.TextStyle{Color: t.Text, Size: t.HeadlineSize, Align: gui.TextAlignCenter}
}

// muted is the text style for supporting detail, centred under the headline
// it supports.
func (t Theme) muted() gui.TextStyle {
	return gui.TextStyle{Color: t.TextMuted, Size: t.BodySize, Align: gui.TextAlignCenter}
}

// small is the text style for reference detail a user reads only if they
// need it: where the assistant looked, which file it is reading.
func (t Theme) small() gui.TextStyle {
	return gui.TextStyle{Color: t.TextMuted, Size: t.SmallSize, Align: gui.TextAlignCenter}
}

// The draft view reads top to bottom like a list, so its text is aligned to
// the left edge, where the eye returns for each card.

// title is the draft's name at the top of the draft view.
func (t Theme) title() gui.TextStyle {
	return gui.TextStyle{Color: t.Text, Size: t.HeadlineSize}
}

// body is the text style for what the user picks from: the cards.
func (t Theme) body() gui.TextStyle {
	return gui.TextStyle{Color: t.Text, Size: t.BodySize}
}

// label is the text style for a line that introduces what is under it.
func (t Theme) label() gui.TextStyle {
	return gui.TextStyle{Color: t.TextMuted, Size: t.BodySize}
}

// note is the text style for reference detail in the draft view.
func (t Theme) note() gui.TextStyle {
	return gui.TextStyle{Color: t.TextMuted, Size: t.SmallSize}
}
