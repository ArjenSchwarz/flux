# Prerequisites for Add Widgets (T-843)

The Xcode-GUI setup takes one sitting and sits between task 1 (which the agent runs) and task 2. Everything below requires the Xcode UI and cannot be done by a coding agent.

## Before task 2 — one Xcode sitting

The agent has just completed task 1, which created:

- `Flux/Packages/FluxCore/Package.swift`
- Empty `Sources/FluxCore/{Models,Networking,Security,Helpers,Widget}` and `Tests/FluxCoreTests/` directories
- A placeholder source file so the empty package compiles

Now do all of the following in Xcode, in order. Once finished, hand back to the agent and it will run tasks 2–43 without further interruption.

### Step 1 — Add the local `FluxCore` package to the project

- [ ] Open `Flux/Flux.xcodeproj`.
- [ ] File → Add Package Dependencies → **Add Local…** → select `Flux/Packages/FluxCore`.
- [ ] In the package-product sheet, add the `FluxCore` library to:
  - the `Flux` (main app) target, and
  - the `FluxTests` test target (so migrated tests can `import FluxCore`).
- [ ] Confirm the package appears under the project's *Package Dependencies* list.
- [ ] Build the main app once (`⌘B`) — should compile clean.

### Step 2 — Create the `FluxWidgets` extension target

- [ ] File → New → Target → **Widget Extension** → name it `FluxWidgets`, bundle ID `me.nore.ig.Flux.FluxWidgets`, untick "Include Configuration Intent".
- [ ] Delete the template files Xcode generates (the generated `FluxWidgets.swift`, provider, and entry) — they'll be replaced by files added in Phase 4.
- [ ] FluxWidgets target → *General* → Deployment target: iOS 26.4.
- [ ] FluxWidgets target → *General* → Frameworks and Libraries: add `FluxCore` (local package product), `WidgetKit.framework`, `SwiftUI.framework`.

### Step 3 — Create the `FluxWidgetsTests` test target

- [ ] File → New → Target → **Unit Testing Bundle** → name it `FluxWidgetsTests`, target to be tested = `FluxWidgets`.
- [ ] Add `FluxCore` and `WidgetKit.framework` as linked frameworks for the test target.
- [ ] Tasks 33, 35, and 37 (`RelevanceScoringTests`, `WidgetAccessibilityTests`, `StatusTimelineProviderTests`) will write files into this target.

### Step 4 — Enable capabilities on both targets

- [ ] On the `FluxWidgets` target → *Signing & Capabilities*:
  - Add **App Groups** and enable `group.me.nore.ig.flux`.
  - Add **Keychain Sharing** and add the group `group.me.nore.ig.flux` (Xcode prefixes with `$(AppIdentifierPrefix)` automatically).
- [ ] On the `Flux` (main app) target → *Signing & Capabilities*:
  - Verify **Keychain Sharing** is already enabled with `group.me.nore.ig.flux`. If not, add it. App Groups should already be present.
- [ ] Open both `.entitlements` files and confirm `keychain-access-groups` contains `$(AppIdentifierPrefix)group.me.nore.ig.flux` in each.

### Step 5 — Provisioning and final verification

- [ ] Confirm the development provisioning profile for `me.nore.ig.Flux` includes:
  - App Group entitlement for `group.me.nore.ig.flux`.
  - Keychain Access Group entitlement for `group.me.nore.ig.flux`.
- [ ] Confirm the `FluxWidgets` target's provisioning profile includes both entitlements (Xcode usually handles this automatically when you add capabilities).
- [ ] Build the whole project (`⌘B`). All four schemes should build clean: `Flux`, `FluxTests`, `FluxUITests`, `FluxWidgets`.
- [ ] Run the main-app tests (`⌘U`) — pre-existing tests should all still pass; no widget coding has happened yet.

Once all five steps are green, tell the agent to continue with task 2.

## Before Testing on device

- [ ] On a physical device (not Simulator), verify lock-screen widgets can read the Keychain once Phase 4 is complete. Simulator is permissive here; device is where entitlement drift shows up.

## Rollback

If the widget target turns out to be unneeded, remove it cleanly by:

1. Deleting the `FluxWidgets` and `FluxWidgetsTests` targets from Xcode's *Targets* list.
2. Removing `Flux/FluxWidgets/` and `Flux/FluxWidgetsTests/` directories.
3. Removing the `FluxWidgets` scheme.

Removing the local `FluxCore` package is *not* recommended even if the widgets are abandoned — the main app will have taken a dependency on it, and reverting that is a larger churn than leaving the package in place.
