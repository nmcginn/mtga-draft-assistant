// Package fetch is the app's one HTTP client (decision 9). Every request the
// assistant makes goes through Client.Get, which names the app in its
// User-Agent, waits its turn on a per-host rate limit, takes a context so it
// can be cancelled, backs off when a server says it is busy, and can ask
// whether something already held has changed rather than fetching it again.
//
// It does nothing but fetch: what to ask for, and what to do with the
// answer, is up to the source packages under it (scryfall/).
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Client makes the app's requests. Its zero value is not usable; New makes
// one. It is safe to use from several goroutines at once, and the rate limit
// holds across all of them.
type Client struct {
	http      *http.Client
	userAgent string

	// limits is the least time between the starts of two requests to a
	// host; a host not listed gets defaultInterval.
	limits map[string]time.Duration

	// retries is how many times a request the server turned away as busy
	// (429, 503) is tried again; backoff is the first wait, doubled each
	// time, when the server does not say how long to wait.
	retries int
	backoff time.Duration

	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	mu   sync.Mutex
	next map[string]time.Time // when each host may next be asked
}

// Options configure a Client. Zero fields take the defaults.
type Options struct {
	// UserAgent names the app on every request: version.UserAgent().
	UserAgent string
	// Limits is the least time between two requests to each host, by host
	// name ("api.scryfall.com"). Hosts not listed get one second.
	Limits map[string]time.Duration
	// Transport carries the requests; nil is net/http's. Tests point it at
	// an httptest server's.
	Transport http.RoundTripper
	// Timeout bounds a whole request, body included; zero is 30 seconds.
	Timeout time.Duration
	// Retries and Backoff: how often a busy answer is retried (zero is 3),
	// and the first wait between tries when the server does not say (zero
	// is one second). Negative Retries means none.
	Retries int
	Backoff time.Duration
}

// defaultInterval is the rate limit for a host nobody has written one down
// for: slow enough that no service would notice us.
const defaultInterval = time.Second

// maxWait caps how long a server's Retry-After is honoured for. A server that
// asks for longer is better treated as failing, and the source retries the
// whole fetch later on its own schedule.
const maxWait = time.Minute

// New returns a client with opts.
func New(opts Options) *Client {
	c := &Client{
		http:      &http.Client{Transport: opts.Transport, Timeout: opts.Timeout},
		userAgent: opts.UserAgent,
		limits:    opts.Limits,
		retries:   opts.Retries,
		backoff:   opts.Backoff,
		now:       time.Now,
		sleep:     sleep,
		next:      map[string]time.Time{},
	}
	if c.http.Timeout == 0 {
		c.http.Timeout = 30 * time.Second
	}
	if c.retries == 0 {
		c.retries = 3
	}
	if c.retries < 0 {
		c.retries = 0
	}
	if c.backoff == 0 {
		c.backoff = time.Second
	}
	// A redirect is a new request, to a host that may have its own limit
	// and that we did not choose: it is not followed, and the answer is
	// reported as the status it was.
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// Request is one GET.
type Request struct {
	URL string
	// Accept is the Accept header; Scryfall requires one.
	Accept string
	// ETag and LastModified are the validators of a copy already held,
	// from an earlier Response. Set, the request is conditional, and a
	// server that has nothing newer answers NotModified with no body.
	ETag, LastModified string
	// MaxBytes caps the body read; a longer one is an error rather than
	// held in memory. Zero is 16 MiB.
	MaxBytes int64
}

// Response is what came back from a request that succeeded.
type Response struct {
	// Status is the HTTP status: 2xx, or 304 when NotModified.
	Status int
	Body   []byte
	// NotModified says the copy held is still current: the request was
	// conditional and the server had nothing newer. Body is then empty.
	NotModified bool
	// ETag and LastModified are this response's validators, to make the
	// next request for it conditional; empty when the server gave none.
	ETag, LastModified string
}

const defaultMaxBytes = 16 << 20

// Get fetches req.URL. Any status other than 2xx or 304 is an error: a
// *StatusError, after retrying a 429 or 503 up to the client's limit. A
// request that never got an answer is a *HostError. Both say which host, in
// words a user can act on.
func (c *Client) Get(ctx context.Context, req Request) (Response, error) {
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Response{}, fmt.Errorf("not a web address: %q", req.URL)
	}
	for try := 0; ; try++ {
		if err := c.wait(ctx, u.Hostname()); err != nil {
			return Response{}, err
		}
		resp, wait, err := c.do(ctx, u, req)
		if err == nil || wait < 0 {
			return resp, err
		}
		if wait == 0 {
			wait = c.backoff << try
		}
		// Every request to this host waits, not only this one, and even
		// when this one gives up: the server said it is busy, not that
		// this one URL is.
		c.holdOff(u.Hostname(), wait)
		if try >= c.retries {
			return resp, err
		}
	}
}

// do makes one request. wait is how long to wait before trying again when
// the server was busy (0 when it did not say), or -1 when trying again would
// not help.
func (c *Client) do(ctx context.Context, u *url.URL, req Request) (resp Response, wait time.Duration, err error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Response{}, -1, fmt.Errorf("building a request for %s: %w", u.Host, err)
	}
	hr.Header.Set("User-Agent", c.userAgent)
	if req.Accept != "" {
		hr.Header.Set("Accept", req.Accept)
	}
	if req.ETag != "" {
		hr.Header.Set("If-None-Match", req.ETag)
	}
	if req.LastModified != "" {
		hr.Header.Set("If-Modified-Since", req.LastModified)
	}
	r, err := c.http.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, -1, ctx.Err()
		}
		return Response{}, -1, &HostError{Host: u.Hostname(), Err: err}
	}
	defer func() { _ = r.Body.Close() }()

	switch {
	case r.StatusCode == http.StatusNotModified:
		return Response{Status: r.StatusCode, NotModified: true, ETag: r.Header.Get("ETag"), LastModified: r.Header.Get("Last-Modified")}, 0, nil
	case r.StatusCode == http.StatusTooManyRequests || r.StatusCode == http.StatusServiceUnavailable:
		// The body is drained so the connection can be reused for the
		// retry, but its size is bounded: it is an apology, not data.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
		return Response{}, retryAfter(r.Header.Get("Retry-After"), c.now()), &StatusError{Host: u.Hostname(), Code: r.StatusCode, Body: nil}
	case r.StatusCode < 200 || r.StatusCode > 299:
		// Kept, bounded, for a source to read: Scryfall says why in it.
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		return Response{}, -1, &StatusError{Host: u.Hostname(), Code: r.StatusCode, Body: body}
	}

	limit := req.MaxBytes
	if limit <= 0 {
		limit = defaultMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, -1, ctx.Err()
		}
		return Response{}, -1, &HostError{Host: u.Hostname(), Err: err}
	}
	if int64(len(body)) > limit {
		return Response{}, -1, fmt.Errorf("%s sent more than %d bytes for %s", u.Hostname(), limit, u.Path)
	}
	return Response{
		Status:       r.StatusCode,
		Body:         body,
		ETag:         r.Header.Get("ETag"),
		LastModified: r.Header.Get("Last-Modified"),
	}, 0, nil
}

// retryAfter is how long a Retry-After header asks to wait, as seconds or as
// a date; 0 when it says nothing usable, and -1 when it asks for longer than
// is worth waiting in one request.
func retryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	var d time.Duration
	if s, err := strconv.Atoi(h); err == nil {
		d = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = t.Sub(now)
	} else {
		return 0
	}
	switch {
	case d > maxWait:
		return -1
	case d <= 0:
		// "Now" still means after the rate limit's own interval, which
		// wait applies; zero here would mean "pick a backoff".
		return time.Nanosecond
	}
	return d
}

// wait blocks until host may be asked again, and books the slot after.
func (c *Client) wait(ctx context.Context, host string) error {
	interval, ok := c.limits[host]
	if !ok {
		interval = defaultInterval
	}
	c.mu.Lock()
	now := c.now()
	at := c.next[host]
	if at.Before(now) {
		at = now
	}
	c.next[host] = at.Add(interval)
	c.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		return c.sleep(ctx, d)
	}
	return ctx.Err()
}

// holdOff puts host's next slot at least d from now.
func (c *Client) holdOff(host string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at := c.now().Add(d); at.After(c.next[host]) {
		c.next[host] = at
	}
}

// sleep waits d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HostError is a request that got no answer: no connection, a name that
// does not resolve, a timeout. Its text is what a user can check: their
// connection, or whether the host is up.
type HostError struct {
	Host string
	Err  error
}

func (e *HostError) Error() string { return "can't reach " + e.Host }
func (e *HostError) Unwrap() error { return e.Err }

// StatusError is an answer that was not a success.
type StatusError struct {
	Host string
	Code int
	// Body is the start of what the server sent with it, for a source that
	// knows how that server explains itself; nil for a busy answer.
	Body []byte
}

func (e *StatusError) Error() string {
	if e.Code == http.StatusTooManyRequests || e.Code == http.StatusServiceUnavailable {
		return e.Host + " is busy"
	}
	return fmt.Sprintf("%s answered %d %s", e.Host, e.Code, http.StatusText(e.Code))
}

// IsStatus reports whether err is a StatusError with code.
func IsStatus(err error, code int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == code
}
