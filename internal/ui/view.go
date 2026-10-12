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
	return statusView(s.model.Status(), s.theme)
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
