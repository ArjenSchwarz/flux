import Foundation
import Testing
@testable import FluxCore

// swiftlint:disable type_name
@MainActor
@Suite(.serialized)
struct iCloudURLMirrorTests {
// swiftlint:enable type_name
    @Test
    func writeUpdatesBothKVSAndUserDefaults() {
        let kvs = InMemoryKeyValueStore()
        let defaults = makeDefaults()
        let mirror = iCloudURLMirror(
            kvs: kvs,
            defaults: defaults,
            notificationCenter: NotificationCenter()
        )

        mirror.write("https://example.com")

        #expect(defaults.apiURL == "https://example.com")
        #expect(kvs.string(forKey: iCloudURLMirror.key) == "https://example.com")
    }

    @Test
    func startSeedsUserDefaultsFromKVS() {
        let kvs = InMemoryKeyValueStore()
        kvs.set("https://kvs-value.example", forKey: iCloudURLMirror.key)
        let defaults = makeDefaults()
        let mirror = iCloudURLMirror(
            kvs: kvs,
            defaults: defaults,
            notificationCenter: NotificationCenter()
        )

        mirror.start()
        defer { mirror.stop() }

        #expect(defaults.apiURL == "https://kvs-value.example")
    }

    @Test
    func externalKVSChangeUpdatesUserDefaultsMirror() async throws {
        let kvs = InMemoryKeyValueStore()
        kvs.set("https://initial.example", forKey: iCloudURLMirror.key)
        let defaults = makeDefaults()
        let center = NotificationCenter()
        let mirror = iCloudURLMirror(
            kvs: kvs,
            defaults: defaults,
            notificationCenter: center
        )

        mirror.start()
        defer { mirror.stop() }

        kvs.set("https://changed.example", forKey: iCloudURLMirror.key)
        center.post(
            name: iCloudURLMirror.externalChangeNotification,
            object: nil
        )

        try await waitForUpdate(defaults: defaults, expected: "https://changed.example")
        #expect(defaults.apiURL == "https://changed.example")
    }

    @Test
    func startDoesNotOverwriteWhenKVSEmpty() {
        let kvs = InMemoryKeyValueStore()
        let defaults = makeDefaults()
        defaults.apiURL = "https://local.example"
        let mirror = iCloudURLMirror(
            kvs: kvs,
            defaults: defaults,
            notificationCenter: NotificationCenter()
        )

        mirror.start()
        defer { mirror.stop() }

        #expect(defaults.apiURL == "https://local.example")
    }

    private func makeDefaults() -> UserDefaults {
        let suite = "flux.test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        return defaults
    }

    private func waitForUpdate(
        defaults: UserDefaults,
        expected: String,
        timeout: Duration = .milliseconds(500)
    ) async throws {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while ContinuousClock.now < deadline {
            if defaults.apiURL == expected { return }
            try await Task.sleep(for: .milliseconds(10))
        }
    }
}

final class InMemoryKeyValueStore: KeyValueStore, @unchecked Sendable {
    private let lock = NSLock()
    private var storage: [String: String] = [:]

    func string(forKey key: String) -> String? {
        lock.withLock { storage[key] }
    }

    func set(_ value: String?, forKey key: String) {
        lock.withLock {
            if let value {
                storage[key] = value
            } else {
                storage.removeValue(forKey: key)
            }
        }
    }

    @discardableResult
    func synchronize() -> Bool { true }
}
