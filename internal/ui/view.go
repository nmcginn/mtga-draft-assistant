// Package ui turns an app.Model into go-gui views. A view reads state and
// lays it out; it decides nothing that is not about layout, and it never
// fetches anything. The one place that touches the window system is Run.
package ui

import (
	"fmt"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
)

// state is the window's single state slot.
type state struct {
	model app.Model
	theme Theme
	// showImages is told, after every message, which pictures are on
	// screen: the image task's Want in the app. Nil in most tests.
	showImages func([]app.ImageWant)
}

// newState is the state the first frame paints: no I/O has happened yet.
func newState(model app.Model) *state {
	return &state{model: model, theme: Dark}
}

// mainView builds the whole window from the current state. go-gui calls it
// again for every frame that needs one, so it must stay cheap: no allocation
// beyond the views themselves, and no I/O, ever.
func mainView(w *gui.Window) gui.View {
	s := gui.State[state](w)
	if s.model.ShowsDraft() {
		width, _ := w.WindowSize()
		return draftView(s.model.DraftScreen(), s.theme, width)
	}
	return statusView(s.model.Status(), s.theme)
}

// A card's picture is 488×680 (decision 12); a tile shows it at this size,
// two abreast in the window's default width, more as it widens, one when
// it is narrowed to its minimum.
const (
	cardWidth  = 176
	cardHeight = 245
)

// The picks sit beside the pack, at panelWidth, once the window is wide
// enough to keep two cards abreast with the panel next to them; narrower,
// they go under it. The margin is the window's padding and the gaps between
// cards and beside the panel, with room to spare, since the theme sets the
// gaps. The curve's columns are curveHeight tall at the most common mana
// value.
const (
	panelWidth  = 240
	besideWidth = 2*cardWidth + panelWidth + 128
	curveHeight = 48
	curveColumn = 28
)

// draftView is the draft: its name and position, the pack as a grid of
// cards that reflows with the window's width, and the picks so far, counted
// up and listed by pack: beside the pack when the window is wide enough
// for both, and under it when not. Each card is its picture once that is
// on disk, and until then a tile with its name, cost, type and rarity. It
// scrolls, since a full pack and three packs of picks are taller than the
// window.
func draftView(d app.DraftScreen, t Theme, width int) gui.View {
	content := []gui.View{text("draft-title", d.Title, t.title())}
	if d.Position != "" {
		content = append(content, text("draft-position", d.Position, t.label()))
	}
	if d.Notice != "" {
		content = append(content, text("draft-notice", d.Notice, t.label()))
	}

	pack := []gui.View{text("draft-heading", d.Heading, t.label())}
	if len(d.Cards) > 0 {
		tiles := make([]gui.View, len(d.Cards))
		for i, c := range d.Cards {
			tiles[i] = cardTile(fmt.Sprintf("draft-card-%d", i), c, t)
		}
		pack = append(pack, gui.Row(gui.ContainerCfg{
			ID:      "draft-grid",
			Sizing:  gui.FillFit,
			Wrap:    true,
			Spacing: gui.SpacingSmall,
			Padding: gui.NewPadding(0, 0, 0, 0),
			Content: tiles,
		}))
	}
	packView := section("draft-pack", pack)

	switch {
	case d.PicksHeading == "":
		content = append(content, packView)
	case len(d.Cards) > 0 && width >= besideWidth:
		content = append(content, gui.Row(gui.ContainerCfg{
			ID:      "draft-beside",
			Sizing:  gui.FillFit,
			Spacing: gui.SpacingLarge,
			Padding: gui.NewPadding(0, 0, 0, 0),
			Content: []gui.View{packView, picksView(d, t, true)},
		}))
	default:
		content = append(content, packView, picksView(d, t, false))
	}
	if d.Deck != nil {
		content = append(content, deckView(d.Deck, t))
	}
	if d.Footnote != "" {
		content = append(content, text("draft-footnote", d.Footnote, t.note()))
	}
	return gui.Column(gui.ContainerCfg{
		ID:            "draft",
		Sizing:        gui.FillFill,
		Color:         t.Background,
		Scrollable:    true,
		ScrollbarCfgY: &gui.ScrollbarCfg{ColorThumb: t.Subtle, ColorBackground: t.Background},
		Spacing:       gui.SpacingMedium,
		Padding:       gui.NewPadding(16, 20, 16, 20),
		Content:       content,
	})
}

// text is a line of the draft view, wrapped to the width it is given.
func text(id, s string, style gui.TextStyle) gui.View {
	return gui.Text(gui.TextCfg{ID: id, Text: s, TextStyle: style, Mode: gui.TextModeWrap})
}

// picksView is the picks so far (or the pool): their heading, the counts
// that say what the picks add up to, and the picks by pack: panelWidth
// wide beside the pack, and the window's width under it.
func picksView(d app.DraftScreen, t Theme, beside bool) gui.View {
	picks := []gui.View{text("draft-picks-heading", d.PicksHeading, t.label())}
	picks = append(picks, summaryViews("draft", d.Summary, t)...)
	if len(d.Pool) > 0 {
		picks = append(picks, poolView(d.Pool, t))
	}
	for i, p := range d.Picks {
		picks = append(picks, text(fmt.Sprintf("draft-picks-%d", i), p, t.note()))
	}
	v := section("draft-picks", picks)
	if !beside {
		return v
	}
	return gui.Column(gui.ContainerCfg{
		ID:      "draft-picks-panel",
		Sizing:  gui.FixedFit,
		Width:   panelWidth,
		Padding: gui.NewPadding(0, 0, 0, 0),
		Content: []gui.View{v},
	})
}

// A finished draft's pool is laid out in columns of poolColumn, one for each
// heap, that wrap with the window: two abreast at its default width, one at
// its narrowest, and more as it widens (decision 30).
const poolColumn = 176

// poolView is a finished draft's pool, one column for each of its heaps:
// the heap's name and count over its cards, a card to a line. Its cards
// are the cards the user builds from, so they are in the body's style, and
// the heading in the label's, as over the pack.
func poolView(groups []app.PoolGroup, t Theme) gui.View {
	cols := make([]gui.View, len(groups))
	for i, g := range groups {
		id := fmt.Sprintf("draft-pool-%d", i)
		content := make([]gui.View, 0, len(g.Cards)+1)
		content = append(content, text(id+"-heading", g.Heading, t.label()))
		for j, c := range g.Cards {
			content = append(content, text(fmt.Sprintf("%s-%d", id, j), c, t.body()))
		}
		cols[i] = gui.Column(gui.ContainerCfg{
			ID:      id,
			Sizing:  gui.FixedFit,
			Width:   poolColumn,
			Spacing: gui.SpacingTight,
			Padding: gui.NewPadding(4, 0, 4, 0),
			Content: content,
		})
	}
	return gui.Row(gui.ContainerCfg{
		ID:      "draft-pool",
		Sizing:  gui.FillFit,
		Wrap:    true,
		Spacing: gui.SpacingMedium,
		Padding: gui.NewPadding(0, 0, 0, 0),
		Content: cols,
	})
}

// summaryViews are the lines of a summary, the picks' or the deck's, under
// IDs that start with prefix: the colours, the kinds, the curve, and what
// was left out.
func summaryViews(prefix string, sum *app.PicksSummary, t Theme) []gui.View {
	if sum == nil {
		return nil
	}
	var out []gui.View
	if sum.Colours != "" {
		out = append(out, text(prefix+"-summary-colours", sum.Colours, t.body()))
	}
	if sum.Kinds != "" {
		out = append(out, text(prefix+"-summary-kinds", sum.Kinds, t.body()))
	}
	if len(sum.Curve) > 0 {
		out = append(out, curveView(prefix+"-curve", sum.Curve, t))
	}
	if sum.Unknown != "" {
		out = append(out, text(prefix+"-summary-unknown", sum.Unknown, t.note()))
	}
	return out
}

// curveView is the mana curve as columns: each with its count over it and
// its mana value under it, the tallest curveHeight high, the others in
// proportion, so the shape reads at a glance and the numbers on a second
// look. A mana value with no spells has its count and no column.
func curveView(id string, curve []app.CurveColumn, t Theme) gui.View {
	most := 0
	for _, c := range curve {
		most = max(most, c.Count)
	}
	cols := make([]gui.View, len(curve))
	for i, c := range curve {
		col := fmt.Sprintf("%s-%d", id, i)
		content := []gui.View{gui.Text(gui.TextCfg{ID: col + "-count", Text: fmt.Sprint(c.Count), TextStyle: t.note()})}
		if c.Count > 0 {
			content = append(content, gui.Column(gui.ContainerCfg{
				ID:      col + "-bar",
				Sizing:  gui.FixedFixed,
				Width:   curveColumn - 8,
				Height:  max(2, float32(curveHeight*c.Count)/float32(most)),
				Color:   t.Bar,
				Radius:  gui.RadiusSmall,
				Padding: gui.NewPadding(0, 0, 0, 0),
			}))
		}
		content = append(content, gui.Text(gui.TextCfg{ID: col + "-label", Text: c.Label, TextStyle: t.note()}))
		cols[i] = gui.Column(gui.ContainerCfg{
			ID:      col,
			Sizing:  gui.FixedFit,
			Width:   curveColumn,
			HAlign:  gui.HAlignCenter,
			VAlign:  gui.VAlignBottom,
			Spacing: gui.SpacingTight,
			Padding: gui.NewPadding(0, 0, 0, 0),
			Content: content,
		})
	}
	return gui.Row(gui.ContainerCfg{
		ID:      id,
		Sizing:  gui.FitFit,
		VAlign:  gui.VAlignBottom,
		Spacing: gui.SpacingSmall,
		Padding: gui.NewPadding(4, 0, 0, 0),
		Content: cols,
	})
}

// cardTile is one card in the pack, at a card's size: its picture, which
// carries its name for a screen reader, or a placeholder that names it.
// Both take the same space, so the grid does not jump as pictures arrive.
func cardTile(id string, c app.CardLine, t Theme) gui.View {
	if c.Image != "" {
		return gui.Column(gui.ContainerCfg{
			ID:      id + "-box",
			Sizing:  gui.FixedFixed,
			Width:   cardWidth,
			Height:  cardHeight,
			Padding: gui.NewPadding(0, 0, 0, 0),
			Content: []gui.View{gui.Image(gui.ImageCfg{
				ID:      id + "-image",
				Src:     c.Image,
				Width:   cardWidth,
				Height:  cardHeight,
				A11YCfg: gui.A11YCfg{A11YLabel: c.Name, A11YDescription: c.Detail},
			})},
		})
	}
	content := []gui.View{gui.Text(gui.TextCfg{ID: id, Text: c.Name, TextStyle: t.body(), Mode: gui.TextModeWrap})}
	if c.Detail != "" {
		content = append(content, gui.Text(gui.TextCfg{ID: id + "-detail", Text: c.Detail, TextStyle: t.note(), Mode: gui.TextModeWrap}))
	}
	return gui.Column(gui.ContainerCfg{
		ID:      id + "-box",
		Sizing:  gui.FixedFixed,
		Width:   cardWidth,
		Height:  cardHeight,
		Color:   t.Surface,
		Radius:  gui.RadiusMedium,
		Spacing: gui.SpacingSmall,
		Padding: gui.NewPadding(10, 10, 10, 10),
		Clip:    true,
		Content: content,
	})
}

// section is a group of lines in the draft view, set apart from the next.
func section(id string, content []gui.View) gui.View {
	return gui.Column(gui.ContainerCfg{
		ID:      id,
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingSmall,
		Padding: gui.NewPadding(8, 0, 0, 0),
		Content: content,
	})
}

// statusView is the centred headline-and-next-step shown whenever there is no
// draft to display, with the places searched and a footnote under it when
// there are any.
func statusView(st app.Status, t Theme) gui.View {
	content := []gui.View{
		gui.Text(gui.TextCfg{ID: "status-headline", Text: st.Headline, TextStyle: t.headline(), Mode: gui.TextModeWrap}),
		gui.Text(gui.TextCfg{ID: "status-detail", Text: st.Detail, TextStyle: t.muted(), Mode: gui.TextModeWrap}),
	}
	if len(st.Places) > 0 {
		places := make([]gui.View, 0, len(st.Places)+1)
		places = append(places, gui.Text(gui.TextCfg{ID: "status-places-label", Text: "Looked for Player.log in:", TextStyle: t.small()}))
		for i, p := range st.Places {
			places = append(places, gui.Text(gui.TextCfg{ID: fmt.Sprintf("status-place-%d", i), Text: p, TextStyle: t.small(), Mode: gui.TextModeWrap}))
		}
		content = append(content, gui.Column(gui.ContainerCfg{
			ID:      "status-places",
			Spacing: gui.SpacingSmall,
			Sizing:  gui.FillFit,
			HAlign:  gui.HAlignCenter,
			Padding: gui.NewPadding(12, 0, 0, 0),
			Content: places,
		}))
	}
	if st.Footnote != "" {
		content = append(content, gui.Text(gui.TextCfg{ID: "status-footnote", Text: st.Footnote, TextStyle: t.small(), Mode: gui.TextModeWrap}))
	}
	return gui.Column(gui.ContainerCfg{
		ID:      "status",
		Sizing:  gui.FillFill,
		Color:   t.Background,
		HAlign:  gui.HAlignCenter,
		VAlign:  gui.VAlignMiddle,
		Padding: gui.NewPadding(24, 24, 24, 24),
		Content: content,
	})
}

// Post hands msg to the window from any goroutine. The model is changed only
// on the UI thread, during the next frame, so I/O goroutines never share
// memory with a view (decision 6).
//
// go-gui does not rebuild the view because a command ran, so the command asks
// for it. It is marked within the same frame, which then rebuilds before it
// draws: the new state is on screen in the frame that applied it.
func Post(w *gui.Window, msg app.Msg) {
	w.QueueCommand(func(w *gui.Window) {
		s := gui.State[state](w)
		s.model = s.model.Apply(msg)
		// The image task learns what is on screen from the state the
		// window is about to draw; Want returns at once.
		if s.showImages != nil {
			s.showImages(s.model.ImagesShown())
		}
		w.InvalidateLayout()
	})
}
