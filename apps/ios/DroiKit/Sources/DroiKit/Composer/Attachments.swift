// Images attached in the composer, held as base64 until the message goes out
// (the Daemon takes image blocks inline, not by path). Reading and shrinking
// a picked image is up to the app.
import Foundation

public enum ImageMediaType: String, Sendable, Hashable, CaseIterable, Codable {
    case jpeg = "image/jpeg"
    case png = "image/png"
    case gif = "image/gif"
    case webp = "image/webp"
}

public struct ImageAttachment: Sendable, Hashable, Identifiable {
    public var id: String
    public var name: String
    public var mediaType: ImageMediaType
    /// Base64 without the data-URL prefix, as the Daemon wants it.
    public var data: String

    public init(id: String = newID(), name: String, mediaType: ImageMediaType, data: String) {
        self.id = id
        self.name = name
        self.mediaType = mediaType
        self.data = data
    }

    public var dataURL: String { "data:\(mediaType.rawValue);base64,\(data)" }
}

/// Longest edge the model can use; Anthropic downsamples anything larger anyway.
public let maxImageEdge = 1568
/// Re-encoded as JPEG at this quality. The Daemon estimates context as
/// characters / 4 and counts base64 image data at full length, so a 2 MB
/// screenshot reads as ~700k tokens and trips compaction; kept small it does not.
public let imageJPEGQuality = 0.85
/// Above this the desktop re-encodes an image; the phone always does.
public let maxImageBytes = 300_000

/// Scale (w, h) down to fit `max` on the longest edge, never up.
public func fitWithin(width: Int, height: Int, max: Int) -> (width: Int, height: Int) {
    let longest = Swift.max(width, height)
    if longest <= max { return (width, height) }
    let scale = Double(max) / Double(longest)
    return (
        Swift.max(1, Int((Double(width) * scale).rounded())), Swift.max(1, Int((Double(height) * scale).rounded()))
    )
}

/// An RFC 4122 v4 id in lower case, as the Daemon and the other Clients write them.
public func newID() -> String {
    UUID().uuidString.lowercased()
}
