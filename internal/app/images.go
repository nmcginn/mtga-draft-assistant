package app

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"time"
)

// CardImage is what the window knows of one card's picture.
type CardImage struct {
	// Path is the picture on disk, once it is there: a local file, never
	// an address (decision 12).
	Path string
	// Err is why the last download failed, while there is no Path, and
	// RetryIn how soon it is tried again.
	Err     string
	RetryIn time.Duration
}

// ImageWant is a card on screen whose picture the window can show: the
// image task keeps these on disk, fetching the ones it does not have.
type ImageWant struct {
	ArenaID int
	URL     string
}

// ImageReady reports that ArenaID's picture is on disk at Path.
type ImageReady struct {
	ArenaID int
	Path    string
}

// ImageFailed reports that ArenaID's picture could not be had, and that
// the image task tries again after RetryIn.
type ImageFailed struct {
	ArenaID int
	Err     error
	RetryIn time.Duration
}

// ImagesEvicted reports that the pictures of ArenaIDs were deleted to keep
// the image folder under its cap. None of them was on screen; a later pack
// that shows one asks for it again.
type ImagesEvicted struct{ ArenaIDs []int }

// ImageCacheUnavailable reports that pictures cannot be kept in Dir, so
// cards show by name. The image task keeps trying.
type ImageCacheUnavailable struct {
	Dir string
	Err error
}

func (ImageReady) isMsg()            {}
func (ImageFailed) isMsg()           {}
func (ImagesEvicted) isMsg()         {}
func (ImageCacheUnavailable) isMsg() {}

// applyImages is Apply for the image task's messages. Like the cards, the
// pictures on disk do not depend on which log is followed, so they are
// kept when it is lost or starts over.
func (m Model) applyImages(msg Msg) Model {
	switch msg := msg.(type) {
	case ImageReady:
		if msg.Path == "" {
			return m
		}
		m.Images = withImage(m.Images, msg.ArenaID, CardImage{Path: msg.Path})
		// A picture saved means the folder works again.
		m.ImageProblem = ""
	case ImageFailed:
		img := CardImage{Err: "no reason given", RetryIn: msg.RetryIn}
		if msg.Err != nil {
			img.Err = msg.Err.Error()
		}
		m.Images = withImage(m.Images, msg.ArenaID, img)
	case ImagesEvicted:
		if len(msg.ArenaIDs) == 0 {
			return m
		}
		next := maps.Clone(m.Images)
		for _, id := range msg.ArenaIDs {
			delete(next, id)
		}
		m.Images = next
	case ImageCacheUnavailable:
		m.ImageDir = msg.Dir
		m.ImageProblem = "no reason given"
		var pe *fs.PathError
		switch {
		case errors.As(msg.Err, &pe) && pe.Err != nil:
			m.ImageProblem = pe.Err.Error()
		case msg.Err != nil:
			m.ImageProblem = msg.Err.Error()
		}
	}
	return m
}

// withImage is images with id's entry replaced by img, as a new map, so a
// Model held elsewhere keeps the map it had.
func withImage(images map[int]CardImage, id int, img CardImage) map[int]CardImage {
	next := maps.Clone(images)
	if next == nil {
		next = map[int]CardImage{}
	}
	next[id] = img
	return next
}

// ImagesShown is the cards on screen that have a picture to show, in the
// order they are shown: the pack, while it is waiting for a pick. Only
// these are fetched (decision 12), and while they are shown none of them
// is deleted to make room. Picks and the pool are listed by name, so they
// need none.
func (m Model) ImagesShown() []ImageWant {
	if !m.ShowsDraft() {
		return nil
	}
	d := m.Draft
	if d.Done || d.Cards == nil {
		return nil
	}
	if _, picked := d.Picked(); picked {
		return nil
	}
	var out []ImageWant
	for _, id := range d.Cards {
		if c, ok := m.card(d.Set, id); ok && c.ImageURL != "" {
			if !slices.ContainsFunc(out, func(w ImageWant) bool { return w.ArenaID == id }) {
				out = append(out, ImageWant{ArenaID: id, URL: c.ImageURL})
			}
		}
	}
	return out
}

// imagesNotice says why pictures are missing from the pack, when a
// download failed for one on screen: what went wrong, that the cards are
// named meanwhile, and when it is tried again. Empty otherwise: a picture
// still on its way is a placeholder with the card's name, which needs no
// explaining.
func (m Model) imagesNotice(shown []ImageWant) string {
	for _, w := range shown {
		img := m.Images[w.ArenaID]
		if img.Path != "" || img.Err == "" {
			continue
		}
		return fmt.Sprintf("Couldn't download card images from Scryfall (%s). Check your internet connection; "+
			"cards show by name until then, and the assistant tries again %s.", img.Err, within(img.RetryIn))
	}
	return ""
}
