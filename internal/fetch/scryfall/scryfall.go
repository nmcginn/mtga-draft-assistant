// Package scryfall fetches a set's cards from Scryfall, which maps the ids
// Arena's log uses to names and details (decision 15). It asks for one set at
// a time, through its paged search, and never for the bulk file, which holds
// every card ever printed (docs/ARCHITECTURE.md's network budget).
package scryfall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
)

// BaseURL is Scryfall's API.
const BaseURL = "https://api.scryfall.com"

// Host and Interval are the rate limit the app's client keeps for Scryfall.
// Scryfall asks for 50–100 ms between requests; this is five times that
// (decision 25), because a set is only a few pages, so the wait costs a
// second or two, once per set, and keeps clear of any tighter limit on
// search in particular.
const (
	Host     = "api.scryfall.com"
	Interval = 500 * time.Millisecond
)

// accept is the Accept header Scryfall asks every client to send.
const accept = "application/json;q=0.9,*/*;q=0.8"

// maxPages bounds one set's search. A set is a few hundred prints, two or
// three pages of 175; more than this is not a set, and is not followed
// further.
const maxPages = 20

// Source fetches from Scryfall through the app's client.
type Source struct {
	Client *fetch.Client
	// BaseURL is Scryfall's address; empty is BaseURL. Tests point it at
	// an httptest server.
	BaseURL string
}

// setCode is what a set code from Arena's log may be before it is put into a
// query: letters and digits, so nothing in the log can change what is asked.
var setCode = regexp.MustCompile(`^[A-Za-z0-9]{2,8}$`)

// Set returns every card in set that Scryfall knows an Arena id for. A set
// Scryfall has no cards for at all is not an error: the result is empty, and
// the draft shows its cards by id. An error says, in words for the user,
// what went wrong: "can't reach api.scryfall.com".
func (s Source) Set(ctx context.Context, set string) ([]cards.Card, error) {
	if !setCode.MatchString(set) {
		return nil, fmt.Errorf("%q is not a set code", set)
	}
	base := s.BaseURL
	if base == "" {
		base = BaseURL
	}
	q := url.Values{
		"q": {"e:" + strings.ToLower(set) + " game:arena"},
		// Every print, not one per card name: showcase and borderless
		// prints have Arena ids of their own, and turn up in packs.
		"unique": {"prints"},
	}
	next := base + "/cards/search?" + q.Encode()
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("scryfall address %q: %w", base, err)
	}

	var out []cards.Card
	for page := 1; next != ""; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("scryfall's search for %s ran past %d pages", set, maxPages)
		}
		resp, err := s.Client.Get(ctx, fetch.Request{URL: next, Accept: accept})
		if err != nil {
			if page == 1 && notFound(err) {
				return nil, nil
			}
			return nil, err
		}
		var l list
		if err := json.Unmarshal(resp.Body, &l); err != nil || l.Object != "list" {
			return nil, errors.New("scryfall sent a page the assistant can't read")
		}
		for _, c := range l.Data {
			if card, ok := c.card(set); ok {
				out = append(out, card)
			}
		}
		next = ""
		if l.HasMore {
			// The next page is followed only where the search began: a
			// page is not trusted to send the client to another host.
			u, err := url.Parse(l.NextPage)
			if err != nil || u.Scheme != baseURL.Scheme || u.Host != baseURL.Host {
				return nil, errors.New("scryfall's next page is somewhere else")
			}
			next = l.NextPage
		}
	}
	return out, nil
}

// notFound reports whether err is Scryfall's answer to a search that
// matched nothing: a 404 with a not_found error object.
func notFound(err error) bool {
	var se *fetch.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		return false
	}
	var e struct {
		Object string `json:"object"`
		Code   string `json:"code"`
	}
	return json.Unmarshal(se.Body, &e) == nil && e.Object == "error" && e.Code == "not_found"
}

// list is a page of Scryfall's search: the fields read, of its List object.
type list struct {
	Object   string `json:"object"`
	HasMore  bool   `json:"has_more"`
	NextPage string `json:"next_page"`
	Data     []card `json:"data"`
}

// card is the fields read of Scryfall's Card object.
type card struct {
	ArenaID  int    `json:"arena_id"`
	Name     string `json:"name"`
	ManaCost string `json:"mana_cost"`
	TypeLine string `json:"type_line"`
	Rarity   string `json:"rarity"`
	Images   images `json:"image_uris"`
	Faces    []struct {
		ManaCost string `json:"mana_cost"`
		TypeLine string `json:"type_line"`
		Images   images `json:"image_uris"`
	} `json:"card_faces"`
}

// images is the field read of a card's or a face's image_uris: the size the
// window shows (decision 12).
type images struct {
	Normal string `json:"normal"`
}

// card is c as the assistant keeps it, and false for a print Arena does
// not have (no arena_id), which no log will name.
func (c card) card(set string) (cards.Card, bool) {
	if c.ArenaID <= 0 || c.Name == "" {
		return cards.Card{}, false
	}
	out := cards.Card{
		ArenaID:  c.ArenaID,
		Set:      strings.ToUpper(set),
		Name:     c.Name,
		ManaCost: c.ManaCost,
		TypeLine: c.TypeLine,
		Rarity:   c.Rarity,
		ImageURL: imageURL(c.Images.Normal),
	}
	// A card with two faces printed on two sides has a picture of each
	// and none of its own; its front is what Arena shows in a pack.
	if out.ImageURL == "" && len(c.Faces) > 0 {
		out.ImageURL = imageURL(c.Faces[0].Images.Normal)
	}
	// A card with two faces may carry its cost and type only on them:
	// they are joined as Scryfall joins the names, "1W // 3W".
	if out.ManaCost == "" {
		out.ManaCost = faces(c, func(f int) string { return c.Faces[f].ManaCost })
	}
	if out.TypeLine == "" {
		out.TypeLine = faces(c, func(f int) string { return c.Faces[f].TypeLine })
	}
	return out, true
}

// ImageHost is where Scryfall keeps its card images. Its files are not
// rate limited by Scryfall; ImageInterval is ours, so a pack's images
// arrive within a couple of seconds without bursting at the host.
const (
	ImageHost     = "cards.scryfall.io"
	ImageInterval = 100 * time.Millisecond
)

// imageURL is u when it is a picture on Scryfall's image host, and "" when
// it is anything else. A page could name any address, and an image is
// fetched without the user asking, so only the host the budget lists
// (docs/ARCHITECTURE.md) is ever asked.
func imageURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" || parsed.Host != ImageHost {
		return ""
	}
	return u
}

// faces is field of each of c's faces that has one, joined with " // ".
func faces(c card, field func(int) string) string {
	var parts []string
	for i := range c.Faces {
		if v := field(i); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " // ")
}
