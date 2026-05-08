# Prerequisites for What's New (T-1112)

These tasks require human intervention outside of code.

## Before Testing the Feature End-to-End

- [ ] Bump `MARKETING_VERSION` in `Flux/Flux.xcodeproj/project.pbxproj` to the version that ships this feature (e.g. `1.1`). The current value is `1.0`. Without this bump, the auto-presentation path cannot be exercised on a real device — the version-comparison logic only fires when the installed marketing version exceeds the persisted last-seen value. This is conventionally edited via Xcode's target → General → Version field.

## Optional

- [ ] If working with multiple devices: install one build at the pre-feature `MARKETING_VERSION = 1.0`, then upgrade to the post-feature build to verify the Decision 3 seed path (existing v1.0 user upgrading sees the next release's entry).
