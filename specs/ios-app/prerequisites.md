# Prerequisites for iOS App

These tasks must be completed by the user before or during implementation.

## Before Starting

- [x] Create Xcode project with iOS 26 deployment target, SwiftUI app lifecycle, and bundle identifier — created at `Flux/`
- [ ] Add App Group capability in Xcode and add an App Group identifier (e.g. `group.eu.arjen.flux`) — needed for Keychain sharing with future widget extension. The current entitlements have CloudKit and push notifications but no App Group.
- [x] SwiftData is already imported in the project template
- [ ] Ensure the Flux backend (Lambda API) is deployed and accessible for integration testing

## During Implementation

- [ ] Add the App Group identifier to the Keychain access group in entitlements (needed before task 5 — KeychainService implementation)
- [ ] Remove sample files (`ContentView.swift`, `Item.swift`) after they are replaced by implementation tasks
