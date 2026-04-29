import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct NoteEditorViewModelTests {
    @Test
    func characterCountUsesGraphemeCount() {
        let parent = makeParent(client: SuccessNoteAPIClient())
        let editor = NoteEditorViewModel(initial: "👨‍👩‍👧‍👦 hello", parent: parent)

        // Family emoji is 1 grapheme + space + "hello" (5) = 7
        #expect(editor.characterCount == 7)
    }

    @Test
    func canSaveBecomesFalseWhileSaving() async {
        let client = SlowNoteAPIClient()
        let parent = makeParent(client: client)
        let editor = NoteEditorViewModel(initial: "draft", parent: parent)
        #expect(editor.canSave)

        let savingTask = Task { await editor.save() }
        // Yield enough times for save() to flip isSaving = true.
        for _ in 0 ..< 5 { await Task.yield() }
        #expect(editor.canSave == false)

        client.release()
        _ = await savingTask.value
    }

    @Test
    func canSaveIsFalseWhenOverGraphemeCap() {
        let parent = makeParent(client: SuccessNoteAPIClient())
        let longText = String(repeating: "a", count: NoteText.maxGraphemes + 1)
        let editor = NoteEditorViewModel(initial: longText, parent: parent)

        #expect(editor.canSave == false)
    }

    @Test
    func saveReturnsTrueOnSuccessAndUpdatesParentNote() async {
        let parent = makeParent(client: SuccessNoteAPIClient())
        let editor = NoteEditorViewModel(initial: "afternoon away", parent: parent)

        let result = await editor.save()

        #expect(result == true)
        #expect(parent.note == "afternoon away")
        #expect(editor.error == nil)
    }

    @Test
    func saveReturnsFalseOnThrowAndKeepsDraftAndSetsError() async {
        let parent = makeParent(client: ThrowingNoteAPIClient())
        let editor = NoteEditorViewModel(initial: "draft", parent: parent)

        let result = await editor.save()

        #expect(result == false)
        #expect(editor.draft == "draft")
        #expect(editor.error == .badRequest("server rejected"))
        #expect(parent.note == nil)
    }

    @Test
    func rapidDoubleTapDoesNotCallApiTwice() async {
        let client = SlowNoteAPIClient()
        let parent = makeParent(client: client)
        let editor = NoteEditorViewModel(initial: "draft", parent: parent)

        async let first = editor.save()
        // Yield until isSaving flips on so the second call sees canSave == false.
        for _ in 0 ..< 5 { await Task.yield() }
        let second = await editor.save()

        client.release()
        let firstResult = await first

        #expect(second == false)
        #expect(firstResult == true)
        #expect(client.callCount == 1)
    }

    private func makeParent(client: any FluxAPIClient) -> DayDetailViewModel {
        DayDetailViewModel(date: "2026-04-15", apiClient: client)
    }
}

private final class SuccessNoteAPIClient: FluxAPIClient, @unchecked Sendable {
    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }

    func saveNote(date: String, text: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: text, updatedAt: "2026-04-15T03:30:00Z")
    }
}

private final class ThrowingNoteAPIClient: FluxAPIClient, @unchecked Sendable {
    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.badRequest("server rejected")
    }
}

private final class SlowNoteAPIClient: FluxAPIClient, @unchecked Sendable {
    private let lock = NSLock()
    private var _callCount = 0
    private let semaphore = DispatchSemaphore(value: 0)

    var callCount: Int {
        lock.lock(); defer { lock.unlock() }
        return _callCount
    }

    func release() {
        semaphore.signal()
    }

    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }

    func saveNote(date: String, text: String) async throws -> NoteResponse {
        lock.lock()
        _callCount += 1
        lock.unlock()
        await withCheckedContinuation { continuation in
            DispatchQueue.global().async { [semaphore] in
                semaphore.wait()
                continuation.resume()
            }
        }
        return NoteResponse(date: date, text: text, updatedAt: "2026-04-15T03:30:00Z")
    }
}
