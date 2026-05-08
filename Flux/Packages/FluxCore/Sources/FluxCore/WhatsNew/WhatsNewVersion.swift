import Foundation

public struct WhatsNewVersion: Comparable, Hashable, Sendable {
    public let components: [Int]

    public init?(_ string: String) {
        guard !string.isEmpty else { return nil }
        let parts = string.split(separator: ".", omittingEmptySubsequences: false)
        var parsed: [Int] = []
        parsed.reserveCapacity(parts.count)
        for part in parts {
            guard let value = Int(part) else { return nil }
            parsed.append(value)
        }
        guard !parsed.isEmpty else { return nil }
        self.components = parsed
    }

    public static func < (lhs: Self, rhs: Self) -> Bool {
        let count = max(lhs.components.count, rhs.components.count)
        for index in 0..<count {
            let lhsValue = index < lhs.components.count ? lhs.components[index] : 0
            let rhsValue = index < rhs.components.count ? rhs.components[index] : 0
            if lhsValue != rhsValue { return lhsValue < rhsValue }
        }
        return false
    }

    // Equality and hashing both treat trailing zeros as missing components, so
    // `1.2 == 1.2.0` and they hash to the same bucket.
    public static func == (lhs: Self, rhs: Self) -> Bool {
        Self.trim(lhs.components) == Self.trim(rhs.components)
    }

    public func hash(into hasher: inout Hasher) {
        hasher.combine(Self.trim(components))
    }

    private static func trim(_ components: [Int]) -> [Int] {
        var result = components
        while result.count > 1, result.last == 0 {
            result.removeLast()
        }
        return result
    }
}
