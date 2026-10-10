---
description: Pick the next roadmap task and land it as one reviewable pull request
---

You are doing tonight's increment of work on `mtga-draft-assistant`. The human
reviews it in the morning, so the deliverable is **one pull request they can
review over coffee**.

Read `CLAUDE.md` first — it holds the priorities and conventions this project is
built around. Then:

## 1. Choose the task

Read `docs/ROADMAP.md` and take the **highest-priority unchecked task** that fits
in a single, complete PR and is not marked **needs human**. Read `CLAUDE.md`'s
"What one pull request means" before you size anything: there is no line target,
and the tests in this codebase are two to four times the length of the code they
cover — judging a task by the total diff is what makes a night's work look too
big when it is not.

Before starting, check open pull requests. If a previous night's PR is still open
and unmerged, prefer work that does not conflict with it, or address review
feedback on it instead of starting something new — an unreviewed pile-up helps
nobody.

If the next task depends on a **needs human** item that is still open (most often
a fixture from a real Arena session), do not invent its input. Take the next task
that does not depend on it, and say in the PR which blocked task you skipped and
what it is waiting for.

If the top task really is too large, take the smallest **complete** slice — one
a user meets as a finished change rather than a staging post — and split the
rest into new roadmap entries. Slice at a seam: another view, another data
source, an open design question. Never slice a single user-visible behaviour
into the honest half tonight and the useful half tomorrow.

The failure mode to watch for here is taking *too little*, not too much.

## 2. Build it

```sh
git fetch origin master
git checkout -b nightly/$(date +%Y-%m-%d)-<short-slug> origin/master
```

Work to the task's acceptance criteria. Non-negotiables from `CLAUDE.md`:

- Tests come with the change, and they cover the acceptance criteria plus the
  awkward cases — an empty or truncated log, a line cut mid-write, a card with
  no rating, a set never seen, a tiny window, an unwritable cache.
- Log formats come from fixtures in `testdata/`, never from memory.
- No `panic`, `log.Fatal`, or `os.Exit` outside `cmd/`; no `unsafe`. The lint
  will stop you. Goroutines recover their own panics.
- Nothing blocks first paint or the UI thread on I/O. State changes only through
  `app.Model.Apply`, posted with `ui.Post`.
- Every new network request is cached, named, rate limited, cancellable, and
  added to the network budget in `docs/ARCHITECTURE.md`.
- Colours come from `ui.Theme`.
- Error messages tell the user what to do next.

## 3. Validate

```sh
make check
```

Formatting, vet, lint, tests under the race detector, cross-compilation, and the
binary size budget must all pass. If a test fails, fix the cause. Never weaken or
delete a test to get to green — if a test is genuinely wrong, change it
deliberately and say so in the PR.

Where a change is visible, launch it and look at it. On a headless Linux machine
with `xvfb`, Mesa EGL (`libegl1 libegl-mesa0 libgl1-mesa-dri`), ImageMagick and
`dbus` installed (the same command CI's smoke job runs):

```sh
dbus-run-session -- xvfb-run -a -s "-screen 0 420x760x24" make smoke
```

That writes `bin/first-paint.png`; read it and check it looks right. For a state
the first paint does not reach (a pack on screen), point the app at a fixture log
once that flag exists, or describe what you checked in the headless tests. CI's
smoke job uploads the same screenshot for the reviewer.

## 4. Audit your own follow-ups

Before you write the PR, read every roadmap entry you are about to add and put
one question to it: **would the reviewer expect this to be in the PR already?**

- *"The deck view should show ratings too"* — a real follow-up. That view is not
  built yet, so there is nothing here to finish.
- *"This error should also say how to fix it"* — not a follow-up. It is the
  message you wrote tonight, still short of the bar in `CLAUDE.md`. Go and
  finish it.

An entry that fails the question is not an entry. Delete it and do the work now,
however large that makes the PR — a finished change beats two thirds of one and
a second review of the same paragraph tomorrow. Whether the extra work is small
is not the test; whether it completes a thought this PR started is.

Keep the entries that pass, and in the PR give each one a sentence saying why it
is separate. If you cannot write that sentence, it was not a follow-up.

## 5. Ship it

Move the completed task to **Done** in `docs/ROADMAP.md` as one line with its
decision numbers, and add the follow-ups that survived the audit. Record notable
choices in `docs/DECISIONS.md`: what was decided and why, in a few lines. The
long explanation belongs in the PR description, not in either file. Update
`docs/ARCHITECTURE.md` when a package or a budget changes. Then commit, push,
and open a PR against `master` using the repository's PR template.

The PR description is the handover — the reviewer has no other context. Say what
changed, why, how you verified it, and what deserves the closest look. Call out
anything you were unsure about; a flagged uncertainty is far more useful than a
confident-sounding guess. Anything you need from the human (a fixture, a call on
a third party's terms) goes at the top of the review notes, not the bottom.

## If you finish early

Do not start an unrelated task. In order:

1. **Finish this one.** Re-run the audit in step 4 over the follow-ups you
   wrote, and fold in anything that fails it. This is the best use of the time,
   every time.
2. **Improve what you built:** another test for a case you did not cover, a
   clearer error message, a doc comment explaining something non-obvious.

Consistent, well-reviewed progress is the goal — volume is not. Neither is a PR
that stops at the first defensible boundary.
