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
            completion(timeline)
        }
    }

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
