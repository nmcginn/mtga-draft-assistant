package scryfall

import (
	"context"

	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
)

// maxImageBytes caps one image. A normal-sized card is 488×680, about 70 KB
// as JPEG; anything near this is not a card picture.
const maxImageBytes = 4 << 20

// Images downloads card pictures through the app's client. It only fetches:
// keeping them, and deciding which to ask for, is the image task's
// (decision 12).
type Images struct {
	Client *fetch.Client
}

// Image returns the bytes at u, a card's ImageURL. The URL was checked to
// be on ImageHost when the card was read, so it is asked as given.
func (i Images) Image(ctx context.Context, u string) ([]byte, error) {
	resp, err := i.Client.Get(ctx, fetch.Request{URL: u, Accept: "image/jpeg,image/png;q=0.9", MaxBytes: maxImageBytes})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
