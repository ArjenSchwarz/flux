import Foundation

public enum WhatsNewCatalogue {
    public static let releases: [WhatsNewRelease] = [
        WhatsNewRelease(
            version: "1.1",
            date: dateOf(year: 2026, month: 5),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Mac app",
                    detail: "Flux now runs natively on macOS with the same Dashboard, History, and Day Detail screens."
                ),
                Highlight(
                    category: .new,
                    title: "Light and dark mode",
                    detail: "Pick Light, Dark, or Follow System in Settings."
                ),
                Highlight(
                    category: .new,
                    title: "Custom fonts",
                    detail: "Choose any font installed on your device for the whole app."
                ),
                Highlight(
                    category: .improved,
                    title: "Redesigned dashboard",
                    detail: "Battery, solar, house, and grid live values are easier to scan at a glance."
                ),
                Highlight(
                    category: .improved,
                    title: "History overview",
                    detail: "Eight at-a-glance tiles summarise your last 7, 14, or 30 days."
                ),
                Highlight(
                    category: .improved,
                    title: "Day in five blocks",
                    detail: "Each day's energy split shows up at the top of Day Detail."
                )
            ]
        )
    ]

    private static func dateOf(year: Int, month: Int) -> Date {
        var components = DateComponents()
        components.year = year
        components.month = month
        components.day = 1
        components.timeZone = TimeZone(identifier: "Australia/Sydney")
        return Calendar(identifier: .gregorian).date(from: components) ?? Date(timeIntervalSince1970: 0)
    }
}
