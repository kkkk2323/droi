// Skills as the Daemon lists them for a Session, and the one switch a Client
// may offer on each. The Daemon owns the state: a switch writes
// `disabledSkills` to `~/.factory/settings.json` (user level, every project)
// or to the Workspace's `.factory/settings.json` (project level); the droid
// CLI and the Factory App read the same files. Which skills get a switch
// follows the Factory App: a project skill and a built-in one do, a personal
// skill (`~/.factory/skills`) is managed by editing its files, and one the
// organization turned off is read-only. Where the switch writes is one rule:
// a project skill to its project, a built-in one for the user.
import DroidProtocol
import Foundation

/// Where a switch is written.
public enum SkillLevel: String, Sendable, Hashable {
    case user, project
}

public struct SkillSwitch: Sendable, Hashable {
    /// Whether the skill is on now; flipping writes `disabled: on` at `level`.
    public var on: Bool
    public var level: SkillLevel

    public init(on: Bool, level: SkillLevel) {
        self.on = on
        self.level = level
    }
}

public let skillLocationOrder: [SkillLocation] = [.project, .personal, .builtin, .automation]

extension SkillLocation {
    public var label: String {
        switch self {
        case .project: "Project"
        case .personal: "Personal"
        case .builtin: "Built-in"
        case .automation: "Automation"
        default: rawValue
        }
    }
}

private func disabledLevels(_ skill: SkillInfo) -> [String] {
    if case .ledger(let ledger)? = skill.disabledBy { return ledger.sources.map(\.level.rawValue) }
    return []
}

/// The switch for a skill, or nil when nothing here can change it: a personal
/// skill, one with no Workspace to write a project switch into, one turned off
/// in its own file, and one turned off at a level this switch does not write
/// (the organization's, or the other of user and project).
public func skillSwitch(_ skill: SkillInfo, projectAvailable: Bool) -> SkillSwitch? {
    let level: SkillLevel? =
        switch skill.location {
        case .project: projectAvailable ? .project : nil
        case .builtin: .user
        default: nil
        }
    guard let level else { return nil }
    if skill.enabled != false { return SkillSwitch(on: true, level: level) }
    let levels = disabledLevels(skill)
    if levels.isEmpty || levels.contains(where: { $0 != level.rawValue }) { return nil }
    return SkillSwitch(on: false, level: level)
}

/// Why a skill is off, for its badge.
public func disabledLabel(_ skill: SkillInfo) -> String {
    let levels = disabledLevels(skill)
    if levels.contains("org") { return "Disabled by organization" }
    if case .frontmatter? = skill.disabledBy { return "Disabled in its file" }
    if !levels.isEmpty && levels.allSatisfy({ $0 == "project" }) { return "Disabled for this project" }
    return "Disabled"
}

/// The skills grouped by where they live, in a fixed order, each group sorted by name.
public func groupSkills(_ skills: [SkillInfo]) -> [(location: SkillLocation, skills: [SkillInfo])] {
    var groups: [SkillLocation: [SkillInfo]] = [:]
    for skill in skills { groups[skill.location, default: []].append(skill) }
    func order(_ location: SkillLocation) -> Int {
        skillLocationOrder.firstIndex(of: location) ?? skillLocationOrder.count
    }
    return groups.keys
        .sorted { a, b in
            order(a) != order(b) ? order(a) < order(b) : a.rawValue.localizedCompare(b.rawValue) == .orderedAscending
        }
        .map { location in
            (location, groups[location]!.sorted { $0.name.localizedCompare($1.name) == .orderedAscending })
        }
}
