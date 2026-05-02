import Foundation

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

    private let kvs: any KeyValueStore
    private let defaults: UserDefaults
    private let notificationCenter: NotificationCenter
    private var observerTask: Task<Void, Never>?

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
        guard observerTask == nil else { return }
        _ = kvs.synchronize()
        pullFromRemote()
        let center = notificationCenter
        let stream = center.notifications(named: Self.externalChangeNotification)
        observerTask = Task { @MainActor [weak self] in
            for await _ in stream {
                self?.pullFromRemote()
            }
        }
    }

    public func stop() {
        observerTask?.cancel()
        observerTask = nil
    }

    public func write(_ url: String) {
        defaults.apiURL = url
        kvs.set(url, forKey: Self.key)
        _ = kvs.synchronize()
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
