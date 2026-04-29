import Foundation

public enum NoteText {
    public static let maxGraphemes = 200

    /// NFC + leading/trailing whitespace trim. The Go server applies the
    /// equivalent before counting and storing, so client and server agree.
    public static func normalised(_ text: String) -> String {
        text.precomposedStringWithCanonicalMapping
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Grapheme-cluster count over the NFC + trimmed string. Swift `Character`
    /// is defined as a grapheme cluster (UAX #29), so `String.count` is the
    /// grapheme count.
    public static func graphemeCount(_ text: String) -> Int {
        normalised(text).count
    }
}
