import FluxCore
import Foundation
import UserNotifications

/// Drives SoCAlertsView and SoCAlertEditor. Wraps SoCAlertsService so the
/// view layer never reaches into the service directly.
@MainActor
@Observable
final class SoCAlertsViewModel {
    /// Mode of the editor sheet.
    enum EditorMode: Equatable {
        case create
        case edit(SoCAlertRule)
    }

    /// Editor draft. Mutate freely from the editor view; `canSave` reflects
    /// whether the current draft passes validation.
    var draft: SoCAlertRuleDraft = SoCAlertRuleDraft()

    /// `nil` when no sheet is showing. The view binds `isPresented` to
    /// `editorMode != nil`.
    private(set) var editorMode: EditorMode?

    private let service: SoCAlertsService
    private static let ruleCap = 10

    init(service: SoCAlertsService) {
        self.service = service
    }

    // MARK: - Derived view state

    var rules: [SoCAlertRule] { service.rules }

    var addAffordanceEnabled: Bool { rules.count < Self.ruleCap }

    var showsPermissionDeniedBanner: Bool { service.authStatus == .denied }

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

    /// Pass-through so views never reach into SoCAlertsService.shared and
    /// permission/registration flows honour the injected service in tests
    /// and previews.
    func requestAuthorizationAndRegister() async {
        try? await service.requestAuthorizationAndRegister()
    }

    func beginCreate() {
        draft = SoCAlertRuleDraft()
        editorMode = .create
    }

    func beginEdit(_ rule: SoCAlertRule) {
        draft = SoCAlertRuleDraft(rule: rule)
        editorMode = .edit(rule)
    }

    @discardableResult
    func save() async throws -> SoCAlertRule? {
        guard let mode = editorMode else { return nil }
        guard canSave else { return nil }
        switch mode {
        case .create:
            let created = try await service.create(draft)
            editorMode = nil
            return created
        case .edit(let original):
            // The backend stamps its own `updatedAt`; sending `now` here keeps
            // the outgoing payload from carrying a stale value the server is
            // about to overwrite.
            let updated = SoCAlertRule(
                id: original.id,
                thresholdPercent: draft.thresholdPercent,
                windowStart: draft.windowStart,
                windowEnd: draft.windowEnd,
                enabled: draft.enabled,
                label: draft.label,
                createdAt: original.createdAt,
                updatedAt: Date()
            )
            let result = try await service.update(updated)
            editorMode = nil
            return result
        }
    }

    func delete(_ rule: SoCAlertRule) async throws {
        try await service.delete(rule.id)
    }

    func toggleEnabled(_ rule: SoCAlertRule) async throws {
        var updated = rule
        updated.enabled.toggle()
        _ = try await service.update(updated)
    }

    func clearError() { service.clearError() }
}
