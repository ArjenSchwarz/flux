import Foundation
import UserNotifications

/// Wraps the small handful of UNUserNotificationCenter calls the alert
/// service makes, so SoCAlertsService can be unit-tested without spinning
/// up the real notification subsystem.
public enum NotificationAuthService {
    /// Returns the current authorisation status. Async because
    /// UNUserNotificationCenter.notificationSettings is async on iOS 14+.
    public static func currentStatus() async -> UNAuthorizationStatus {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        return settings.authorizationStatus
    }

    /// Requests alert authorisation. Returns the granted flag; an error
    /// propagates if the system call itself fails (rare).
    public static func requestAlertsAuthorization() async throws -> Bool {
        try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
    }
}
