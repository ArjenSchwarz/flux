#if os(macOS)
import AppKit
import Testing
@testable import Flux

@Suite
struct FluxAppDelegateTests {
    @Test
    func applicationShouldTerminateAfterLastWindowClosedReturnsTrue() {
        let delegate = FluxAppDelegate()
        #expect(delegate.applicationShouldTerminateAfterLastWindowClosed(NSApp) == true)
    }
}
#endif
