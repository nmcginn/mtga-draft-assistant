package arena

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
)

// Format is a draft format the parser knows Arena's lines for. Each one is
// learned from a fixture under testdata/logs; a format without one is not
// guessed at (decision 23).
type Format string

const (
	// PremierDraft is drafted against people. Its packs arrive as
	// Draft.Notify lines, and picks are EventPlayerDraftMakePick requests.
	PremierDraft Format = "PremierDraft"
	// QuickDraft is drafted against bots. Packs arrive in the responses to
	// BotDraftDraftStatus and BotDraftDraftPick, and picks are the latter's
	// requests.
	QuickDraft Format = "QuickDraft"
)

// formats are the event name prefixes with a fixture behind them.
var formats = map[string]Format{
	string(PremierDraft): PremierDraft,
	string(QuickDraft):   QuickDraft,
}

// Event is something that happened in a draft, read from the log.
type Event interface{ isEvent() }

// DraftStarted reports that a draft began: the event joined, the set it
// drafts, and its format.
type DraftStarted struct {
	Event  string // Arena's name for it, such as "PremierDraft_FRA_20260929"
	Set    string // the set code, such as "FRA"
	Format Format
}

// PackShown reports the pack in front of the player. Pack and Pick count
// from 1, whatever Arena logged.
type PackShown struct {
	Pack, Pick int
	Cards      []int // Arena ids, in Arena's order
}

// PickMade reports the card the player took. Pack and Pick count from 1.
type PickMade struct {
	Pack, Pick int
	Card       int
}

// DraftEnded reports that picking is over, with every card drafted. The pool
// is in Arena's order, which is not always the order picked.
type DraftEnded struct {
	Pool []int
}

func (DraftStarted) isEvent() {}
func (PackShown) isEvent()    {}
func (PickMade) isEvent()     {}
func (DraftEnded) isEvent()   {}

// The prefixes of the lines that start a draft event. Arena writes a request
// on one line; a response is a "<== Name(id)" line followed by its body.
const (
	unityPrefix   = "[UnityCrossThreadLogger]"
	notifyPrefix  = unityPrefix + "Draft.Notify "
	requestPrefix = unityPrefix + "==> "
	replyPrefix   = "<== "
)

// Parser turns the lines of Arena's log into draft events, one line at a
// time, in the order they were written. It does no I/O: what it remembers
// between lines (a response header waiting for its body, the draft under
// way, the last pack shown) is all in the struct, so it is tested by feeding
// it lines. Start a new Parser when the log starts over.
//
// Almost every line in the log is not about drafting; those are recognised
// by their first bytes and cost nothing. A draft line that cannot be read (cut
// off mid-JSON, or a shape no fixture has shown) is reported to Debug and
// skipped: the draft goes on, and so does the parser.
type Parser struct {
	// Debug receives the lines that looked like draft events and were
	// skipped, at debug level. Nil means slog.Default(), which drops debug.
	Debug *slog.Logger

	// reply is the name of the response whose body the next line holds, or
	// "" when the next line is not a response body we read.
	reply string
	// event is the draft under way, so a resumed bot draft that never logged
	// joining it can still say which draft it is. "" when none is known.
	event string
	// unknown is a draft joined in a format with no fixture; its lines are
	// skipped rather than read as a format they might not be.
	unknown string
	// shown is the last pack shown, so a pack Arena logs twice (it re-sends
	// on reconnect) is reported once.
	shown *PackShown
}

// Line reads one line of the log, without its newline, and returns the draft
// events it holds: none for almost every line. It never fails.
func (p *Parser) Line(line string) []Event {
	line = strings.TrimSuffix(line, "\r")
	reply := p.reply
	p.reply = ""

	switch {
	case reply != "" && strings.HasPrefix(line, "{"):
		return p.response(reply, line)
	case strings.HasPrefix(line, replyPrefix):
		p.reply = replyName(line[len(replyPrefix):])
		return nil
	case !strings.HasPrefix(line, unityPrefix):
		// Stack traces, audio, and the rest: the bulk of the log.
		return nil
	case strings.HasPrefix(line, notifyPrefix):
		return p.notify(line[len(notifyPrefix):])
	case strings.HasPrefix(line, requestPrefix):
		return p.request(line[len(requestPrefix):])
	}
	return nil
}

// replyName is the name of a response from its header, "Name(id)", or "" for
// a response the parser does not read.
func replyName(header string) string {
	name, _, _ := strings.Cut(header, "(")
	switch name {
	case "EventJoin", "BotDraftDraftStatus", "BotDraftDraftPick", "DraftCompleteDraft":
		return name
	}
	return ""
}

func (p *Parser) debug() *slog.Logger {
	if p.Debug != nil {
		return p.Debug
	}
	return slog.Default()
}

// skip reports a draft line that could not be read.
func (p *Parser) skip(what string, err error) []Event {
	p.debug().Debug("skipped a draft line", "line", what, "reason", err)
	return nil
}

// skipping says whether lines of the draft under way are to be skipped,
// because it is in a format with no fixture, and reports it if so.
func (p *Parser) skipping(what string) bool {
	if p.unknown == "" {
		return false
	}
	p.debug().Debug("skipped a draft line", "line", what, "reason", "format not covered yet", "event", p.unknown)
	return true
}

// started notes that event is now the draft under way, and returns that
// news, or nothing when it was under way already.
func (p *Parser) started(event string, f Format, set string) []Event {
	if event == p.event {
		return nil
	}
	p.event, p.unknown, p.shown = event, "", nil
	return []Event{DraftStarted{Event: event, Set: set, Format: f}}
}

// shownPack reports pack, unless it is the one reported last.
func (p *Parser) shownPack(pack PackShown) []Event {
	if p.shown != nil && p.shown.Pack == pack.Pack && p.shown.Pick == pack.Pick && slices.Equal(p.shown.Cards, pack.Cards) {
		return nil
	}
	p.shown = &pack
	return []Event{pack}
}

// ParseEventName splits Arena's name for an event, such as
// "PremierDraft_FRA_20260929", into its format and set. ok is false for an
// event in a format with no fixture yet, or a name of another shape.
func ParseEventName(name string) (f Format, set string, ok bool) {
	parts := strings.Split(name, "_")
	if len(parts) < 3 || parts[1] == "" {
		return "", "", false
	}
	f, ok = formats[parts[0]]
	if !ok {
		return "", "", false
	}
	return f, parts[1], true
}

// --- Premier Draft: Draft.Notify and EventPlayerDraftMakePick ---

func (p *Parser) notify(body string) []Event {
	if p.skipping("Draft.Notify") {
		return nil
	}
	var n struct {
		SelfPack, SelfPick int
		PackCards          *string
	}
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		return p.skip("Draft.Notify", err)
	}
	if n.SelfPack < 1 || n.SelfPick < 1 || n.PackCards == nil {
		return p.skip("Draft.Notify", fmt.Errorf("no pack and pick number, or no cards: %s", body))
	}
	cards, err := cardList(strings.Split(*n.PackCards, ","))
	if err != nil {
		return p.skip("Draft.Notify", err)
	}
	return p.shownPack(PackShown{Pack: n.SelfPack, Pick: n.SelfPick, Cards: cards})
}

// request reads the requests that are draft events: a pick, in either
// format. Rest is everything after "==> ".
func (p *Parser) request(rest string) []Event {
	name, body, _ := strings.Cut(rest, " ")
	switch name {
	case "EventPlayerDraftMakePick":
		return p.premierPick(body)
	case "BotDraftDraftPick":
		return p.botPick(body)
	}
	return nil
}

// requestBody unwraps a request line's body, {"id":…,"request":"<json>"},
// into the request it carries.
func requestBody(body string, into any) error {
	var r struct{ Request *string }
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return err
	}
	if r.Request == nil {
		return fmt.Errorf("no request in %s", body)
	}
	return json.Unmarshal([]byte(*r.Request), into)
}

func (p *Parser) premierPick(body string) []Event {
	const what = "EventPlayerDraftMakePick"
	if p.skipping(what) {
		return nil
	}
	var r struct {
		GrpIds     []int
		Pack, Pick int
	}
	if err := requestBody(body, &r); err != nil {
		return p.skip(what, err)
	}
	// Every pick a fixture shows is one card. A request for more would be
	// a format that takes several, which no fixture has shown yet.
	if len(r.GrpIds) != 1 || r.Pack < 1 || r.Pick < 1 {
		return p.skip(what, fmt.Errorf("not one card with a pack and pick number: %s", body))
	}
	return []Event{PickMade{Pack: r.Pack, Pick: r.Pick, Card: r.GrpIds[0]}}
}

// --- Quick Draft: BotDraftDraftStatus and BotDraftDraftPick ---

func (p *Parser) botPick(body string) []Event {
	const what = "BotDraftDraftPick"
	var r struct {
		EventName string
		PickInfo  *struct {
			CardIds                []string
			PackNumber, PickNumber int
		}
	}
	if err := requestBody(body, &r); err != nil {
		return p.skip(what, err)
	}
	if _, _, ok := ParseEventName(r.EventName); !ok {
		return p.skip(what, fmt.Errorf("event %q is in a format not covered yet", r.EventName))
	}
	if r.PickInfo == nil || len(r.PickInfo.CardIds) != 1 || r.PickInfo.PackNumber < 0 || r.PickInfo.PickNumber < 0 {
		return p.skip(what, fmt.Errorf("not one card with a pack and pick number: %s", body))
	}
	card, err := cardList(r.PickInfo.CardIds)
	if err != nil {
		return p.skip(what, err)
	}
	// Bot drafts count packs and picks from 0.
	return []Event{PickMade{Pack: r.PickInfo.PackNumber + 1, Pick: r.PickInfo.PickNumber + 1, Card: card[0]}}
}

// botDraft reads the response to BotDraftDraftStatus or BotDraftDraftPick:
// the pack now in front of the player, or the pool when picking is over.
func (p *Parser) botDraft(what, body string) []Event {
	var r struct{ Payload *string }
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return p.skip(what, err)
	}
	if r.Payload == nil {
		return p.skip(what, fmt.Errorf("no payload in %s", body))
	}
	var d struct {
		EventName              string
		DraftStatus            string
		PackNumber, PickNumber int
		DraftPack, PickedCards []string
	}
	if err := json.Unmarshal([]byte(*r.Payload), &d); err != nil {
		return p.skip(what, err)
	}
	f, set, ok := ParseEventName(d.EventName)
	if !ok {
		return p.skip(what, fmt.Errorf("event %q is in a format not covered yet", d.EventName))
	}

	switch d.DraftStatus {
	case "PickNext":
		cards, err := cardList(d.DraftPack)
		if err != nil {
			return p.skip(what, err)
		}
		if len(cards) == 0 || d.PackNumber < 0 || d.PickNumber < 0 {
			return p.skip(what, fmt.Errorf("a pick with no pack: %s", *r.Payload))
		}
		// The status response is also how Arena reopens a bot draft that
		// was joined before this log began, so it can be the first news of
		// the draft.
		events := p.started(d.EventName, f, set)
		return append(events, p.shownPack(PackShown{Pack: d.PackNumber + 1, Pick: d.PickNumber + 1, Cards: cards})...)
	case "Completed":
		pool, err := cardList(d.PickedCards)
		if err != nil {
			return p.skip(what, err)
		}
		return p.ended(pool)
	}
	return p.skip(what, fmt.Errorf("draft status %q", d.DraftStatus))
}

// --- Both formats ---

func (p *Parser) response(name, body string) []Event {
	switch name {
	case "EventJoin":
		return p.join(body)
	case "BotDraftDraftStatus", "BotDraftDraftPick":
		return p.botDraft(name, body)
	case "DraftCompleteDraft":
		return p.completeDraft(body)
	}
	return nil
}

// join reads the response to joining an event. Most events are not drafts;
// those say nothing.
func (p *Parser) join(body string) []Event {
	var r struct {
		Course *struct {
			InternalEventName, CurrentModule string
		}
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return p.skip("EventJoin", err)
	}
	if r.Course == nil {
		return p.skip("EventJoin", fmt.Errorf("no course in %s", body))
	}
	switch r.Course.CurrentModule {
	case "PlayerDraft", "BotDraft":
	default:
		return nil
	}
	name := r.Course.InternalEventName
	f, set, ok := ParseEventName(name)
	if !ok {
		p.event, p.unknown, p.shown = "", name, nil
		return p.skip("EventJoin", fmt.Errorf("event %q is in a format not covered yet", name))
	}
	// Joining is always a new draft, even of an event drafted before.
	p.event = ""
	return p.started(name, f, set)
}

// completeDraft reads the response to DraftCompleteDraft, which a draft
// against people sends when picking is over: the course, with its pool.
func (p *Parser) completeDraft(body string) []Event {
	const what = "DraftCompleteDraft"
	if p.skipping(what) {
		return nil
	}
	var r struct{ CardPool *[]int }
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return p.skip(what, err)
	}
	if r.CardPool == nil {
		return p.skip(what, fmt.Errorf("no card pool in %s", body))
	}
	return p.ended(*r.CardPool)
}

func (p *Parser) ended(pool []int) []Event {
	p.event, p.shown = "", nil
	if pool == nil {
		pool = []int{}
	}
	return []Event{DraftEnded{Pool: pool}}
}

// cardList reads Arena ids written as strings.
func cardList(ids []string) ([]int, error) {
	out := make([]int, 0, len(ids))
	for _, s := range ids {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("card id %q is not a number", s)
		}
		out = append(out, n)
	}
	return out, nil
}
