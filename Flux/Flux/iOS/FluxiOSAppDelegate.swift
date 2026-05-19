#if canImport(UIKit) && !os(macOS)
import FluxCore
import UIKit
import os

@MainActor
final class FluxiOSAppDelegate: NSObject, UIApplicationDelegate {
    private let log = Logger(subsystem: "me.nore.ig.flux", category: "apns")

    func application(
        _ application: UIApplication,
        supportedInterfaceOrientationsFor window: UIWindow?
    ) -> UIInterfaceOrientationMask {
        OrientationLock.shared.mask
    }

    // MARK: - APNs token lifecycle (soc-alerts spec)

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        // The SoCAlertsService.shared singleton is @MainActor; the delegate
        // callback is already on the main thread but the explicit Task hop
        // satisfies Swift 6 strict concurrency without warnings.
        Task { @MainActor in
            try? await SoCAlertsService.shared.registerDeviceIfNeeded(
                token: deviceToken,
                tz: .current
            )
        }
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        log.error("APNs registration failed: \(error.localizedDescription, privacy: .public)")
    }
}
#endif
