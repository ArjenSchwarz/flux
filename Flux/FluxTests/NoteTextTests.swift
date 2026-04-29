import FluxCore
import Foundation
import Testing

@Suite
struct NoteTextTests {
    private struct Fixture: Decodable {
        let name: String
        let input: String
        let graphemes: Int
    }

    @Test
    func graphemeCountMatchesCrossStackFixture() throws {
        let entries = try Self.loadFixture()

        for entry in entries {
            let count = NoteText.graphemeCount(entry.input)
            #expect(
                count == entry.graphemes,
                "fixture \(entry.name): expected \(entry.graphemes) graphemes, got \(count)"
            )
        }
    }

    @Test
    func normalisationIsIdempotent() {
        let inputs = [
            "hello",
            "café",                      // NFD form
            "  leading and trailing  ",
            "👨‍👩‍👧‍👦",
            "a b c"
        ]

        for input in inputs {
            let once = NoteText.normalised(input)
            let twice = NoteText.normalised(once)
            #expect(once == twice, "normalise was not idempotent for input \(input)")
        }
    }

    @Test
    func leadingAndTrailingWhitespaceIsTrimmed() {
        #expect(NoteText.normalised("   hello   ") == "hello")
        #expect(NoteText.normalised("\n\thello world\n\t") == "hello world")
        #expect(NoteText.normalised("   ") == "")
    }

    @Test
    func internalWhitespaceIsPreserved() {
        let input = "line one\nline two   spaced"
        #expect(NoteText.normalised(input) == input)
        // Grapheme count covers each space and newline.
        #expect(NoteText.graphemeCount(input) == input.count)
    }

    private static func loadFixture(file: String = #filePath) throws -> [Fixture] {
        // #filePath is <repo>/Flux/FluxTests/NoteTextTests.swift; walk up to repo root.
        let testFile = URL(fileURLWithPath: file)
        let repoRoot = testFile
            .deletingLastPathComponent()  // FluxTests/
            .deletingLastPathComponent()  // Flux/ (Xcode project root)
            .deletingLastPathComponent()  // repo root
        let fixtureURL = repoRoot.appendingPathComponent("internal/api/testdata/note_lengths.json")

        let data = try Data(contentsOf: fixtureURL)
        return try JSONDecoder().decode([Fixture].self, from: data)
    }
}
