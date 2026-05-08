import Foundation

public struct WhatsNewRelease: Identifiable, Hashable, Sendable {
    public let version: String
    public let date: Date
    public let highlights: [Highlight]

    public var id: String { version }

    public init(version: String, date: Date, highlights: [Highlight]) {
        self.version = version
        self.date = date
        self.highlights = highlights
    }
}

public struct Highlight: Hashable, Sendable {
    public enum Category: String, CaseIterable, Sendable {
        case new
        case improved
        case fixed

        public var label: String {
            switch self {
            case .new: "New"
            case .improved: "Improved"
            case .fixed: "Fixed"
            }
        }
    }

    public let category: Category
    public let title: String
    public let detail: String?
    public let symbol: String?

    public init(category: Category, title: String, detail: String? = nil, symbol: String? = nil) {
        self.category = category
        self.title = title
        self.detail = detail
        self.symbol = symbol
    }
}
