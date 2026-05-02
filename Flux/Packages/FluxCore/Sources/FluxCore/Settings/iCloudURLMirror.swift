import Foundation
import os

public extension Notification.Name {
    static let fluxCredentialsChanged = Notification.Name("me.nore.ig.flux.credentialsChanged")
}

public protocol KeyValueStore: AnyObject {
    func string(forKey key: String) -> String?
    func set(_ value: String?, forKey key: String)
    @discardableResult
    func synchronize() -> Bool
}

extension NSUbiquitousKeyValueStore: KeyValueStore {}

// swiftlint:disable type_name
@MainActor
public final class iCloudURLMirror {
// swiftlint:enable type_name
    public static let shared: iCloudURLMirror = iCloudURLMirror()

    public static let key = UserDefaults.apiURLKey
    public static let externalChangeNotification = NSUbiquitousKeyValueStore.didChangeExternallyNotification

    nonisolated static let logger = Logger(subsystem: "eu.arjen.flux", category: "icloud-mirror")

    private let kvs: any KeyValueStore
    private let defaults: UserDefaults
    private let notificationCenter: NotificationCenter
    private var observer: NSObjectProtocol?

    public init(
        kvs: any KeyValueStore = NSUbiquitousKeyValueStore.default,
        defaults: UserDefaults = .fluxAppGroup,
        notificationCenter: NotificationCenter = .default
    ) {
        self.kvs = kvs
        self.defaults = defaults
        self.notificationCenter = notificationCenter
    }

    public func start() {
        guard observer == nil else { return }
        // Register the observer synchronously before pulling, so any external
        // notification fired during the pull (or right after) is queued and
        // delivered — an AsyncSequence-based subscription doesn't subscribe
        // until iteration begins, which leaves a race window.
        observer = notificationCenter.addObserver(
            forName: Self.externalChangeNotification,
            object: nil,
            queue: nil
        ) { [weak self] _ in
            Task { @MainActor in
                self?.pullFromRemote()
            }
        }
        _ = kvs.synchronize()
        pullFromRemote()
    }

    public func stop() {
        if let observer {
            notificationCenter.removeObserver(observer)
            self.observer = nil
        }
    }

    public func write(_ url: String) {
        defaults.apiURL = url
        kvs.set(url, forKey: Self.key)
        if !kvs.synchronize() {
            // Most likely the `com.apple.developer.ubiquity-kvstore-identifier`
            // entitlement is missing, so cross-device URL sync silently won't
            // happen. Surface in Console.app for diagnosability.
            Self.logger.warning(
                "NSUbiquitousKeyValueStore.synchronize() returned false; cross-device URL sync may be disabled"
            )
        }
    }

    private func pullFromRemote() {
        guard let remote = kvs.string(forKey: Self.key), !remote.isEmpty else {
            return
        }
        if defaults.apiURL != remote {
            defaults.apiURL = remote
            notificationCenter.post(name: .fluxCredentialsChanged, object: nil)
        }
    }
}
