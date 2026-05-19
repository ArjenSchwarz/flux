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
  aws ssm put-parameter --name "/flux/apns/bundle-id" --type String --value "me.nore.ig.flux"
  aws ssm put-parameter --name "/flux/apns/env"       --type String --value "development"
  ```
  `env=development` is correct for Xcode-built debug installs on your own devices. You will flip it to `production` only when shipping a TestFlight or App Store build (see Xcode capability note below).

## Xcode capability — already done; nothing required during this feature

`Flux/Flux/Flux.entitlements` already declares `aps-environment = development` and the "Push Notifications" capability is configured for both iOS and macOS targets. For your normal Xcode-installed dev builds, that's everything — no extra setup. **The one time you'd touch this** is when you eventually ship to TestFlight or the App Store: change the entitlement to `aps-environment = production` (one-line edit in `Flux.entitlements`, mirrored on macOS) **and** update `/flux/apns/env` SSM param to `production` so the poller talks to Apple's production APNs host. The `.p8` key works on both hosts unchanged.

## Before Testing on a Real Device

- [ ] **Bundle ID match.** Confirm the device-side bundle ID matches `/flux/apns/bundle-id`. APNs rejects pushes when the topic header (bundle ID) differs from what's expected for the key.
- [ ] **Watch the device-token roundtrip on first launch.** Open Settings → Alerts in the app, grant notification permission. In Xcode's Console or Console.app, you should see `didRegisterForRemoteNotificationsWithDeviceToken` fire and a subsequent successful `POST /devices` to the Lambda. If the delegate callback never fires, the app's push entitlement or signing is misconfigured.

## Key Rotation (operational, not for this feature)

When you eventually rotate the key (no scheduled cadence — do it if a key is ever exposed, or when good hygiene demands it):
1. Generate a second key in the Apple Developer portal (you can have two active simultaneously).
2. `aws ssm put-parameter --overwrite --name "/flux/apns/key" --type SecureString --value "$(cat /path/to/new.p8)"` and update `/flux/apns/key-id`.
3. `aws ecs update-service --cluster flux --service flux-poller --force-new-deployment` to make the running poller reload the new key.
4. Once the new key is verified working, revoke the old one in the Apple portal.

There is no deadline on this — it's purely operational.
