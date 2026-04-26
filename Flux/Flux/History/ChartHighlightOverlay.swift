import Charts
import SwiftUI

extension View {
    /// Adds a drag gesture to a Chart that maps the touch x to the nearest
    /// entry date and invokes `onSelect` with that entry's day identifier.
    /// All History cards share this so a tap in any chart highlights the
    /// same day across the screen.
    func historySelectionOverlay(
        entries: [(dayID: String, date: Date)],
        onSelect: @escaping (String) -> Void
    ) -> some View {
        chartOverlay { proxy in
            GeometryReader { geometry in
                Rectangle()
                    .fill(.clear)
                    .contentShape(Rectangle())
                    .gesture(
                        DragGesture(minimumDistance: 0)
                            .onChanged { value in
                                guard let plotFrameAnchor = proxy.plotFrame else { return }
                                let plotFrame = geometry[plotFrameAnchor]
                                let relativeX = value.location.x - plotFrame.origin.x
                                guard relativeX >= 0, relativeX <= proxy.plotSize.width,
                                      let date = proxy.value(atX: relativeX) as Date? else {
                                    return
                                }
                                let nearest = entries.min {
                                    abs($0.date.timeIntervalSince(date)) < abs($1.date.timeIntervalSince(date))
                                }
                                if let nearest {
                                    onSelect(nearest.dayID)
                                }
                            }
                    )
            }
        }
    }
}
