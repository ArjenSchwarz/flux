import Foundation

/// Three-state content for the per-row sub-line slot. Encoded as a
/// tagged union so layout-stability and accessibility correctness
/// can't be confused at the type level.
///
/// - `.hidden` — no slot rendered; row at pre-feature height
/// - `.reserved` — slot rendered, no glyph (loading / fallback)
/// - `.text` — slot rendered with the formatted delta string
enum SublineContent: Equatable, Sendable {
    case hidden
    case reserved
    case text(String)
}
