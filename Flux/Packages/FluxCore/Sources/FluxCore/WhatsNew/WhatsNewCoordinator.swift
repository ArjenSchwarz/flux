import Foundation
import os

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

    /// The canonical version string that `silentSet` will return and that the
    /// dismiss-write site should persist. Single source of truth so auto and
    /// manual call sites do not disagree on `1.1` vs `1.1.0` spelling.
    public var canonicalInstalledVersion: String { installedString }

    /// Build a coordinator from the running app's bundle and shared defaults.
    ///
    /// Returns `nil` if `CFBundleShortVersionString` is missing or unparseable.
    /// Call sites should treat `nil` as "skip the What's New flow this launch"
    /// — a misconfigured bundle is the only way to hit it, and there is no
    /// recovery path the caller can take. A debug log line is emitted so a
    /// silent skip in the field can be traced via Console.app rather than
    /// looking like the feature stopped working.
    public static func forCurrentInstall(
        bundle: Bundle = .main,
        defaults: UserDefaults = .fluxAppGroup,
        catalogue: [WhatsNewRelease] = WhatsNewCatalogue.releases
    ) -> WhatsNewCoordinator? {
        guard let raw = bundle.infoDictionary?["CFBundleShortVersionString"] as? String else {
            Self.logger.debug("forCurrentInstall: CFBundleShortVersionString missing — skipping")
            return nil
        }
        guard let installed = WhatsNewVersion(raw) else {
            Self.logger.debug("forCurrentInstall: CFBundleShortVersionString \(raw, privacy: .public) unparseable — skipping")
            return nil
        }
        return WhatsNewCoordinator(
            catalogue: catalogue,
            installed: installed,
            lastSeen: defaults.lastSeenWhatsNewVersion,
            hasAnyFluxPref: defaults.hasAnyFluxPreferenceWritten
        )
    }

    private static let logger = Logger(subsystem: "eu.arjen.flux", category: "whats-new")

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
