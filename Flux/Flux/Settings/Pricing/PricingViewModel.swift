import FluxCore
import Foundation

/// Drives PricingPeriodsView and PricingEditor. Wraps PricingService so the
/// view layer doesn't reach into the service directly. Surfaces backend
/// validation errors as the editor's banner state.
@MainActor
@Observable
final class PricingViewModel {
    enum EditorMode: Equatable {
        case create
        case edit(PricingPlan)
    }

    var draft: PricingPlanDraft = PricingPlanDraft()
    private(set) var editorMode: EditorMode?

    /// The last `PricingValidationReason` surfaced by a failed save. The
    /// editor renders this as either an inline field-level message or a
    /// banner. Cleared on the next successful save.
    private(set) var lastValidationError: PricingValidationReason?

    /// When the last create error was an overlap with the open-ended plan,
    /// the editor offers a one-tap remediation button (AC 6.5).
    private(set) var overlapRemediationTargetId: String?

    let service: PricingService

    init(service: PricingService) {
        self.service = service
    }

    var plans: [PricingPlan] { service.plans }

    var lastErrorMessage: String? {
        guard let err = service.lastError else { return nil }
        if let api = err as? FluxAPIError {
            return api.message
        }
        return err.localizedDescription
    }

    var showsErrorBanner: Bool {
        if lastValidationError != nil { return true }
        return service.lastError != nil
    }

    var isEditorPresented: Bool { editorMode != nil }

    var canSave: Bool {
        draft.validate() == nil
    }

    func setEditorPresented(_ presented: Bool) {
        if !presented {
            editorMode = nil
            lastValidationError = nil
            overlapRemediationTargetId = nil
        }
    }

    func refresh() async {
        try? await service.refresh()
    }

    func beginCreate() {
        draft = PricingPlanDraft()
        editorMode = .create
        lastValidationError = nil
        overlapRemediationTargetId = nil
    }

    func beginEdit(_ plan: PricingPlan) {
        draft = PricingPlanDraft(plan: plan)
        editorMode = .edit(plan)
        lastValidationError = nil
        overlapRemediationTargetId = nil
    }

    // MARK: - Window editing

    /// Appends a rated window seeded from the plan's default rate — the
    /// starting point for "this stretch costs something other than the norm".
    /// A free window is one toggle away.
    func addWindow() {
        draft.windows.append(
            PlanWindow(start: "00:00", end: "01:00", free: false, rate: draft.defaultRate)
        )
    }

    func removeWindow(at index: Int) {
        guard draft.windows.indices.contains(index) else { return }
        draft.windows.remove(at: index)
    }

    /// Toggles a window between free and rated. A free window carries no rate
    /// by contract, so the rate is dropped on the way in and re-seeded from the
    /// default rate on the way out rather than resurrecting a stale value.
    func setWindowFree(_ free: Bool, at index: Int) {
        guard draft.windows.indices.contains(index) else { return }
        draft.windows[index].free = free
        draft.windows[index].rate = free ? nil : draft.defaultRate
    }

    // MARK: - Persistence

    func save() async throws {
        guard let mode = editorMode else { return }
        do {
            switch mode {
            case .create:
                _ = try await service.create(draft.normalised())
            case .edit(let existing):
                _ = try await service.update(id: existing.id, draft.normalised())
            }
            editorMode = nil
            lastValidationError = nil
            overlapRemediationTargetId = nil
        } catch let error as FluxAPIError {
            if case .pricingValidation(let reason) = error {
                lastValidationError = reason
                if case .overlap(let targetId) = reason {
                    overlapRemediationTargetId = targetId
                }
            }
            throw error
        }
    }

    func delete(_ plan: PricingPlan) async throws {
        try await service.delete(id: plan.id)
    }

    /// One-tap remediation for the AC 6.5 flow. Ends the open-ended plan ON the
    /// new plan's start date and creates the successor, in a single
    /// transactional request. Under exclusive end dates both rows carry the
    /// same literal date and the switch day belongs to the successor (AC 2.2).
    func remediateOverlap() async throws {
        guard let closingId = overlapRemediationTargetId else { return }
        do {
            _ = try await service.replaceOpenEnded(closingId: closingId, with: draft.normalised())
            editorMode = nil
            lastValidationError = nil
            overlapRemediationTargetId = nil
        } catch let error as FluxAPIError {
            if case .pricingValidation(let reason) = error {
                lastValidationError = reason
            }
            throw error
        }
    }

    func clearError() {
        lastValidationError = nil
        service.clearError()
    }

    /// Explanatory copy under the remediation button. Phrased around the switch
    /// day rather than "the day before this start date": the predecessor now
    /// ends ON this date and the successor prices it (AC 6.5).
    static func remediationFooter(startDate: String) -> String {
        "This ends the existing open-ended plan on \(startDate) and starts the new one that same day — "
            + "all in one transaction."
    }
}
