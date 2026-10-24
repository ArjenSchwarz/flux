# Flux account and identifier migration

Prepared from release v1.8 (`61856df9faadb2f8b093b8501439ce611af9b506`).
The user selected development team `4PJS97G236` in Xcode before these changes.
All project and target Debug/Release configurations now use that selection.

## Identifiers

| Resource | Prepared value |
| --- | --- |
| iOS and macOS application | `me.nore.ig.apps.Flux` |
| Widget extension | `me.nore.ig.apps.Flux.FluxWidgets` |
| Unit tests | `me.nore.ig.apps.FluxTests` |
| UI tests | `me.nore.ig.apps.FluxUITests` |
| App Group | `group.me.nore.ig.apps.flux` |
| Keychain service | `me.nore.ig.apps.flux` |
| Keychain access-group entitlement | `$(AppIdentifierPrefix)group.me.nore.ig.apps.flux` |
| Runtime App Group keychain selector | `group.me.nore.ig.apps.flux` |
| iCloud key-value store | `$(TeamIdentifierPrefix)$(CFBundleIdentifier)` (expected `4PJS97G236.me.nore.ig.apps.Flux`) |
| Widget kinds | `me.nore.ig.apps.flux.widget.battery`, `me.nore.ig.apps.flux.widget.accessory` |
| URL type name | `me.nore.ig.apps.flux.deeplink` |
| APNs topic required at cutover | `me.nore.ig.apps.Flux` (case-sensitive) |

The existing suffixes and casing are retained. The non-namespaced Control Center
kind `FluxControlBattery` and `flux://` URL scheme remain unchanged. With both apps
installed, the shared URL scheme may resolve to either installation; verify links
after choosing which installation to keep. There is no watch target.

The Keychain entitlement still uses Xcode's AppIdentifierPrefix expansion, and
the runtime retains the existing App Group selector convention. Verify actual
signed entitlements and shared-token access on both iOS and macOS; a successful
unsigned build does not establish those permissions.

## Storage and credentials

This is a new bundle identity, not an in-place App Store app transfer. The former
app (`me.nore.ig.Flux`, team `V24684SCZN`) and its containers are untouched.
The new identity gets separate standard defaults, local SwiftData cache, App Group
preferences/widget cache, Keychain items and iCloud key-value namespace. No code
copies, opens, deletes or resets the previous identity's storage.

Flux has no CloudKit container entitlement or CloudKit record database integration.
Its iCloud usage is the API URL in NSUbiquitousKeyValueStore and a synchronizable
Keychain token. No CloudKit container is created, transferred or selected here.
The existing key-value entitlement expression follows the new team and bundle;
it does not grant access to the former team's URL or credentials.

Before a first installation/launch, decide whether manual reconfiguration is
sufficient. Preserve the former installation until that decision and verification
are complete. Enter the API URL and token into the new app through Settings using
the user's existing credential source; never commit or log credentials. The
backend's energy history remains in the same AWS DynamoDB tables and can be
fetched again once configured. Local preferences, widgets and cached data are not
automatically carried over. A new defaults domain also produces a new per-install
device ID, so existing per-device alert rules do not automatically follow it.

If continuity of local settings, credentials or alert rules is required, plan and
authorize that separately before installing or removing the old app. The existing
settings/accessibility migrators only operate within the selected new identity;
they are not a cross-team migration mechanism.

## Provisioning and push cutover checklist

1. Under team `4PJS97G236`, verify the app and widget IDs, the App Group, shared
   Keychain access, iCloud key-value capability and app push capability. Verify
   Debug and Release profiles on both supported platforms. No portal registration,
   certificate/key creation, trust change or provisioning update was performed.
2. Decide the App Store/TestFlight distribution path for the new bundle ID. It
   cannot replace the existing app record simply by changing the signing team.
3. Plan APNs cutover before granting notification permission in the new app.
   The backend reads one team/key/topic set from `/flux/apns/team-id`, `/key-id`,
   `/key` and `/bundle-id`. The intended new team is `4PJS97G236` and topic is
   `me.nore.ig.apps.Flux`; a valid destination-team APNs key is also required.
   These live values and credentials have not been read or changed.
   Both clients currently share that one key. Environment-restricted APNs keys
   cannot authenticate both hosts; only a key authorised for both environments
   supports simultaneous development and production delivery with this setup.
   [Apple documents the key-scope distinction](https://developer.apple.com/news/?id=wy4tb0uo).
   Separate environment credentials are outside this migration.
4. The current backend distinguishes development/production APNs environments,
   but does not route different teams/topics per registration. Mixing old and new
   app tokens under a single topic can fail delivery and classify tokens as stale.
   Choose an explicit coordinated cutover or implement multi-identity routing
   before coexistence. Preserve old registrations/rules until the plan is approved;
   do not overwrite credentials, purge registrations or redeploy as a side effect
   of this source change.
5. After separate installation authorization, verify credentials, settings,
   widget sharing, cross-device URL/token sync and actual push delivery. Compilation
   and synthetic tests do not prove live account access or data preservation.

Older feature specifications and release notes describe the identifiers used when
those features shipped. This document supersedes their identifier/provisioning
assumptions for this migration; their historical decisions remain intact. The
current SoC-alert prerequisite examples use the new topic but are not commands to
execute before the cutover decision.

## References

- [Apple: configuring App Groups](https://developer.apple.com/documentation/xcode/configuring-app-groups)
- [Apple: sharing Keychain access](https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps)
- [Apple: app transfer overview](https://developer.apple.com/help/app-store-connect/transfer-an-app/overview-of-app-transfer/)

## Validation on 23 October 2026

- Remote `refs/heads/main` was read successfully and still matched the base commit.
- Static checks passed for all 10 project/target configurations, four bundle IDs,
  matching app/widget entitlements, runtime group selectors and Makefile launch ID.
- `plutil -lint` and `git diff --check` passed.
- `go test ./internal/poller/apns` passed using synthetic APNs clients.
- Xcode 27.0 (27A266a): Debug `build-for-testing` for generic iOS Simulator passed;
  Release `build` for generic macOS passed. Both used `CODE_SIGNING_ALLOWED=NO`,
  `SWIFT_EMIT_LOC_STRINGS=NO` and separate derived-data paths under
  `/tmp/flux-apps-migration-20261023/`. Neither provisioning-update flag was supplied.
- Generated app, widget and test Info.plist bundle identifiers were checked against
  the new identifiers. This verifies built configuration, not signed entitlements.
- Swift/iOS test targets were compiled, not executed. No app was installed or
  launched, and no device, Keychain, iCloud or APNs runtime acceptance was attempted.
- The compiler emitted Sendable warnings in unchanged Swift sources.
- The user's untracked Xcode Cloud manifest is preserved byte-for-byte; the original
  main checkout's pre-existing tracked diff also retains its original hash.

Local receipts: `ios-build.log`, `macos-build.log`, `configuration-check.json` and
`product-identifiers.json` under `/tmp/flux-apps-migration-20261023/`.

## Release follow-up on 24 October 2026

The user reports that the new app is deployed, Apple/AWS setup is complete and
ECS has restarted. These are user-reported operational steps, not independently
verified account or delivery results. The user subsequently confirmed: “Confirmed that the Flux notification arrived
on my phone.” This establishes user-confirmed live push delivery on that current
phone installation, not TestFlight delivery or other environment variants.
No AWS changes are part of this release workflow.
