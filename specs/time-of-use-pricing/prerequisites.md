# Prerequisites for Time-of-Use Pricing

These tasks must be completed by the user before or during implementation. They implement the cutover order from design.md (AC 5.4): deploy band-aware code → migrate → enter the new plan → switch date.

## Before Testing / Cutover

- [x] Deploy the updated stack (poller image, Lambda, CloudFormation template) — the template change adds the poller's pricing-table read grant and `TABLE_PRICING`, and removes the off-peak SSM parameters and env vars. Until migration runs, the transitional read conversion keeps legacy rows working.
- [x] Update both installed apps (iOS + macOS) to the band-aware build. Legacy builds fail safely against a migrated API (no costs shown) but should not linger.
- [x] Run `cmd/migrate-pricing` against production: first with no `--apply` (report-only is the default — review the transform and golden cost verification output), then with `--apply` to write. Note this is the opposite default to the `cmd/backfill-*` tools, which write unless given `--dry-run`; `migrate-pricing` has no `--dry-run` flag and will refuse to start if given one. Must complete before the new plan is entered. **Blocks task 39** (removal of the transitional read conversion).
- [ ] Enter the new plan in the app (end current plan on the switch date, add successor: free 10:00–15:00, cheaper rate 01:00–06:00, default rate otherwise, feed-in and savings reference rates) — before the switch date so the poller picks it up at midnight.

## Cutover Record

Completed 2026-07-24. The migration ran clean: 1 legacy row transformed, 101 priced days verified identical under both formulas, 0 mismatches, 0 unpriced. Task 39 was unblocked by that run and is now done — the read path rejects the legacy shape instead of converting it.

Two things went wrong during the deploy that are worth remembering:

- **The stack was deployed before the poller image finished building.** `ContainerImageUri` is `:latest`, and the arm64 build takes ~7 minutes while the CloudFormation deploy is much faster, so the new template ran the old binary. Rolling the template back then inverted the mismatch (old template, new image), and the rollback could not complete because the ECS service never stabilised. Deploy the image first, or pin `ContainerImageUri` to the `sha-<short>` tag.
- **Retagging `:latest` does not affect a running ECS deployment.** ECS resolves a tag to a digest once, when a deployment is created, and pins it for that deployment's lifetime — so every retry kept pulling the digest resolved before the retag. `aws ecs update-service --force-new-deployment` is what makes it re-resolve.
