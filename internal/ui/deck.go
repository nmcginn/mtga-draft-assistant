package ui

import (
	"fmt"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/deck"
)

// deckView is the deck being built from a finished draft's pool (decision
// 31): the colours to build in, how the deck was made, its size and the
// button that copies it for Arena, its counts and curve, the deck by kind
// with its basic lands, and the rest of the pool as the sideboard. Every
// card is a button that moves one copy between the deck and the
// sideboard; every change is a message to the model, posted like any
// other (decision 6), so the view stays a picture of the state.
func deckView(d *app.DeckScreen, t Theme) gui.View {
	content := []gui.View{text("deck-heading", d.Heading, t.label())}

	colours := make([]gui.View, len(d.Colours))
	for i, c := range d.Colours {
		rest := t.Control
		if c.Chosen {
			rest = t.Chosen
		}
		msg := app.DeckColourToggled{Colour: c.Colour}
		colours[i] = button(buttonCfg{
			id: "deck-colour-" + c.Letter, label: c.Letter, name: c.Name, chosen: c.Chosen,
			style: t.body(), rest: rest, hover: t.Hover, width: curveColumn,
		}, func(w *gui.Window) { Post(w, msg) })
	}
	content = append(content, gui.Row(gui.ContainerCfg{
		ID:      "deck-colours",
		Sizing:  gui.FitFit,
		Spacing: gui.SpacingSmall,
		Padding: gui.NewPadding(0, 0, 0, 0),
		Content: colours,
	}), text("deck-note", d.Note, t.note()))
	if d.Size != "" {
		content = append(content, text("deck-size", d.Size, t.body()))
	}

	export := d.Export
	content = append(content, button(buttonCfg{
		id: "deck-copy", label: "Copy for Arena", name: "Copy the deck for Arena's import",
		style: t.body(), rest: t.Control, hover: t.Hover,
	}, func(w *gui.Window) {
		// The clipboard belongs to the window system, which go-gui
		// reaches on this thread; the copy is a few hundred bytes.
		w.SetClipboard(export)
		Post(w, app.DeckCopied{})
	}))
	if d.Copied != "" {
		content = append(content, text("deck-copied", d.Copied, t.note()))
	}
	content = append(content, summaryViews("deck", d.Summary, t)...)

	for i, g := range d.Groups {
		id := fmt.Sprintf("deck-group-%d", i)
		content = append(content, text(id+"-heading", g.Heading, t.label()))
		content = append(content, cardButtons(id, g.Cards, false, t)...)
		if i == len(d.Groups)-1 {
			for _, b := range d.Basics {
				content = append(content, basicRow(b, t))
			}
		}
	}

	content = append(content, text("deck-side-heading", d.SideboardHeading, t.label()))
	content = append(content, cardButtons("deck-side", d.Sideboard, true, t)...)
	if d.Unknown != "" {
		content = append(content, text("deck-unknown", d.Unknown, t.note()))
	}
	return section("deck", content)
}

// cardButtons is a line for each card, which moves one copy of it into the
// deck (toDeck) or out of it when clicked.
func cardButtons(id string, lines []app.DeckLine, toDeck bool, t Theme) []gui.View {
	out := make([]gui.View, len(lines))
	name := "Move one to the sideboard"
	if toDeck {
		name = "Move one into the deck"
	}
	for i, l := range lines {
		msg := app.DeckCardMoved{Card: l.Card, ToDeck: toDeck}
		out[i] = button(buttonCfg{
			id: fmt.Sprintf("%s-%d", id, i), label: l.Text, description: name,
			style: t.body(), rest: t.Background, hover: t.Hover, fill: true,
		}, func(w *gui.Window) { Post(w, msg) })
	}
	return out
}

// basicRow is one basic land's count, with a button to take one out and
// one to add one.
func basicRow(b app.DeckBasic, t Theme) gui.View {
	id := "deck-basic-" + b.Colour.Letter()
	land := deck.Basic(b.Colour).Name
	less, more := app.DeckBasicChanged{Colour: b.Colour, By: -1}, app.DeckBasicChanged{Colour: b.Colour, By: 1}
	return gui.Row(gui.ContainerCfg{
		ID:      id,
		Sizing:  gui.FillFit,
		VAlign:  gui.VAlignMiddle,
		Spacing: gui.SpacingSmall,
		Padding: gui.NewPadding(0, 0, 0, 0),
		Content: []gui.View{
			button(buttonCfg{
				id: id + "-less", label: "−", name: "One fewer " + land,
				style: t.body(), rest: t.Control, hover: t.Hover, width: curveColumn,
			}, func(w *gui.Window) { Post(w, less) }),
			button(buttonCfg{
				id: id + "-more", label: "+", name: "One more " + land,
				style: t.body(), rest: t.Control, hover: t.Hover, width: curveColumn,
			}, func(w *gui.Window) { Post(w, more) }),
			text(id+"-count", b.Text, t.body()),
		},
	})
}

// buttonCfg is what differs between the deck section's buttons.
type buttonCfg struct {
	id, label         string
	name, description string // for a screen reader; name defaults to label
	chosen            bool
	style             gui.TextStyle
	rest, hover       gui.Color
	// width fixes the button's width; fill stretches it across its
	// column; neither fits it to its label.
	width float32
	fill  bool
}

// button is a button painted from the theme alone: rest at rest, hover
// under the pointer, pressed or focused, never a colour of go-gui's own
// theme (decision 7).
func button(cfg buttonCfg, onClick func(*gui.Window)) gui.View {
	sizing := gui.FitFit
	switch {
	case cfg.width > 0:
		sizing = gui.FixedFit
	case cfg.fill:
		sizing = gui.FillFit
	}
	name := cfg.name
	if name == "" {
		name = cfg.label
	}
	// A line of the deck reads from the left and sits close to the next,
	// as the pool's lines do; a short label sits in the middle of its
	// button.
	align, padding := gui.HAlignCenter, gui.NewPadding(4, 8, 4, 8)
	if cfg.fill {
		align, padding = gui.HAlignLeft, gui.NewPadding(2, 8, 2, 8)
	}
	a11yState := gui.AccessState(0)
	if cfg.chosen {
		a11yState = gui.AccessStateSelected
	}
	return gui.Button(gui.ButtonCfg{
		ID:        cfg.id,
		Sizing:    sizing,
		Width:     cfg.width,
		HAlign:    gui.Some(align),
		Padding:   padding,
		Radius:    gui.RadiusSmall,
		A11YCfg:   gui.A11YCfg{A11YLabel: name, A11YDescription: cfg.description},
		A11YState: a11yState,
		Colors: gui.ColorSet{
			Base: cfg.rest, Hover: cfg.hover, Click: cfg.hover, Focus: cfg.hover,
			Border: cfg.rest, BorderFocus: cfg.hover,
		},
		Content: []gui.View{gui.Text(gui.TextCfg{ID: cfg.id + "-label", Text: cfg.label, TextStyle: cfg.style, Mode: gui.TextModeWrap})},
		OnClick: func(ctx gui.EventCtx) { onClick(ctx.Window) },
	})
}
