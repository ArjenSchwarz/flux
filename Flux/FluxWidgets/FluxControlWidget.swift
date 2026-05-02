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
                logic: Self.makeLogic()
            )
        ) { value in
            ControlWidgetButton(
                action: OpenURLIntent(URL(string: "flux://dashboard")!)
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

    private static let widgetSession: URLSession = {
        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5
        config.waitsForConnectivity = false
        return URLSession(configuration: config)
    }()

    private static func makeLogic() -> StatusTimelineLogic {
        let cache = WidgetSnapshotCache()
        let keychain = KeychainService()
        let client = makeAPIClient(keychain: keychain)
        return StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { keychain.loadToken() }
        )
    }

    private static func makeAPIClient(keychain: KeychainService) -> (any FluxAPIClient)? {
        guard let raw = UserDefaults.fluxAppGroup.apiURL?
                .trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty,
              let url = URL(string: raw) else {
            return nil
        }
        return URLSessionAPIClient(
            baseURL: url,
            keychainService: keychain,
            session: widgetSession
        )
    }
}
#endif
