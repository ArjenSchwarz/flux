import Foundation

/// Server-assigned simulation preset (a named added-load scenario). The id,
/// createdAt, and updatedAt fields are populated by the backend; the client
/// treats them as opaque and never mutates them locally. Mirrors the wire
/// shape of `/simulation-presets` (`presetId` → `id`, `label`, `watts`,
/// `createdAt`, `updatedAt`).
public struct SimulationPreset: Identifiable, Codable, Sendable, Equatable {
    public let id: String
    public var label: String
    public var watts: Int
    public let createdAt: Date
    public var updatedAt: Date

    public init(
        id: String,
        label: String,
        watts: Int,
        createdAt: Date,
        updatedAt: Date
    ) {
        self.id = id
        self.label = label
        self.watts = watts
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }
}

/// The editable shape used by the preset editor sheet. The backend assigns
/// id / createdAt / updatedAt on POST, so the draft only carries the writable
/// fields and a local validation method. `watts` defaults to 0 so a fresh
/// draft starts invalid, forcing a deliberate entry (mirrors the empty-label
/// rule).
public struct SimulationPresetDraft: Sendable, Equatable {
    public var label: String
    public var watts: Int

    public init(label: String = "", watts: Int = 0) {
        self.label = label
        self.watts = watts
    }

    /// Convenience: seed a draft from an existing preset for the edit sheet.
    public init(preset: SimulationPreset) {
        self.label = preset.label
        self.watts = preset.watts
    }

    /// Reasons a draft can fail validation. Map back to user-visible text in
    /// the view model so this enum can stay localisation-agnostic.
    public enum ValidationError: Error, Equatable, Sendable {
        case emptyLabel
        case labelTooLong
        case wattsOutOfRange
    }

    /// Inclusive bound on a preset's watt value (Req 1.3). Mirrors the
    /// server-side `presetWattsMax` so a stored preset can never produce a
    /// rejected status request.
    public static let wattsRange = 1 ... 20000
    /// Maximum label length in characters (Req 1.3), matching the server cap.
    public static let labelMaxChars = 40

    /// Local pre-flight validation matching AC 1.3. Returns nil when the
    /// draft is valid. The backend re-validates server-side; this is purely
    /// for early feedback in the editor.
    public func validate() -> ValidationError? {
        let trimmed = label.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return .emptyLabel
        }
        if label.count > Self.labelMaxChars {
            return .labelTooLong
        }
        if !Self.wattsRange.contains(watts) {
            return .wattsOutOfRange
        }
        return nil
    }
}
