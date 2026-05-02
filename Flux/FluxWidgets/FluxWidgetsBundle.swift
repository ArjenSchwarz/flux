import SwiftUI
import WidgetKit

@main
struct FluxWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FluxBatteryWidget()
        #if os(iOS)
        FluxAccessoryWidget()
        #endif
        #if os(macOS)
        FluxControlWidget()
        #endif
    }
}
