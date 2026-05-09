import Foundation

public enum WhatsNewCatalogue {
    public static let releases: [WhatsNewRelease] = [
        WhatsNewRelease(
            version: "1.1",
            date: dateOf(year: 2026, month: 5),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Solar by block",
                    detail: "Day Detail now shows how much solar you generated during morning peak, off-peak, and afternoon peak."
                ),
                Highlight(
                    category: .new,
                    title: "Energy left",
                    detail: "Dashboard shows the usable kWh remaining at the current battery level, next to the empty-by time."
                ),
                Highlight(
                    category: .new,
                    title: "What's New sheet",
                    detail: "Flux shows a summary of what's changed after each app update. Reopen it any time from Settings."
                ),
                Highlight(
                    category: .improved,
                    title: "Lower cutoff threshold",
                    detail: "Battery is treated as empty at 5% instead of 10%, matching the new minimum discharge setting. Predictions and the empty-by time use the new floor."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.0",
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
        guard let date = DateFormatting.sydneyCalendar.date(from: components) else {
            fatalError("WhatsNewCatalogue: invalid date components year=\(year) month=\(month)")
        }
        return date
    }
}
