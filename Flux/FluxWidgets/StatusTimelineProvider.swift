import FluxCore
import Foundation
import WidgetKit

struct StatusTimelineProvider: TimelineProvider {
    private static let widgetSession: URLSession = {
        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5
        config.waitsForConnectivity = false
        return URLSession(configuration: config)
    }()

    private let logic: StatusTimelineLogic

    init(logic: StatusTimelineLogic? = nil) {
        if let logic {
            self.logic = logic
        } else {
            self.logic = StatusTimelineProvider.makeLogic()
        }
    }

    func placeholder(in context: Context) -> StatusEntry {
        logic.placeholder()
    }

    func getSnapshot(in context: Context, completion: @escaping (StatusEntry) -> Void) {
        let isPreview = context.isPreview
        Task {
            let entry = await logic.snapshot(isPreview: isPreview)
            completion(entry)
        }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<StatusEntry>) -> Void) {
        Task {
            let timeline = await logic.timeline()
            #if DEBUG
            Self.writeHeartbeat(firstEntry: timeline.entries.first)
            #endif
            completion(timeline)
        }
    }

    #if DEBUG
    private static func writeHeartbeat(firstEntry: StatusEntry?) {
        let group = UserDefaults(suiteName: UserDefaults.fluxAppGroupSuiteName)
        let shared = group ?? .standard

        shared.set(Date(), forKey: WidgetDiagnosticKeys.lastRunAt)
        shared.set(group != nil, forKey: WidgetDiagnosticKeys.canReadGroup)

        let cache = WidgetSnapshotCache()
        shared.set(cache.read() != nil, forKey: WidgetDiagnosticKeys.cacheReadable)

        let keychain = KeychainService()
        let tokenPresent = (keychain.loadToken()?.isEmpty == false)
        shared.set(tokenPresent, forKey: WidgetDiagnosticKeys.tokenReadable)

        shared.set(UserDefaults.fluxAppGroup.apiURL ?? "", forKey: WidgetDiagnosticKeys.apiURL)

        let sourceString: String
        switch firstEntry?.source {
        case .live: sourceString = "live"
        case .cache: sourceString = "cache"
        case .placeholder: sourceString = "placeholder"
        case .none: sourceString = "no entry"
        }
        shared.set(sourceString, forKey: WidgetDiagnosticKeys.lastSource)
    }
    #endif

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
