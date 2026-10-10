package ui

import "github.com/go-gui-org/go-gui/gui"

// Theme is the entire palette and type scale. Every colour a view paints comes
// from here, never from a literal in a widget, so a palette change is a change
// to one file and a contrast check has one place to look (decision 7).
type Theme struct {
	Background gui.Color // the window behind everything
	Text       gui.Color // primary text: card names, headlines
	TextMuted  gui.Color // secondary text: hints, counts, sources

	HeadlineSize float32
	BodySize     float32
}

// Dark is the default theme. Arena is dark, and a companion window that
// flashes white beside it is the first thing a user would turn off.
var Dark = Theme{
	Background: gui.Hex(0x15171c),
	Text:       gui.Hex(0xe8e6e3),
	TextMuted:  gui.Hex(0x9a9ea6),

	HeadlineSize: 22,
	BodySize:     14,
}

// headline is the text style for the one most important line in a view.
func (t Theme) headline() gui.TextStyle {
	return gui.TextStyle{Color: t.Text, Size: t.HeadlineSize}
}

// muted is the text style for supporting detail, centred under the headline
// it supports.
func (t Theme) muted() gui.TextStyle {
	return gui.TextStyle{Color: t.TextMuted, Size: t.BodySize, Align: gui.TextAlignCenter}
}
