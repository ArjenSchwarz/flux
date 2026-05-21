# Prerequisites for SoC Alerts

These tasks must be completed by the user before implementation can finish. All of them can be done before any coding starts — none gate intermediate tasks.

## Before Starting (or any time before Task 17 / 20 / 21)

- [ ] **Apple Developer portal — APNs Authentication Key.** Sign in to <https://developer.apple.com>, *Certificates, Identifiers & Profiles → Keys → "+"*. Create a key with "Apple Push Notifications service (APNs)" enabled. Download the `.p8` file (Apple only lets you download it once — store it somewhere safe). Note the **Key ID** (10 chars, shown next to the key) and **Team ID** (10 chars, top-right of the portal). APNs keys **do not expire** — they are valid until you revoke them in the same portal. (This is the main advantage over the old APNs certificates, which had a 1-year expiry. The short-lived JWTs that the poller sends to APNs are minted from the key automatically and refreshed every ~50 minutes; that's handled by the `sideshow/apns2` library, not by you.) Apple allows up to **two active keys per team**, so when you eventually rotate, you can run both for a window.
- [ ] **Create SSM SecureString parameters.** CloudFormation cannot manage `SecureString`, so create them manually now. They can sit unused until the poller deploys.
  ```bash
  aws ssm put-parameter --name "/flux/apns/key"       --type SecureString \
    --value "$(cat /path/to/AuthKey_XXXXXXXXXX.p8)"
  aws ssm put-parameter --name "/flux/apns/key-id"    --type String --value "XXXXXXXXXX"
  aws ssm put-parameter --name "/flux/apns/team-id"   --type String --value "YYYYYYYYYY"
  aws ssm put-parameter --name "/flux/apns/bundle-id" --type String --value "me.nore.ig.Flux"
  ```
  **The bundle ID is case-sensitive.**
  - **Canonical value**: `PRODUCT_BUNDLE_IDENTIFIER` in `Flux/Flux.xcodeproj/project.pbxproj` (or Xcode → target → Signing & Capabilities). The iOS app target is `me.nore.ig.Flux` with a capital `F`.
  - **Not the App Group**: `group.me.nore.ig.flux` is intentionally lowercase. Don't conflate the two.
  - **Symptom of a mismatch**: APNs returns `status=400 reason=TopicDisallowed` and silently drops every push.

  There is **no** `/flux/apns/env` parameter — the APNs environment (sandbox vs production) is carried per device on its registration row. The poller maintains one HTTP/2 client per environment (same `.p8` key, different host) and dispatches each push against the host that matches the device's token. This is what lets one user run Xcode dev builds while the other runs TestFlight on the same backend.

## Xcode capability — already done; nothing required during this feature

`Flux/Flux/Flux.entitlements` already declares `aps-environment = development` and the "Push Notifications" capability is configured for both iOS and macOS targets. For your normal Xcode-installed dev builds, that's everything — no extra setup. **For TestFlight or App Store builds** the entitlement becomes `aps-environment = production` automatically when Xcode signs with a distribution profile (no source change required). The app reads its own `aps-environment` entitlement at runtime and sends it to the backend on registration, so the poller routes pushes for that device to the correct APNs host without any operator action.

## Before Testing on a Real Device

- [ ] **Bundle ID match (case-sensitive).** Confirm `/flux/apns/bundle-id` matches `PRODUCT_BUNDLE_IDENTIFIER` exactly, including case. APNs treats the `apns-topic` header case-sensitively and returns `status=400 reason=TopicDisallowed` on a mismatch, which the poller logs as `flux_apns_push_failed class=permanent` and otherwise leaves no user-visible signal. Verify with:
  ```bash
  # Look for the line that reads exactly: PRODUCT_BUNDLE_IDENTIFIER = me.nore.ig.Flux;
  # The other entries are tests / UI tests / widget extension.
  grep PRODUCT_BUNDLE_IDENTIFIER Flux/Flux.xcodeproj/project.pbxproj | sort -u
  aws ssm get-parameter --name "/flux/apns/bundle-id" --query 'Parameter.Value' --output text
  ```
- [ ] **Watch the device-token roundtrip on first launch.** Open Settings → Alerts in the app, grant notification permission. In Xcode's Console or Console.app, you should see `didRegisterForRemoteNotificationsWithDeviceToken` fire and a subsequent successful `POST /devices` to the Lambda. If the delegate callback never fires, the app's push entitlement or signing is misconfigured.

## Key Rotation (operational, not for this feature)

When you eventually rotate the key (no scheduled cadence — do it if a key is ever exposed, or when good hygiene demands it):
1. Generate a second key in the Apple Developer portal (you can have two active simultaneously).
2. `aws ssm put-parameter --overwrite --name "/flux/apns/key" --type SecureString --value "$(cat /path/to/new.p8)"` and update `/flux/apns/key-id`.
3. `aws ecs update-service --cluster flux --service flux-poller --force-new-deployment` to make the running poller reload the new key.
4. Once the new key is verified working, revoke the old one in the Apple portal.

There is no deadline on this — it's purely operational.
