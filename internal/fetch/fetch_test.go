package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTime is a clock that only moves when the client sleeps, so rate limits
// and backoffs are tested by what the client asked to wait, not by waiting.
type fakeTime struct {
	mu     sync.Mutex
	at     time.Time
	slept  []time.Duration
	failOn int // the sleep (counting from 1) that reports ctx ended; 0 never
}

func (f *fakeTime) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.at
}

func (f *fakeTime) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.slept = append(f.slept, d)
	if f.failOn == len(f.slept) {
		return context.Canceled
	}
	f.at = f.at.Add(d)
	return ctx.Err()
}

func (f *fakeTime) sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.slept...)
}

// client is a Client for srv with a fake clock and the given limits.
func client(srv *httptest.Server, opts Options) (*Client, *fakeTime) {
	opts.Transport = srv.Client().Transport
	if opts.UserAgent == "" {
		opts.UserAgent = "mtga-draft-assistant/test (+https://example.invalid)"
	}
	c := New(opts)
	ft := &fakeTime{at: time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)}
	c.now, c.sleep = ft.now, ft.sleep
	return c, ft
}

// host is the host name of srv, as the rate limits are keyed.
func host(srv *httptest.Server) string {
	h, _, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	return h
}

func TestEveryRequestNamesTheAppAndAsksForWhatTheCallerAccepts(t *testing.T) {
	var ua, accept string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, accept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c, _ := client(srv, Options{UserAgent: "mtga-draft-assistant/1.0 (+https://example.invalid)"})
	resp, err := c.Get(t.Context(), Request{URL: srv.URL + "/cards", Accept: "application/json"})
	if err != nil {
		t.Fatal(err)
	}
	if ua != "mtga-draft-assistant/1.0 (+https://example.invalid)" || accept != "application/json" {
		t.Errorf("the request carried User-Agent %q and Accept %q", ua, accept)
	}
	if resp.Status != http.StatusOK || string(resp.Body) != "{}" {
		t.Errorf("got %d %q, want 200 {}", resp.Status, resp.Body)
	}
}

func TestRequestsToOneHostAreSpacedByItsLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c, ft := client(srv, Options{Limits: map[string]time.Duration{host(srv): 500 * time.Millisecond}})
	for range 3 {
		if _, err := c.Get(t.Context(), Request{URL: srv.URL}); err != nil {
			t.Fatal(err)
		}
	}
	// The clock only moves when the client sleeps, so each request after
	// the first waits out the whole interval.
	got := ft.sleeps()
	if len(got) != 2 || got[0] != 500*time.Millisecond || got[1] != 500*time.Millisecond {
		t.Errorf("the client slept %v between three requests, want 500ms twice", got)
	}
}

func TestARequestAfterTheIntervalHasPassedDoesNotWait(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c, ft := client(srv, Options{Limits: map[string]time.Duration{host(srv): 500 * time.Millisecond}})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	ft.mu.Lock()
	ft.at = ft.at.Add(time.Second)
	ft.mu.Unlock()
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if got := ft.sleeps(); len(got) != 0 {
		t.Errorf("the client slept %v, though the interval had passed", got)
	}
}

func TestAHostWithNoWrittenLimitGetsOneRequestASecond(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	c, ft := client(srv, Options{Limits: map[string]time.Duration{"api.scryfall.com": time.Millisecond}})
	for range 2 {
		if _, err := c.Get(t.Context(), Request{URL: srv.URL}); err != nil {
			t.Fatal(err)
		}
	}
	if got := ft.sleeps(); len(got) != 1 || got[0] != time.Second {
		t.Errorf("the client slept %v, want one second for a host with no limit", got)
	}
}

func TestEachHostHasItsOwnLimit(t *testing.T) {
	// Plain HTTP, because the test server's certificate does not name
	// localhost: the second host is the same server by another name.
	a := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer a.Close()
	c, ft := client(a, Options{Limits: map[string]time.Duration{"127.0.0.1": time.Hour, "localhost": time.Hour}})
	other := strings.Replace(a.URL, "127.0.0.1", "localhost", 1)
	for _, u := range []string{a.URL, other} {
		if _, err := c.Get(t.Context(), Request{URL: u}); err != nil {
			t.Fatal(err)
		}
	}
	if got := ft.sleeps(); len(got) != 0 {
		t.Errorf("a request to a second host waited %v on the first host's limit", got)
	}
}

func TestABusyServerIsAskedAgainAfterTheWaitItGives(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c, ft := client(srv, Options{Limits: map[string]time.Duration{host(srv): 100 * time.Millisecond}})
	resp, err := c.Get(t.Context(), Request{URL: srv.URL})
	if err != nil || string(resp.Body) != "ok" {
		t.Fatalf("got %q, %v; want the retry's answer", resp.Body, err)
	}
	if got := ft.sleeps(); len(got) != 1 || got[0] != 2*time.Second {
		t.Errorf("the client slept %v before retrying, want the 2s the server asked for", got)
	}
}

func TestABusyServerThatGivesNoWaitIsBackedOffFromExponentially(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c, ft := client(srv, Options{Limits: map[string]time.Duration{host(srv): time.Millisecond}, Backoff: time.Second})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	got := ft.sleeps()
	if len(got) != len(want) {
		t.Fatalf("the client slept %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the client slept %v, want %v", got, want)
		}
	}
}

func TestABusyServerThatStaysBusyIsAnErrorAfterTheRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, _ := client(srv, Options{Retries: 2})
	_, err := c.Get(t.Context(), Request{URL: srv.URL})
	if !IsStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("got %v, want a 429 StatusError", err)
	}
	if calls != 3 {
		t.Errorf("the server was asked %d times, want once and two retries", calls)
	}
	if !strings.Contains(err.Error(), "is busy") {
		t.Errorf("the error says %q, want that the host is busy", err)
	}
}

func TestAServerAskingForALongWaitIsNotWaitedOn(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, ft := client(srv, Options{})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); !IsStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("got %v, want a 429 StatusError", err)
	}
	if calls != 1 || len(ft.sleeps()) != 0 {
		t.Errorf("asked %d times and slept %v, want one request and no hour-long wait", calls, ft.sleeps())
	}
}

func TestAfterABusyAnswerTheNextRequestToThatHostWaitsToo(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, ft := client(srv, Options{Retries: -1, Limits: map[string]time.Duration{host(srv): time.Millisecond}})
	_, _ = c.Get(t.Context(), Request{URL: srv.URL})
	_, _ = c.Get(t.Context(), Request{URL: srv.URL + "/other"})
	// With no retries the first request gives up at once, but the server
	// said it is busy: the second waits as it asked.
	if got := ft.sleeps(); len(got) != 1 || got[0] != 5*time.Second {
		t.Errorf("the second request waited %v, want the 5s the server asked of the first", got)
	}
}

func TestRetryAfterIsReadAsSecondsOrADate(t *testing.T) {
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	for h, want := range map[string]time.Duration{
		"":                              0,
		"7":                             7 * time.Second,
		"0":                             time.Nanosecond,
		"soon":                          0,
		"Fri, 16 Oct 2026 03:00:10 GMT": 10 * time.Second,
		"Fri, 16 Oct 2026 02:00:00 GMT": time.Nanosecond,
		"86400":                         -1,
	} {
		if got := retryAfter(h, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestACopyAlreadyHeldIsAskedAboutNotFetchedAgain(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Thu, 15 Oct 2026 12:00:00 GMT")
		if r.Header.Get("If-None-Match") == `"v1"` && r.Header.Get("If-Modified-Since") == "Thu, 15 Oct 2026 12:00:00 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte("ratings"))
	}))
	defer srv.Close()
	c, _ := client(srv, Options{Limits: map[string]time.Duration{host(srv): time.Millisecond}})
	first, err := c.Get(t.Context(), Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if first.NotModified || string(first.Body) != "ratings" || first.ETag != `"v1"` || first.LastModified == "" {
		t.Fatalf("first fetch = %+v, want the body and its validators", first)
	}
	again, err := c.Get(t.Context(), Request{URL: srv.URL, ETag: first.ETag, LastModified: first.LastModified})
	if err != nil {
		t.Fatal(err)
	}
	if !again.NotModified || len(again.Body) != 0 {
		t.Errorf("conditional fetch = %+v, want NotModified with no body", again)
	}
}

func TestAnErrorStatusSaysWhichHostAndKeepsWhatItExplained(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"object":"error","code":"not_found"}`))
	}))
	defer srv.Close()
	c, _ := client(srv, Options{})
	_, err := c.Get(t.Context(), Request{URL: srv.URL})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound || !strings.Contains(string(se.Body), "not_found") {
		t.Fatalf("got %v, want a 404 StatusError with the body", err)
	}
	if want := host(srv) + " answered 404 Not Found"; err.Error() != want {
		t.Errorf("the error says %q, want %q", err, want)
	}
}

func TestAnUnreachableHostSaysSoByName(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	c, _ := client(srv, Options{})
	srv.Close() // nothing listens there any more
	_, err := c.Get(t.Context(), Request{URL: srv.URL})
	var he *HostError
	if !errors.As(err, &he) || he.Host != "127.0.0.1" {
		t.Fatalf("got %v, want a HostError naming the host", err)
	}
	if err.Error() != "can't reach 127.0.0.1" {
		t.Errorf("the error says %q", err)
	}
}

func TestARequestIsCancelledWithItsContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c, _ := client(srv, Options{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() {
		_, err := c.Get(ctx, Request{URL: srv.URL})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled request returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled request did not return")
	}
}

func TestARequestWaitingOnTheRateLimitIsCancelledToo(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	c, ft := client(srv, Options{})
	ft.failOn = 1
	_, _ = c.Get(t.Context(), Request{URL: srv.URL})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); !errors.Is(err, context.Canceled) {
		t.Errorf("a request cancelled while waiting its turn returned %v", err)
	}
	if calls != 1 {
		t.Errorf("the server was asked %d times, want only the first request", calls)
	}
}

func TestABodyOverTheCapIsAnErrorNotHeld(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	c, _ := client(srv, Options{})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL, MaxBytes: 1024}); err == nil {
		t.Error("a 2 KiB body under a 1 KiB cap was accepted")
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			t.Error("the redirect was followed")
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	c, _ := client(srv, Options{})
	if _, err := c.Get(t.Context(), Request{URL: srv.URL}); !IsStatus(err, http.StatusFound) {
		t.Errorf("got %v, want the redirect reported as its status", err)
	}
}

func TestSomethingThatIsNotAWebAddressIsRefusedWithoutARequest(t *testing.T) {
	c := New(Options{})
	for _, u := range []string{"", "file:///etc/passwd", "api.scryfall.com/cards", "https://"} {
		if _, err := c.Get(t.Context(), Request{URL: u}); err == nil {
			t.Errorf("Get(%q) succeeded", u)
		}
	}
}
