# Prerequisites for Time-of-Use Pricing

These tasks must be completed by the user before or during implementation. They implement the cutover order from design.md (AC 5.4): deploy band-aware code → migrate → enter the new plan → switch date.

## Before Testing / Cutover

- [ ] Deploy the updated stack (poller image, Lambda, CloudFormation template) — the template change adds the poller's pricing-table read grant and `TABLE_PRICING`, and removes the off-peak SSM parameters and env vars. Until migration runs, the transitional read conversion keeps legacy rows working.
- [ ] Update both installed apps (iOS + macOS) to the band-aware build. Legacy builds fail safely against a migrated API (no costs shown) but should not linger.
- [ ] Run `cmd/migrate-pricing` against production: first with no `--apply` (report-only is the default — review the transform and golden cost verification output), then with `--apply` to write. Note this is the opposite default to the `cmd/backfill-*` tools, which write unless given `--dry-run`; `migrate-pricing` has no `--dry-run` flag and will refuse to start if given one. Must complete before the new plan is entered. **Blocks task 39** (removal of the transitional read conversion).
- [ ] Enter the new plan in the app (end current plan on the switch date, add successor: free 10:00–15:00, cheaper rate 01:00–06:00, default rate otherwise, feed-in and savings reference rates) — before the switch date so the poller picks it up at midnight.
