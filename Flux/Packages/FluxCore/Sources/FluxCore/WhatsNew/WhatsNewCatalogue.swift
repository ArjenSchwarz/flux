import Foundation

public enum WhatsNewCatalogue {
    // Each release note is authored as a single line of prose so it stays easy
    // to read and edit. Line length is not meaningful for this copy, so the
    // rule is disabled across the catalogue and re-enabled right after it.
    // swiftlint:disable line_length
    public static let releases: [WhatsNewRelease] = [
        WhatsNewRelease(
            version: "1.8",
            date: dateOf(year: 2026, month: 7),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Prices that change during the day",
                    detail: "A plan is now a default rate plus the time windows that differ from it — a free window, a cheaper overnight rate, or both. Set them up in Settings → Pricing."
                ),
                Highlight(
                    category: .new,
                    title: "Plans switch over on their own",
                    detail: "Enter your next plan ahead of time with the date it starts, and it takes over on that day without you having to do anything."
                ),
                Highlight(
                    category: .improved,
                    title: "The free window follows your plan",
                    detail: "Chart shading, the off-peak card and the charge projection all pick up the free window from whichever plan covers that day, so they move with it when a plan changes."
                ),
                Highlight(
                    category: .improved,
                    title: "Costs come from your bands",
                    detail: "Each day's cost is worked out band by band. Past days are unaffected — every historical figure was checked against the old calculation before anything changed."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.7",
            date: dateOf(year: 2026, month: 6),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Off-peak charge projection",
                    detail: "While charging during the off-peak window, the Dashboard shows the battery level it's expected to reach by the time the window ends — for example ~99% by 14:00."
                ),
                Highlight(
                    category: .new,
                    title: "Past weeks and months",
                    detail: "History's Wk and Mo ranges can now step back through earlier weeks and months — use the chevrons, tap the label to pick a date, or tap Current to return to today."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.6",
            date: dateOf(year: 2026, month: 6),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Simulate a load",
                    detail: "Tap Simulate on the Dashboard to preview how a load like charging the car would change your battery flow and empty-by time — it's a what-if, nothing actually changes. Manage presets in Settings → Simulation."
                ),
                Highlight(
                    category: .new,
                    title: "Week and month ranges",
                    detail: "History adds Wk and Mo ranges for this week and this month so far, alongside the 7, 14, and 30-day views."
                ),
                Highlight(
                    category: .improved,
                    title: "Totals include today",
                    detail: "History's overview totals now count today, so a range that's only just begun shows real numbers instead of dashes."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.5",
            date: dateOf(year: 2026, month: 6),
            highlights: [
                Highlight(
                    category: .fixed,
                    title: "Large values fit",
                    detail: "When your solar, house, or grid power tops 10 kW, the Dashboard reading no longer wraps onto a second line — it shrinks slightly to stay on one line."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.4",
            date: dateOf(year: 2026, month: 5),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Daily costs",
                    detail: "See what each day cost you — peak imports, solar feed-in, and off-peak savings — on Day Detail, plus totals for the whole range on History. Set your rates in Settings → Pricing."
                ),
                Highlight(
                    category: .new,
                    title: "iPad layout",
                    detail: "On iPad, Flux now uses a sidebar with multi-column screens instead of the stretched iPhone layout."
                ),
                Highlight(
                    category: .new,
                    title: "Won't empty before off-peak",
                    detail: "When your battery can't run down before the off-peak window opens, the Dashboard says so — and shows the current charge or discharge rate."
                ),
                Highlight(
                    category: .improved,
                    title: "Mac app layout",
                    detail: "The Mac app gets the same multi-column layout, with the day's date in the window title and previous/next-day navigation in the toolbar."
                ),
                Highlight(
                    category: .fixed,
                    title: "Accurate grid import",
                    detail: "Your peak and off-peak grid import is now measured from the live readings rather than lagging snapshots, so the split is correct — including today's, updated through the day."
                )
            ]
        ),
        WhatsNewRelease(
            version: "1.3",
            date: dateOf(year: 2026, month: 5),
            highlights: [
                Highlight(
                    category: .new,
                    title: "Battery alerts",
                    detail: "Get a push when your battery drops below a level you choose, inside a time window you set. Up to 10 rules per device — set them up in Settings → Alerts."
                ),
                Highlight(
                    category: .fixed,
                    title: "No more overnight zeros",
                    detail: "When the inverter goes quiet at night, the Dashboard now shows \"Awaiting live data\" instead of locking to 0% and 0 W everywhere."
                )
            ]
        ),
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
    // swiftlint:enable line_length

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
