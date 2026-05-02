#if os(macOS)
import AppIntents
import FluxCore
import Foundation
import SwiftUI
import WidgetKit

struct FluxControlWidget: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(
            kind: WidgetKinds.controlBattery,
            provider: ControlSOCProvider(
                cache: WidgetSnapshotCache(),
                logic: WidgetRuntime.makeLogic()
            )
        ) { value in
            ControlWidgetButton(
                action: OpenURLIntent(WidgetDeepLink.dashboardURL)
            ) {
                Label(
                    "\(Int(value.percent))%",
                    systemImage: SOCFormatting.symbol(for: value.percent)
                )
            }
        }
        .displayName("Flux Battery")
        .description("Live battery state")
    }
}
#endif
