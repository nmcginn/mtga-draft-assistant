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
		return draftView(s.model.DraftScreen(), s.theme)
	}
	return statusView(s.model.Status(), s.theme)
}

// draftView is the draft: its name and position, the pack one card a line,
// and the picks so far under it. It scrolls, since a full pack and three
// packs of picks are taller than the window.
func draftView(d app.DraftScreen, t Theme) gui.View {
	text := func(id, s string, style gui.TextStyle) gui.View {
		return gui.Text(gui.TextCfg{ID: id, Text: s, TextStyle: style, Mode: gui.TextModeWrap})
	}
	content := []gui.View{text("draft-title", d.Title, t.title())}
	if d.Position != "" {
		content = append(content, text("draft-position", d.Position, t.label()))
	}

	pack := make([]gui.View, 0, len(d.Cards)+1)
	pack = append(pack, text("draft-heading", d.Heading, t.label()))
	for i, c := range d.Cards {
		pack = append(pack, text(fmt.Sprintf("draft-card-%d", i), c, t.body()))
	}
	content = append(content, section("draft-pack", pack))

	if d.PicksHeading != "" {
		picks := make([]gui.View, 0, len(d.Picks)+1)
		picks = append(picks, text("draft-picks-heading", d.PicksHeading, t.label()))
		for i, p := range d.Picks {
			picks = append(picks, text(fmt.Sprintf("draft-picks-%d", i), p, t.note()))
		}
		content = append(content, section("draft-picks", picks))
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
		w.InvalidateLayout()
	})
}
