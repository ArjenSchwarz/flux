import SwiftUI
import WidgetKit

@main
struct FluxWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FluxBatteryWidget()
        FluxAccessoryWidget()
    }
}
