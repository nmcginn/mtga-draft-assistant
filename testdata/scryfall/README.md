# Scryfall responses

**These are not recordings.** The session that wrote the Scryfall client could
not reach `api.scryfall.com`, so these pages follow the shapes Scryfall
documents for a [list](https://scryfall.com/docs/api/lists), a
[card](https://scryfall.com/docs/api/cards), and an
[error](https://scryfall.com/docs/api/errors), trimmed to the fields the client
reads, with invented names. The Arena ids are real: they are the first pack of
`../logs/2026-10-fra-premier-full-draft.log`.

What the tests lean on:

- `fra-search-page1.json`, `fra-search-page2.json`: `e:fra game:arena`,
  `unique=prints`, in two pages joined by `next_page`. Page 1 holds a print
  with no `arena_id` (paper only); page 2 a transforming card whose mana cost
  is on its faces only. Arena id 106439, in the fixture's first pack, is in
  neither: it stands for a card Scryfall does not know yet.
- `search-not-found.json`: what Scryfall answers, with a 404, to a search
  that matches nothing, which is what a set it has never heard of gets.

The tests serve them from an `httptest` server, with `https://api.scryfall.com`
in `next_page` replaced by the test server's address.

## Replacing them with recordings

From a machine that can reach Scryfall:

```sh
curl -sS -H 'User-Agent: mtga-draft-assistant/dev (+https://github.com/nmcginn/mtga-draft-assistant)' \
  -H 'Accept: application/json' \
  'https://api.scryfall.com/cards/search?q=e%3Afra+game%3Aarena&unique=prints'
```

then trim `data` to the cards the tests name, keeping every field of each card
the client reads (`arena_id`, `name`, `mana_cost`, `type_line`, `rarity`,
`card_faces`) and the list's own fields, and note the date captured here.
