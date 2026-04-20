import Foundation

public enum WidgetDeepLink {
    public static let scheme = "flux"
    public static let dashboardURL = URL(string: "flux://dashboard")!

    public enum Destination: Equatable {
        case dashboard
    }

    public static func parse(_ url: URL) -> Destination? {
        guard url.scheme == scheme else { return nil }

        switch url.host {
        case "dashboard":
            return .dashboard
        default:
            return nil
        }
    }
}
