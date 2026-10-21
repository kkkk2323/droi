import DroidProtocol
import Synchronization
import Testing

@testable import DroiKit

private func skill(
    _ name: String = "review", location: SkillLocation = .builtin, enabled: Bool? = true,
    disabledBy: SkillDisabledBy? = nil
) -> SkillInfo {
    SkillInfo(name: name, location: location, filePath: "/skills/\(name)/SKILL.md", enabled: enabled, disabledBy: disabledBy)
}

private func off(_ levels: [SettingsLevel], location: SkillLocation = .builtin) -> SkillInfo {
    skill(
        location: location, enabled: false,
        disabledBy: .ledger(SkillDisabledByLedger(kind: "ledger", sources: levels.map { .init(level: $0) })))
}

private let frontmatter = try! (["kind": "frontmatter"] as JSONValue).decode(SkillDisabledBy.self)

@Suite struct SkillsTests {
    @Test func aBuiltInSkillSwitchesForTheUser() {
        #expect(skillSwitch(skill(), projectAvailable: true) == SkillSwitch(on: true, level: .user))
        #expect(skillSwitch(skill(), projectAvailable: false) == SkillSwitch(on: true, level: .user))
    }

    @Test func aProjectSkillSwitchesForItsProjectAndNotWithoutOne() {
        #expect(skillSwitch(skill(location: .project), projectAvailable: true) == SkillSwitch(on: true, level: .project))
        #expect(skillSwitch(skill(location: .project), projectAvailable: false) == nil)
    }

    @Test func aSkillTurnedOffAtTheLevelTheSwitchWritesComesBackFromIt() {
        #expect(skillSwitch(off([.user]), projectAvailable: true) == SkillSwitch(on: false, level: .user))
        #expect(
            skillSwitch(off([.project], location: .project), projectAvailable: true) == SkillSwitch(on: false, level: .project))
    }

    @Test func aSkillTurnedOffElsewhereIsReadOnly() {
        #expect(skillSwitch(off([.project]), projectAvailable: true) == nil)
        #expect(skillSwitch(off([.user, .project]), projectAvailable: true) == nil)
        #expect(skillSwitch(off([.org]), projectAvailable: true) == nil)
        #expect(skillSwitch(skill(enabled: false, disabledBy: frontmatter), projectAvailable: true) == nil)
        #expect(skillSwitch(skill(location: .personal), projectAvailable: true) == nil)
    }

    @Test func namesWhoTurnedTheSkillOff() {
        #expect(disabledLabel(off([.org])) == "Disabled by organization")
        #expect(disabledLabel(off([.project])) == "Disabled for this project")
        #expect(disabledLabel(off([.user])) == "Disabled")
        #expect(disabledLabel(off([.user, .project])) == "Disabled")
        #expect(disabledLabel(skill(enabled: false, disabledBy: frontmatter)) == "Disabled in its file")
    }

    @Test func groupsByLocationInAFixedOrderNamesSortedInside() {
        let groups = groupSkills([
            skill("zeta"), skill("alpha"), skill("mine", location: .personal), skill("ours", location: .project),
        ])
        #expect(groups.map(\.location) == [.project, .personal, .builtin])
        #expect(groups.map { $0.skills.map(\.name) } == [["ours"], ["mine"], ["alpha", "zeta"]])
        #expect(SkillLocation.builtin.label == "Built-in")
    }
}

private func entry(
    name: String = "linear", description: String = "Issue tracking", type: MCPServerType = .http,
    url: String? = "https://mcp.linear.app/mcp", command: String? = nil, args: [String]? = nil
) -> MCPRegistryServer {
    MCPRegistryServer(name: name, description: description, type: type, url: url, command: command, args: args)
}

private func server(
    _ name: String = "files", source: SettingsLevel = .user, requiresAuth: Bool? = nil, hasAuthTokens: Bool? = nil
) -> MCPServerStatusInfo {
    MCPServerStatusInfo(
        name: name, status: .connected, source: source, isManaged: false, serverType: .stdio,
        hasAuthTokens: hasAuthTokens, requiresAuth: requiresAuth)
}

@Suite struct McpTests {
    @Test func anEntryWithAnUpperCasePlaceholderNeedsSettingUpFirst() {
        #expect(!needsSetup(entry()))
        #expect(!needsSetup(entry(type: .stdio, url: nil, command: "npx", args: ["-y", "snyk@latest", "mcp", "-t", "stdio"])))
        #expect(
            needsSetup(
                entry(type: .stdio, url: nil, command: "npx", args: ["-y", "@adyen/mcp", "--adyenApiKey=ADYEN_API_KEY", "--env=TEST"])))
        #expect(needsSetup(entry(type: .stdio, url: nil, command: "x", args: ["YOUR_TOKEN"])))
    }

    @Test func searchMatchesEveryWordAndSkipsWhatIsAdded() {
        let all = [
            entry(), entry(name: "sentry", description: "Error tracking and performance monitoring"),
            entry(name: "notion", description: "Notes and docs"),
        ]
        #expect(filterRegistry(all, query: "", taken: []).map(\.name) == ["linear", "sentry", "notion"])
        #expect(filterRegistry(all, query: "TRACKING", taken: []).map(\.name) == ["linear", "sentry"])
        #expect(filterRegistry(all, query: "error track", taken: []).map(\.name) == ["sentry"])
        #expect(filterRegistry(all, query: "tracking", taken: ["linear"]).map(\.name) == ["sentry"])
    }

    @Test func aStdioServerNeedsANameAndACommand() throws {
        #expect((try? parseServerForm(ServerForm(command: "npx x")).get()) == nil)
        #expect((try? parseServerForm(ServerForm(name: "bad name", command: "x")).get()) == nil)
        #expect(
            try parseServerForm(ServerForm(name: "fs", command: "npx", args: " -y  server ")).get()
                == McpServerInput(name: "fs", type: .stdio, command: "npx", args: ["-y", "server"]))
    }

    @Test func anHttpServerNeedsAURLAndHeadersAreNameValueLines() throws {
        #expect((try? parseServerForm(ServerForm(name: "api", type: .http, url: "localhost")).get()) == nil)
        #expect(
            try parseServerForm(
                ServerForm(
                    name: "api", type: .http, url: "https://mcp.example.com/mcp",
                    headers: "Authorization: Bearer x\n\nX-Team: a: b")
            ).get()
                == McpServerInput(
                    name: "api", type: .http, url: "https://mcp.example.com/mcp",
                    headers: ["Authorization": "Bearer x", "X-Team": "a: b"]))
        #expect(
            (try? parseServerForm(ServerForm(name: "api", type: .sse, url: "https://x.y/sse", headers: "nonsense")).get()) == nil)
    }

    @Test func aRegistryEntryFillsTheForm() {
        let form = ServerForm(registry: entry(name: "github", description: "GitHub", url: "https://api.githubcopilot.com/mcp/"))
        #expect(form == ServerForm(name: "github", type: .http, url: "https://api.githubcopilot.com/mcp/"))
    }

    @Test func theOrganizationsServersAreReadOnlyAndLast() {
        let org = server("a-org", source: .org)
        let project = server("z-proj", source: .project)
        let mine = server("m-mine")
        #expect(sortServers([org, project, mine]).map(\.name) == ["m-mine", "z-proj", "a-org"])
        #expect(isReadOnly(org))
        #expect(!canRemove(project))
        #expect(canRemove(mine))
    }

    @Test func droisMemoryServerIsReadOnlyAndNeverRemovedFromHere() {
        let memory = server("droi-memory")
        #expect(isDroiMemory(memory))
        #expect(isReadOnly(memory))
        #expect(!canRemove(memory))
        #expect(!isDroiMemory(server("memory")))
    }

    @Test func aServerWantsSigningInWhenItNeedsAuthAndHasNoTokens() {
        #expect(needsSignIn(server(requiresAuth: true)))
        #expect(!needsSignIn(server(requiresAuth: true, hasAuthTokens: true)))
        #expect(!needsSignIn(server()))
        #expect(MCPServerStatus.connected.label == "Connected")
    }
}

/// Records what archiving asks for, in order.
private final class Recorder: ArchiveActions {
    let calls = Mutex<[String]>([])
    let refuseArchive: Bool

    init(refuseArchive: Bool = false) {
        self.refuseArchive = refuseArchive
    }

    func archiveSession(_ sessionId: String) async throws {
        if refuseArchive { throw GatewayError(message: "CONFLICT") }
        calls.withLock { $0.append("archive \(sessionId)") }
    }

    func unarchiveSession(_ sessionId: String) async throws { calls.withLock { $0.append("unarchive \(sessionId)") } }
    func trashScratchWorkspace(_ path: String) async throws { calls.withLock { $0.append("trash \(path)") } }
    func restoreScratchWorkspace(_ path: String) async throws { calls.withLock { $0.append("restore \(path)") } }
}

@Suite struct ArchiveTests {
    let folder = "/u/.droi/chats/2026-09-26-abcdef"
    let scratch = [SessionTag(name: scratchTag)]
    var first: SessionSummary { summary("first", cwd: folder, tags: scratch) }
    var second: SessionSummary { summary("second", cwd: folder, tags: scratch, parentId: "first") }
    var latest: SessionSummary { summary("latest", cwd: folder, tags: scratch, parentId: "second") }

    @Test func aWorkspaceSessionIsArchivedOnItsOwn() async throws {
        let recorder = Recorder()
        let session = summary("p", cwd: "/w/app")
        try await archiveConversation(recorder, listed: [session], session: session)
        try await unarchiveConversation(recorder, listed: [session], session: session)
        #expect(recorder.calls.withLock { $0 } == ["archive p", "unarchive p"])
    }

    @Test func aScratchSessionTakesItsWholeChainAlongThenItsFolderGoesToTheTrash() async throws {
        let recorder = Recorder()
        try await archiveConversation(recorder, listed: [first, second, latest], session: latest)
        #expect(recorder.calls.withLock { $0 } == ["archive latest", "archive second", "archive first", "trash \(folder)"])
    }

    @Test func unarchivingRecreatesTheFolderBeforeTheSessionsComeBack() async throws {
        let recorder = Recorder()
        try await unarchiveConversation(recorder, listed: [first, second, latest], session: latest)
        #expect(
            recorder.calls.withLock { $0 } == [
                "restore \(folder)", "unarchive latest", "unarchive second", "unarchive first",
            ])
    }

    @Test func aRefusedArchiveKeepsTheFolder() async {
        let recorder = Recorder(refuseArchive: true)
        await #expect(throws: GatewayError(message: "CONFLICT")) {
            try await archiveConversation(recorder, listed: [latest], session: latest)
        }
        #expect(recorder.calls.withLock { $0 }.isEmpty)
    }
}
