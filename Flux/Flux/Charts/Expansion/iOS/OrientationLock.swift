#if canImport(UIKit) && !os(macOS)
import UIKit

@MainActor
final class OrientationLock {
    static let shared = OrientationLock()

    private let defaultMask: UIInterfaceOrientationMask
    private(set) var mask: UIInterfaceOrientationMask
    private(set) var depth: Int = 0

    init(defaultMask: UIInterfaceOrientationMask = .portrait) {
        self.defaultMask = defaultMask
        self.mask = defaultMask
    }

    func enter(_ requested: UIInterfaceOrientationMask) {
        depth += 1
        mask = requested
    }

    func exit() {
        guard depth > 0 else { return }
        depth -= 1
        if depth == 0 {
            mask = defaultMask
        }
    }
}

@MainActor
final class FluxiOSAppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        supportedInterfaceOrientationsFor window: UIWindow?
    ) -> UIInterfaceOrientationMask {
        OrientationLock.shared.mask
    }
}
#endif
