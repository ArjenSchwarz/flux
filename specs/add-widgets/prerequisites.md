# Prerequisites for Add Widgets (T-843)

The Xcode-GUI setup takes one sitting. Completed in this session (2026-04-20) — the checkboxes below are marked done. Kept for reference during rollback or reinstall.

## Xcode sitting — DONE

### Step 1 — Add the local `FluxCore` package to the project

- [x] File → Add Package Dependencies → **Add Local…** → select `Flux/Packages/FluxCore`.
- [x] Add the `FluxCore` library to the `Flux` (main app) target and the `FluxTests` test target.

### Step 2 — Create the widget extension target

- [x] File → New → Target → **Widget Extension**. Xcode auto-appends "Extension" to the name, so the target is `FluxWidgetsExtension`. Folder on disk is `Flux/FluxWidgets/` (synchronized root group path).
- [x] Target → *General* → Frameworks and Libraries: `FluxCore`, `WidgetKit.framework`, `SwiftUI.framework`.
- [x] Delete template files Xcode generated — Phase 4 tasks add the real ones.

### Step 3 — No separate test target

- [x] **Skipped.** Xcode's "Unit Testing Bundle" template does not allow an `.appex` (app extension) as the target-to-be-tested. All widget-testable logic lives in `FluxCore` instead, covered by `FluxCoreTests`. See Decision 19 in `decision_log.md`.

### Step 4 — Enable capabilities

- [x] `FluxWidgetsExtension` target → *Signing & Capabilities* → **App Groups** with `group.me.nore.ig.flux`.
- [x] `Flux` main app target already has App Groups for the same group.
- [x] Xcode auto-added **Keychain Sharing** (`keychain-access-groups` entitlement) to both targets when capabilities were enabled. Both targets now have both App Groups AND Keychain Sharing entitlements using the same group identifier. This is harmless — `KeychainService` uses the bare App Group identifier as `kSecAttrAccessGroup`, which is satisfied by either entitlement.
- [x] Xcode placed the widget entitlements file at `Flux/FluxWidgetsExtension.entitlements` (project root) rather than inside the widget folder. This is the Xcode default and does not need to be moved.

### Step 5 — Provisioning and build verification

- [x] Provisioning profiles auto-updated by Xcode when the App Group was enabled on the widget target.
- [x] `⌘B` builds `Flux` and `FluxWidgetsExtension` clean.

## Before Testing on device (when Phase 4 is complete)

- [ ] On a physical device (not Simulator), verify lock-screen widgets can read the Keychain. Simulator is permissive; device is where entitlement drift shows up.

## Rollback

If the widget work is abandoned:

1. Delete the `FluxWidgetsExtension` target from Xcode → *Targets*.
2. Remove `Flux/FluxWidgets/` directory.
3. Remove the `FluxWidgetsExtension` scheme from Product → Scheme → Manage Schemes.

Removing the `FluxCore` local package is **not** recommended — the main app has taken a dependency on it and reverting that is a larger churn than leaving the package in place.
