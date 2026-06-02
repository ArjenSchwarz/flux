#if os(macOS)
import AppKit
import FluxCore
import os

final class FluxAppDelegate: NSObject, NSApplicationDelegate {
    private let log = Logger(subsystem: "me.nore.ig.flux", category: "apns")

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Note: macOS NSApplication.registerForRemoteNotifications does NOT
        // prompt for permission — the prompt comes from
        // UNUserNotificationCenter.requestAuthorization via SoCAlertsService.
        // Registration is triggered there when authorisation is granted.
    }

    // MARK: - APNs token lifecycle (soc-alerts spec)

    func application(
        _ application: NSApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        Task { @MainActor in
            try? await SoCAlertsService.shared.registerDeviceIfNeeded(
                token: deviceToken,
                timeZone: .current
            )
        }
    }

    func application(
        _ application: NSApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        log.error("APNs registration failed: \(error.localizedDescription, privacy: .public)")
    }
}
#endif
