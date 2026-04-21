import FluxCore
import SwiftUI

@MainActor
struct WidgetDiagnosticsView: View {
    @State private var lines: [DiagnosticLine] = []

    var body: some View {
        Section("Widget diagnostics") {
            ForEach(lines) { line in
                HStack(alignment: .top) {
                    Image(systemName: line.passed ? "checkmark.circle.fill" : "xmark.octagon.fill")
                        .foregroundStyle(line.passed ? .green : .red)
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
        lines = appGroupChecks() + cacheAndTokenChecks() + heartbeatChecks()
    }

    private func appGroupChecks() -> [DiagnosticLine] {
        let group = UserDefaults.fluxAppGroup
        let isShared = group !== UserDefaults.standard

        let marker = ISO8601DateFormatter().string(from: Date())
        group.set(marker, forKey: "fluxDiagnosticMarker")
        let readBack = group.string(forKey: "fluxDiagnosticMarker")
        let roundTrip = readBack == marker

        return [
            DiagnosticLine(
                title: "App Group suite",
                detail: isShared
                    ? "Shared (\(UserDefaults.fluxAppGroupSuiteName))"
                    : "FELL BACK to UserDefaults.standard — App Group not provisioned",
                passed: isShared
            ),
            DiagnosticLine(
                title: "Suite read/write round-trip",
                detail: roundTrip ? "OK (\(marker))" : "Wrote \(marker) but read back \(readBack ?? "nil")",
                passed: roundTrip
            ),
            DiagnosticLine(
                title: "apiURL visible to widget",
                detail: group.apiURL ?? "(none)",
                passed: group.apiURL != nil
            )
        ]
    }

    private func cacheAndTokenChecks() -> [DiagnosticLine] {
        var new: [DiagnosticLine] = []

        if let envelope = WidgetSnapshotCache().read() {
            let fetched = ISO8601DateFormatter().string(from: envelope.fetchedAt)
            new.append(DiagnosticLine(
                title: "App Group widget cache",
                detail: "envelope present, fetchedAt = \(fetched)",
                passed: true
            ))
        } else {
            new.append(DiagnosticLine(
                title: "App Group widget cache",
                detail: "empty (widget would fall through to placeholder)",
                passed: false
            ))
        }

        if let token = KeychainService().loadToken(), !token.isEmpty {
            new.append(DiagnosticLine(
                title: "Keychain token (main app)",
                detail: "present (\(token.count) chars)",
                passed: true
            ))
        } else {
            new.append(DiagnosticLine(
                title: "Keychain token (main app)",
                detail: "not readable — widget cannot fetch live",
                passed: false
            ))
        }
        return new
    }

    private func heartbeatChecks() -> [DiagnosticLine] {
        let group = UserDefaults.fluxAppGroup
        guard let runAt = group.object(forKey: WidgetDiagnosticKeys.lastRunAt) as? Date else {
            return [DiagnosticLine(
                title: "Widget last heartbeat",
                detail: "never — widget has not produced a timeline (or can't write to App Group)",
                passed: false
            )]
        }

        let fmt = DateFormatter()
        fmt.dateStyle = .short
        fmt.timeStyle = .medium

        let canReadGroup = group.bool(forKey: WidgetDiagnosticKeys.canReadGroup)
        let cacheReadable = group.bool(forKey: WidgetDiagnosticKeys.cacheReadable)
        let tokenReadable = group.bool(forKey: WidgetDiagnosticKeys.tokenReadable)
        let widgetAPIURL = group.string(forKey: WidgetDiagnosticKeys.apiURL) ?? ""
        let lastSource = group.string(forKey: WidgetDiagnosticKeys.lastSource) ?? "(none)"

        return [
            DiagnosticLine(title: "Widget last heartbeat", detail: fmt.string(from: runAt), passed: true),
            DiagnosticLine(
                title: "Widget sees App Group",
                detail: canReadGroup ? "yes" : "NO — widget process can't open the shared suite",
                passed: canReadGroup
            ),
            DiagnosticLine(
                title: "Widget reads cache envelope",
                detail: cacheReadable ? "yes" : "NO — widget cache.read() returns nil",
                passed: cacheReadable
            ),
            DiagnosticLine(
                title: "Widget reads Keychain token",
                detail: tokenReadable ? "yes" : "NO — widget can't load the token",
                passed: tokenReadable
            ),
            DiagnosticLine(
                title: "Widget sees apiURL",
                detail: widgetAPIURL.isEmpty ? "(empty)" : widgetAPIURL,
                passed: !widgetAPIURL.isEmpty
            ),
            DiagnosticLine(
                title: "Widget last entry source",
                detail: lastSource,
                passed: lastSource == "live" || lastSource == "cache"
            )
        ]
    }
}

private struct DiagnosticLine: Identifiable {
    let id = UUID()
    let title: String
    let detail: String
    let passed: Bool
}
