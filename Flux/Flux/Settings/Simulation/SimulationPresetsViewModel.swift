import FluxCore
import Foundation

/// Drives SimulationPresetsView and SimulationPresetEditor. Wraps
/// SimulationPresetsService so the view layer never reaches into the service
/// directly. Mirrors SoCAlertsViewModel.
@MainActor
@Observable
final class SimulationPresetsViewModel {
    /// Mode of the editor sheet.
    enum EditorMode: Equatable {
        case create
        case edit(SimulationPreset)
    }

    /// Editor draft. Mutate freely from the editor view; `canSave` reflects
    /// whether the current draft passes validation.
    var draft = SimulationPresetDraft()

    /// `nil` when no sheet is showing. The view binds `isPresented` to
    /// `editorMode != nil`.
    private(set) var editorMode: EditorMode?

    private let service: SimulationPresetsService
    /// Defensive cap (Decision 12); kept in lockstep with the backend's 409.
    static let presetCap = 20

    init(service: SimulationPresetsService) {
        self.service = service
    }

    // MARK: - Derived view state

    var presets: [SimulationPreset] { service.presets }

    var addAffordanceEnabled: Bool { presets.count < Self.presetCap }

    var showsErrorBanner: Bool { service.lastError != nil }

    var lastErrorMessage: String? {
        guard let err = service.lastError else { return nil }
        if let api = err as? FluxAPIError { return api.message }
        return err.localizedDescription
    }

    /// Save is disabled until the draft validates and (in create mode) the
    /// cap is not reached.
    var canSave: Bool {
        guard draft.validate() == nil else { return false }
        if case .create = editorMode, !addAffordanceEnabled { return false }
        return true
    }

    var isEditorPresented: Bool { editorMode != nil }

    /// SwiftUI binding-friendly accessor for sheet presentation. Setting to
    /// false dismisses the editor.
    func setEditorPresented(_ presented: Bool) {
        if !presented { editorMode = nil }
    }

    // MARK: - Lifecycle

    func refresh() async {
        try? await service.refresh()
    }

    func beginCreate() {
        draft = SimulationPresetDraft()
        editorMode = .create
    }

    func beginEdit(_ preset: SimulationPreset) {
        draft = SimulationPresetDraft(preset: preset)
        editorMode = .edit(preset)
    }

    @discardableResult
    func save() async throws -> SimulationPreset? {
        guard let mode = editorMode else { return nil }
        guard canSave else { return nil }
        switch mode {
        case .create:
            let created = try await service.create(draft)
            editorMode = nil
            return created
        case .edit(let original):
            // The backend stamps its own `updatedAt`; the local timestamp is
            // overwritten server-side.
            let updated = SimulationPreset(
                id: original.id,
                label: draft.label,
                watts: draft.watts,
                createdAt: original.createdAt,
                updatedAt: Date()
            )
            let result = try await service.update(updated)
            editorMode = nil
            return result
        }
    }

    func delete(_ preset: SimulationPreset) async throws {
        try await service.delete(preset.id)
    }

    func clearError() { service.clearError() }
}
