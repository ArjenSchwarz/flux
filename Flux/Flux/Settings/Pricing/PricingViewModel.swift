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
        case edit(PricingPeriod)
    }

    var draft: PricingPeriodDraft = PricingPeriodDraft()
    private(set) var editorMode: EditorMode?

    /// The last `PricingValidationReason` surfaced by a failed save. The
    /// editor renders this as either an inline field-level message or a
    /// banner. Cleared on the next successful save.
    private(set) var lastValidationError: PricingValidationReason?

    /// When the last create error was an overlap with the open-ended period,
    /// the editor offers a one-tap remediation button (AC 3.6).
    private(set) var overlapRemediationTargetId: String?

    let service: PricingService

    init(service: PricingService) {
        self.service = service
    }

    var periods: [PricingPeriod] { service.periods }

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
        draft = PricingPeriodDraft()
        editorMode = .create
        lastValidationError = nil
        overlapRemediationTargetId = nil
    }

    func beginEdit(_ period: PricingPeriod) {
        draft = PricingPeriodDraft(period: period)
        editorMode = .edit(period)
        lastValidationError = nil
        overlapRemediationTargetId = nil
    }

    func save() async throws {
        guard let mode = editorMode else { return }
        do {
            switch mode {
            case .create:
                _ = try await service.create(normalisedDraft())
            case .edit(let existing):
                _ = try await service.update(id: existing.id, normalisedDraft())
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

    func delete(_ period: PricingPeriod) async throws {
        try await service.delete(id: period.id)
    }

    /// One-tap remediation for the AC 3.6 flow. Closes the open-ended period
    /// at `draft.startDate − 1 day` and creates the new period in a single
    /// transactional request.
    func remediateOverlap() async throws {
        guard let closingId = overlapRemediationTargetId else { return }
        do {
            _ = try await service.replaceOpenEnded(closingId: closingId, with: normalisedDraft())
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

    private func normalisedDraft() -> PricingPeriodDraft {
        // Persist rates at exactly four decimals so the wire payload matches
        // backend storage precision (Decision 10/20).
        PricingPeriodDraft(
            startDate: draft.startDate,
            endDate: draft.endDate,
            peakRate: PricingPeriodDraft.roundedToFourDP(draft.peakRate),
            feedInRate: PricingPeriodDraft.roundedToFourDP(draft.feedInRate),
            offPeakSavingsRate: PricingPeriodDraft.roundedToFourDP(draft.offPeakSavingsRate)
        )
    }
}
