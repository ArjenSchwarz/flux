import Foundation

public struct WhatsNewCoordinator: Sendable {
    public enum AutoDecision: Equatable, Sendable {
        case present(releases: [WhatsNewRelease])
        case silentSet(version: String)
        case skip
    }

    private struct ParsedRelease {
        let release: WhatsNewRelease
        let version: WhatsNewVersion
    }

    private let parsedCatalogue: [ParsedRelease]
    private let installed: WhatsNewVersion
    private let installedString: String
    private let lastSeen: String?
    private let hasAnyFluxPref: Bool

    public init(
        catalogue: [WhatsNewRelease],
        installed: WhatsNewVersion,
        lastSeen: String?,
        hasAnyFluxPref: Bool
    ) {
        self.parsedCatalogue = catalogue.compactMap { release in
            guard let version = WhatsNewVersion(release.version) else { return nil }
            return ParsedRelease(release: release, version: version)
        }
        self.installed = installed
        self.installedString = parsedCatalogue.first(where: { $0.version == installed })?.release.version
            ?? Self.canonicalString(for: installed)
        self.lastSeen = lastSeen
        self.hasAnyFluxPref = hasAnyFluxPref
    }

    public func autoDecision() -> AutoDecision {
        let effective: WhatsNewVersion
        if let lastSeen {
            guard let parsed = WhatsNewVersion(lastSeen) else {
                return .silentSet(version: installedString)
            }
            effective = parsed
        } else if hasAnyFluxPref {
            guard let seed = WhatsNewVersion("1.0") else { return .skip }
            effective = seed
        } else {
            return .silentSet(version: installedString)
        }

        guard installed > effective else { return .skip }

        let inRange = parsedCatalogue.filter { entry in
            entry.version > effective &&
                entry.version <= installed &&
                !entry.release.highlights.isEmpty
        }

        guard !inRange.isEmpty else {
            return .silentSet(version: installedString)
        }

        let sorted = inRange.sorted { $0.version > $1.version }
        return .present(releases: sorted.map(\.release))
    }

    public func manualLatest() -> WhatsNewRelease? {
        let eligible = parsedCatalogue.filter { entry in
            entry.version <= installed && !entry.release.highlights.isEmpty
        }
        return eligible.max { $0.version < $1.version }?.release
    }

    private static func canonicalString(for version: WhatsNewVersion) -> String {
        version.components.map(String.init).joined(separator: ".")
    }
}
