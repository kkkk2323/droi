// Messages the Daemon is holding for a running Session, as the composer's
// shelf shows them.
//
// Cancelling a turn makes the Daemon drop what it held; the store keeps those
// messages on this Client as `local_paused_after_esc`, so nothing typed is
// lost. A paused message exists only here: it is removed locally and goes out
// again only when sent from the composer. Clearing a client-only message is
// the store's own; a Daemon-held one needs `resolve_queued_user_message`
// with `delete`.
import DroidProtocol

public struct QueuedMessageKind: RawRepresentable, Sendable, Hashable, Codable {
    public let rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public static let daemonQueuedDiscardable = QueuedMessageKind(rawValue: "daemon_queued_discardable")
    public static let daemonQueuedEndOfLoop = QueuedMessageKind(rawValue: "daemon_queued_end_of_loop")
    public static let localDeferredAfterEsc = QueuedMessageKind(rawValue: "local_deferred_after_esc")
    public static let localPausedAfterEsc = QueuedMessageKind(rawValue: "local_paused_after_esc")

    /// Kinds the Daemon holds; the rest live on this Client only.
    public var isHeldByDaemon: Bool { self == .daemonQueuedDiscardable || self == .daemonQueuedEndOfLoop }
    /// Kept back on this Client since a cancelled turn; sending it again is up to the user.
    public var isPaused: Bool { self == .localPausedAfterEsc }
    /// Handed to the running turn rather than waiting for it to end.
    public var isSteering: Bool { self == .daemonQueuedDiscardable || self == .localDeferredAfterEsc }

    /// The shelf's word for it.
    public var label: String { isPaused ? "Paused" : isSteering ? "Next" : "Queued" }
}

/// Up to this many queued messages show as a list; more fold into one row.
public let queuedUnfolded = 2

/// Plain text of a queued message, for the shelf.
public func queuedText(_ content: [ContentBlock]) -> String {
    content.compactMap { block -> String? in
        switch block {
        case .text(let text): text.text.isEmpty ? nil : text.text
        case .image: "[image]"
        default: nil
        }
    }.joined(separator: " ")
}

/// What a queued message holds, in the composer's terms.
public struct QueuedContent: Sendable, Hashable {
    public var text: String
    public var images: [ImageAttachment]

    public init(text: String, images: [ImageAttachment]) {
        self.text = text
        self.images = images
    }
}

public func queuedContent(_ content: [ContentBlock]) -> QueuedContent {
    let text = content.compactMap { block -> String? in
        if case .text(let text) = block, !text.text.isEmpty { return text.text }
        return nil
    }.joined(separator: "\n")
    let images = content.compactMap { block -> ImageAttachment? in
        guard case .image(let image) = block, image.source.type == "base64",
            let mediaType = ImageMediaType(rawValue: image.source.mediaType.rawValue)
        else { return nil }
        return ImageAttachment(name: "image", mediaType: mediaType, data: image.source.data)
    }
    return QueuedContent(text: text, images: images)
}

/// What the composer (or, while a Prompt stands in for it, the stored draft)
/// holds after a queued message is taken back into it: its text first, then
/// what was typed; its images first, then the others.
public func insertTaken(_ taken: QueuedContent, into draft: QueuedContent) -> QueuedContent {
    let text = draft.text.isBlank ? taken.text : "\(taken.text)\n\(draft.text)"
    return QueuedContent(text: text, images: taken.images + draft.images)
}
