# AGENTS.md

Reverse-engineered API clients and protocol documentation for two dating networks that
publish no API. Read this before making changes; several constraints here are not
discoverable from the code.

## Layout

Three Go modules, joined by `go.work`:

```
core/             shared transport; STDLIB ONLY
recon/            Recon API client     → core + coder/websocket
scruff/           SCRUFF API client    → core + coder/websocket
docs/api/         the protocol references
docs/openapi/     OpenAPI 3.1, REST surface only
docs/research/    raw reverse-engineering notes
```

`recon` and `scruff` are separate modules so that someone who wants a Recon client does
not pull in the SCRUFF one, or vice versa. That split is the point — don't collapse it.

`core` holds only what genuinely generalises across two protocols that share nothing.
Resist putting network-specific behaviour there; if it has "recon" or "scruff" in the
name, it belongs in that module.

## Building

```sh
for m in core recon scruff; do (cd $m && go build ./... && go vet ./... && go test ./...); done
```

A wildcard from the repository root reaches **none** of the modules — `go test ./...` at
the root matches nothing and exits 0, which reads as a pass. Always iterate.

`recon/go.mod` and `scruff/go.mod` carry both `require core v0.1.0` and
`replace core => ../core`. That is deliberate: Go applies `replace` only from the main
module, so consumers resolve the tagged version while a fresh clone still builds.

## Hard rules

1. **No vendor source code in the repo.** The docs are original prose describing observed
   behaviour. Do not paste decompiled Java, minified JS, or obfuscated identifiers —
   including as "evidence" in a comment.
2. **The SDKs never log.** No logger, no `fmt.Print`. The caller owns logging, and
   credentials pass through these packages.
3. **Unit tests never touch the network.** A test pointed at `httptest` once dialled the
   real API because a helper hardcoded the production base URL. Every code path must
   honour `WithBaseURL`.
4. **`core` stays stdlib-only.** Its `go.mod` has no `require` block.
5. **Nothing derived from a live account gets committed.** Captures hold real tokens and
   third-party personal data. Fixtures are hand-written or heavily redacted.
6. **Write paths are `[client]`-derived and unverified.** Almost nothing that mutates has
   been sent to a real API. Tests therefore assert the *request we construct* — the part
   we actually know — rather than server behaviour we have not seen. Do not "fix" a write
   to match a guess about the response; if you observe one, record it and update the tag.

## Documentation conventions

Every endpoint in `docs/api/` carries a provenance tag. **`[observed]` and `[client]` are
both reliable** — they record *how* a fact was established, not how much to trust it:

- `[observed]` — seen on the wire.
- `[client]` — read from the vendor's own implementation. Paths, parameters, body shapes
  and enum values are authoritative; the limitation is coverage, not accuracy.
- `[unverified]` — genuinely undetermined. Each instance says what was tried, and every
  document ends with a "Known gaps" section listing them.

If you resolve an `[unverified]` item, move it to the "Recently closed" list rather than
deleting it — knowing that something *was* uncertain is useful.

Internal links are checked in CI; keep anchors valid when you rename a heading. Note that
adding a provenance tag to a heading changes its anchor.

## Per-network gotchas

Each module has its own `AGENTS.md` with the full list. The two that cause the most
wasted time:

- **Recon** returns `accessToken` already prefixed with `"Bearer "`. Adding your own
  prefix yields `Bearer Bearer`, which the *profile* service accepts and the *messaging*
  and *SignalR* services reject with an empty-body 401 — so it looks like it works.
  See [`recon/AGENTS.md`](recon/AGENTS.md).
- **SCRUFF**'s HMAC base is `lon~lat~client_version~device_type`. The `device_id` form
  that several write-ups state is wrong. See [`scruff/AGENTS.md`](scruff/AGENTS.md).

## Ethics and scope

This exists for interoperability — talking to your own accounts — and for documenting
protocols that have no public specification. Keep it that way:

- The line is **member, not moderator**. Everything a user can do is in scope: messaging,
  profile editing, woofs/cruises, favorites, blocks, follows, RSVPs, albums, moments.
  Administrator and anti-fraud surface is not — Recon's payment and verification services
  and its `dvrt/admin` paths, SCRUFF's `trials/admin_*`, `boost/grant`, `face_liveness`,
  `sms/send` and `captcha`. Those stay documented and unimplemented.
- Be deliberate about anything that contacts, notifies, or affects another person. Those
  calls exist, but one per deliberate human action — never in a loop over a grid page.
- Respect the networks' rate expectations. `scruff` mirrors the app's own throttle for
  this reason; keep it on.
- Don't add scraping helpers, bulk enumeration, or anything whose primary use is
  harvesting other users' data.
