// When a Session calls for an alert. Each Client decides what an alert does
// (a sound, a notification, a haptic); every Client marks the Session unread.
import DroidProtocol

public enum AlertEvent: String, Sendable, Hashable {
    case completion
    case awaitingInput = "awaiting-input"
}

/// Follows each Session's working state and says when it calls for an alert,
/// the way Factory App does: once when it starts waiting for an answer, and
/// once when it goes idle after doing something.
public struct AlertTracker: Sendable {
    private var active: Set<String> = []
    private var awaiting: Set<String> = []

    public init() {}

    public mutating func update(_ sessionId: String, workingState: DroidWorkingState) -> AlertEvent? {
        if workingState == .idle {
            awaiting.remove(sessionId)
            return active.remove(sessionId) != nil ? .completion : nil
        }
        active.insert(sessionId)
        guard workingState == .waitingForToolConfirmation else { return nil }
        if awaiting.contains(sessionId) { return nil }
        awaiting.insert(sessionId)
        return .awaitingInput
    }

    /// The Session dropped out of this Client (a reconnect, a close); its next state is no news.
    public mutating func forget(_ sessionId: String) {
        active.remove(sessionId)
        awaiting.remove(sessionId)
    }
}

/// The sound or haptic an alert plays; the sound files are named after it.
public enum AlertSound: String, Sendable, Hashable {
    case finished
    case needsInput = "needs-input"
}

/// The Phone App's alert switches: a sound and a haptic per event, each on or
/// off, kept on the phone. Anything missing or malformed reads as on.
public struct AlertSwitches: Codable, Sendable, Hashable {
    public var finishedSound = true
    public var finishedHaptic = true
    public var needsInputSound = true
    public var needsInputHaptic = true

    public init(
        finishedSound: Bool = true, finishedHaptic: Bool = true, needsInputSound: Bool = true,
        needsInputHaptic: Bool = true
    ) {
        self.finishedSound = finishedSound
        self.finishedHaptic = finishedHaptic
        self.needsInputSound = needsInputSound
        self.needsInputHaptic = needsInputHaptic
    }

    public init(from decoder: any Decoder) throws {
        let value = try? JSONValue(from: decoder)
        func flag(_ key: String) -> Bool { value?[key]?.boolValue ?? true }
        self.init(
            finishedSound: flag("finishedSound"),
            finishedHaptic: flag("finishedHaptic"),
            needsInputSound: flag("needsInputSound"),
            needsInputHaptic: flag("needsInputHaptic"))
    }
}

/// What an alert plays: nil where its switch is off.
public func feedbackFor(_ switches: AlertSwitches, _ alert: AlertEvent) -> (sound: AlertSound?, haptic: AlertSound?) {
    switch alert {
    case .completion:
        (switches.finishedSound ? .finished : nil, switches.finishedHaptic ? .finished : nil)
    case .awaitingInput:
        (switches.needsInputSound ? .needsInput : nil, switches.needsInputHaptic ? .needsInput : nil)
    }
}
