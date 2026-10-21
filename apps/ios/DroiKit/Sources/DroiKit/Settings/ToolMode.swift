// Whether a Session's model calls tools directly or from a Script program.
// The Daemon fixes the mode when it creates the Session and offers no way to
// change it later, so it is chosen only for new Sessions. Without a choice,
// droid decides: `toolExecutionMode` in ~/.factory/settings.json, else
// Factory's per-model list, else direct.
import DroidProtocol

public enum ToolMode: String, Sendable, Hashable, CaseIterable, Codable {
    case directOnly = "direct_only"
    case scriptOnly = "script_only"

    public var label: String {
        switch self {
        case .directOnly: "Direct"
        case .scriptOnly: "Script"
        }
    }

    /// The modes droid knows; droid 0.244 no longer has `direct_and_script`, which Droi once offered.
    public init?(_ mode: ToolExecutionMode?) {
        guard let mode, let known = ToolMode(rawValue: mode.rawValue) else { return nil }
        self = known
    }

    public var executionMode: ToolExecutionMode { ToolExecutionMode(rawValue: rawValue) }
}

/// A new Session page's mode: the one it shows, and the one it asks the
/// Daemon for. The page's own choice wins, then this device's default; when
/// droid decides, the page shows what the Daemon gave the draft.
public func newSessionToolMode(choice: ToolMode?, fallback: ToolMode?, draft: ToolMode?)
    -> (shown: ToolMode?, requested: ToolMode?)
{
    let requested = choice ?? fallback
    return (requested ?? draft, requested)
}
