import FluxCore
import SwiftUI

@MainActor
struct WidgetDiagnosticsView: View {
    @State private var lines: [DiagnosticLine] = []

    var body: some View {
        Section("Widget diagnostics") {
            ForEach(lines) { line in
                HStack(alignment: .top) {
                    Image(systemName: line.ok ? "checkmark.circle.fill" : "xmark.octagon.fill")
                        .foregroundStyle(line.ok ? .green : .red)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(line.title).font(.subheadline)
                        Text(line.detail)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
            }
            Button("Re-run diagnostics") { run() }
        }
        .onAppear { run() }
    }

    private func run() {
        var new: [DiagnosticLine] = []

        let suiteName = UserDefaults.fluxAppGroupSuiteName
        let group = UserDefaults.fluxAppGroup
        let isSharedSuite = group !== UserDefaults.standard
        new.append(DiagnosticLine(
            title: "App Group suite",
            detail: isSharedSuite
                ? "Shared (\(suiteName))"
                : "FELL BACK to UserDefaults.standard — App Group not provisioned",
            ok: isSharedSuite
        ))

        let key = "fluxDiagnosticMarker"
        let marker = ISO8601DateFormatter().string(from: Date())
        group.set(marker, forKey: key)
        let readBack = group.string(forKey: key)
        let roundTrip = readBack == marker
        new.append(DiagnosticLine(
            title: "Suite read/write round-trip",
            detail: roundTrip ? "OK (\(marker))" : "Wrote \(marker) but read back \(readBack ?? "nil")",
            ok: roundTrip
        ))

        let apiURL = group.apiURL ?? "(none)"
        new.append(DiagnosticLine(
            title: "apiURL visible to widget",
            detail: apiURL,
            ok: group.apiURL != nil
        ))

        let cache = WidgetSnapshotCache()
        if let envelope = cache.read() {
            let fetched = ISO8601DateFormatter().string(from: envelope.fetchedAt)
            new.append(DiagnosticLine(
                title: "App Group widget cache",
                detail: "envelope present, fetchedAt = \(fetched)",
                ok: true
            ))
        } else {
            new.append(DiagnosticLine(
                title: "App Group widget cache",
                detail: "empty (widget would fall through to placeholder)",
                ok: false
            ))
        }

        let keychain = KeychainService()
        if let token = keychain.loadToken(), !token.isEmpty {
            new.append(DiagnosticLine(
                title: "Keychain token",
                detail: "present (\(token.prefix(4))…\(token.suffix(4)), \(token.count) chars)",
                ok: true
            ))
        } else {
            new.append(DiagnosticLine(
                title: "Keychain token",
                detail: "not readable — widget cannot fetch live",
                ok: false
            ))
        }

        lines = new
    }
}

private struct DiagnosticLine: Identifiable {
    let id = UUID()
    let title: String
    let detail: String
    let ok: Bool
}
