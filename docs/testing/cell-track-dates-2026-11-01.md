# Cell map date persistence — 2026-11-01

Selected local dates are stored immediately in `trackStart` / `trackEnd` URL
parameters. Browser reload, copied links, and the page's Refresh control restore
them. A fresh URL defaults to the last seven days; repeated DST hours are handled
without exceeding the backend's seven-day duration limit. Device/query parameters,
the fragment and existing history state are preserved.

Verification:
- Node regressions reproduced the one-day default and lost dates before the fix.
- Five Node tests pass, including Moscow/New York, repeated DST-hour duration,
  edit-before-submit reload, query/fragment preservation and explicit Refresh.
- Browser fixture verified initial seven-day dates, editing, reload and Refresh.
- Linux `go test ./internal/admin -count=1 -timeout=120s` passed (11.386s);
  `go build -trimpath` succeeded. Final read-only review found no remaining issues.
- Baseline rebuilt on Linux matched the running production binary exactly.
  The production changes are limited to `mdm_track.js` and the Refresh anchor ID
  in `mdm.html`.

Published binary SHA-256:
`c116a407ef24863817db3dc9984620971f29b7f236ccad8ea2fc63160014d439`.
Rollback: `/opt/quic-lab/rollback/cell-dates-20261101`.
Authenticated public HTML and JavaScript hash verified; all four services active;
configuration hashes and Android version unchanged. Cell queue at verification:
1700 done, 0 pending, 0 retry errors.
