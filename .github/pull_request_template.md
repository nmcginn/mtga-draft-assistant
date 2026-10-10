## What changed

<!-- The change in a couple of sentences. What can a user do now that they could not before? -->

## Why

<!-- Link the roadmap task this completes, or the issue it fixes. -->

Roadmap task:

## How it was verified

<!-- Tests added, and anything checked by hand. For anything visible, attach the
smoke job's first-paint screenshot or a headless render. -->

- [ ] `make check` passes
- [ ] Tests cover the task's acceptance criteria
- [ ] Tests cover the awkward cases (empty input, a log cut mid-line, a card with no rating, a tiny window)
- [ ] No new `panic`, `log.Fatal`, or `os.Exit` outside `cmd/` and tests
- [ ] Nothing new blocks first paint or the UI thread on I/O
- [ ] Every new network request is cached, rate limited, and named in `docs/ARCHITECTURE.md`'s network budget

## Review notes

<!-- What deserves the closest look? Anything you were unsure about, or deliberately left out? -->

<!-- Added roadmap follow-ups? One sentence each on why it is a separate PR rather than part of this one. If it finishes something this PR started, it is not a follow-up. -->
