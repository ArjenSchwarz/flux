import FluxCore
import Foundation
import Observation

@MainActor @Observable
final class NoteEditorViewModel {
    var draft: String
    private(set) var isSaving = false
    private(set) var error: FluxAPIError?

    private let parent: DayDetailViewModel

    init(initial: String, parent: DayDetailViewModel) {
        draft = initial
        self.parent = parent
    }

    var characterCount: Int { NoteText.graphemeCount(draft) }

    var canSave: Bool { !isSaving && characterCount <= NoteText.maxGraphemes }

    /// Returns true on a successful save (caller dismisses the sheet); false
    /// when the call was suppressed (e.g. concurrent save in flight) or the
    /// backend rejected the write — in the failure case `error` is populated
    /// and the draft text is left intact for retry.
    @discardableResult
    func save() async -> Bool {
        guard canSave else { return false }
        isSaving = true
        error = nil
        defer { isSaving = false }

        do {
            try await parent.saveNote(draft)
            return true
        } catch {
            self.error = FluxAPIError.from(error)
            return false
        }
    }
}
