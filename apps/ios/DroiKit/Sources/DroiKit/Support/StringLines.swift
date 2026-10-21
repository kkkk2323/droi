import Foundation

/// Text helpers that split the way the TypeScript Clients do: on "\n" alone,
/// so "\r\n" (one Character in Swift) still splits, keeping its "\r".
extension String {
    func lines() -> [String] {
        unicodeScalars.split(separator: "\n", omittingEmptySubsequences: false).map { String($0) }
    }

    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }

    var isBlank: Bool { trimmed.isEmpty }

    /// The UTF-16 length, as JavaScript counts a string.
    var jsLength: Int { utf16.count }

    /// Counts the non-overlapping occurrences of `token`.
    func occurrences(of token: String) -> Int {
        var count = 0
        var searchRange = startIndex..<endIndex
        while let found = range(of: token, range: searchRange) {
            count += 1
            searchRange = found.upperBound..<endIndex
        }
        return count
    }
}
