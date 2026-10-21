import DroidProtocol
import Foundation
import Testing

@testable import DroiKit

private let scratch = [SessionTag(name: scratchTag)]

private func groupIds(_ groups: [WorkspaceGroup]) -> [[String]] {
    groups.map { $0.sessions.map(\.sessionId) }
}

@Suite struct GroupByWorkspaceTests {
    @Test func groupsByRepoRootFallingBackToCwdNewestFirst() {
        let groups = groupByWorkspace([
            summary("a", cwd: "/w/alpha", updatedAt: 10),
            summary("b", cwd: "/w/beta/sub", repoRoot: "/w/beta", updatedAt: 30),
            summary("c", cwd: "/w/alpha", updatedAt: 20),
            summary("d", cwd: "/w/beta", updatedAt: 5),
        ])
        #expect(groups.map(\.label) == ["beta", "alpha"])
        #expect(groupIds(groups) == [["b", "d"], ["c", "a"]])
    }

    @Test func aWorktreeSessionGroupsUnderItsMainCheckout() {
        let tree = SessionWorktree(path: "/wt/ab12cd34/app", branch: "droid/fix-it", lifecycle: .ephemeral, repoRoot: "/w/app")
        let groups = groupByWorkspace([
            summary("a", cwd: "/w/app", updatedAt: 10),
            summary("b", cwd: tree.path, repoRoot: "/w/app", worktree: tree, updatedAt: 20),
            summary("c", cwd: tree.path, worktree: tree, updatedAt: 30),
        ])
        #expect(groups.map(\.path) == ["/w/app"])
        #expect(groupIds(groups) == [["c", "b", "a"]])
    }

    @Test func workspacesWithMoreConversationsComeFirstEmptyOnesLast() {
        let groups = groupByWorkspace([
            summary(cwd: "/w/busy", updatedAt: 10, messagesCount: 4),
            summary(cwd: "/w/busy", updatedAt: 11, messagesCount: 9),
            summary(cwd: "/w/once", updatedAt: 50, messagesCount: 800),
            summary(cwd: "/w/empty", updatedAt: 90, messagesCount: 0),
            summary(cwd: "/w/empty", updatedAt: 91, messagesCount: 0),
        ])
        #expect(groups.map(\.label) == ["busy", "once", "empty"])
    }

    @Test func sessionsWithoutAnyPathLandInAnUnknownGroup() {
        #expect(groupByWorkspace([summary("x")])[0].label == "Unknown workspace")
    }

    @Test func pinnedWorkspacesAndSessionsComeFirst() {
        let groups = groupByWorkspace(
            [
                summary("a", cwd: "/w/alpha", updatedAt: 10), summary("b", cwd: "/w/beta", updatedAt: 30),
                summary("c", cwd: "/w/alpha", updatedAt: 20), summary("g", cwd: "/w/gamma", updatedAt: 40),
            ],
            pins: Pins(workspaces: ["/w/alpha"], sessions: ["a"]))
        #expect(groups.map(\.label) == ["alpha", "gamma", "beta"])
        #expect(groups[0].sessions.map(\.sessionId) == ["a", "c"])
    }

    @Test func pinnedApartSessionsLeaveTheirGroupsForOneOfTheirOwn() {
        let groups = groupByWorkspace(
            [
                summary("a", cwd: "/w/alpha", updatedAt: 10), summary("b", cwd: "/w/beta", updatedAt: 30),
                summary("c", cwd: "/w/alpha", updatedAt: 20), summary("g", cwd: "/w/gamma", updatedAt: 40),
            ],
            pins: Pins(workspaces: ["/w/alpha"], sessions: ["a", "b"]), pinnedApart: true)
        #expect(groups.map(\.key) == [pinnedSessionsGroupKey, "/w/alpha", "/w/gamma"])
        #expect(groupIds(groups) == [["b", "a"], ["c"], ["g"]])
    }
}

@Suite struct SortingTests {
    let sessions = [
        summary("b1", cwd: "/w/beta", updatedAt: 100), summary("b2", cwd: "/w/beta", updatedAt: 200),
        summary("a1", cwd: "/w/alpha", updatedAt: 900), summary("c1", cwd: "/w/gamma", updatedAt: 500),
    ]

    private func labels(_ order: ListOrder) -> [String] {
        groupByWorkspace(sessions, order: order).map(\.label)
    }

    @Test func byMostSessionsRecentlyActiveNameOrByHand() {
        #expect(labels(ListOrder()) == ["beta", "alpha", "gamma"])
        #expect(labels(ListOrder(workspaces: .recent)) == ["alpha", "gamma", "beta"])
        #expect(labels(ListOrder(workspaces: .name)) == ["alpha", "beta", "gamma"])
        // A Workspace not placed yet follows the placed ones, in the default order.
        #expect(labels(ListOrder(workspaces: .manual, manual: ["/w/gamma"])) == ["gamma", "beta", "alpha"])
    }

    @Test func pinnedWorkspacesStayFirstWhateverTheOrder() {
        let pinned = groupByWorkspace(
            sessions, pins: Pins(workspaces: ["/w/gamma"]), order: ListOrder(workspaces: .name))
        #expect(pinned.map(\.label) == ["gamma", "alpha", "beta"])
    }

    @Test func sessionsNewestFirstOrByWhenTheyWereFirstSeen() {
        func beta(_ order: ListOrder) -> [String] {
            groupByWorkspace(sessions, order: order).first { $0.label == "beta" }!.sessions.map(\.sessionId)
        }
        #expect(beta(ListOrder()) == ["b2", "b1"])
        #expect(beta(ListOrder(sessions: .created, firstSeen: ["b1": 5_000, "b2": 1_000])) == ["b1", "b2"])
    }

    @Test func aMovePlacesOneWorkspaceBeforeOrAfterAnother() {
        let shown = ["/w/a", "/w/b", "/w/c"]
        #expect(moveWorkspace(shown, key: "/w/c", target: "/w/a", place: .before) == ["/w/c", "/w/a", "/w/b"])
        #expect(moveWorkspace(shown, key: "/w/a", target: "/w/b", place: .after) == ["/w/b", "/w/a", "/w/c"])
        #expect(moveWorkspace(shown, key: "/w/a", target: "/w/a", place: .after) == shown)
    }

    @Test func noteFirstSeenAddsOnlyNewSessionsAtTheEarlierOfNowAndTheirLastChange() {
        let known: [String: Double] = ["old": 1]
        #expect(noteFirstSeen(known, [summary("old")], now: 10_000) == known)
        #expect(
            noteFirstSeen(known, [summary("fresh", updatedAt: 50), summary("later", updatedAt: 99)], now: 60_000)
                == ["old": 1, "fresh": 50_000, "later": 60_000])
    }
}

@Suite struct ScratchGroupingTests {
    @Test func everyScratchSessionSitsInOneRecentsGroupLast() {
        let groups = groupByWorkspace([
            summary("s1", cwd: "/u/.droi/chats/2026-09-25-aaaaaa", updatedAt: 50, messagesCount: 9, tags: scratch),
            summary("p", cwd: "/w/alpha", updatedAt: 10),
            summary("s2", cwd: "/u/.droi/chats/2026-09-26-bbbbbb", updatedAt: 60, messagesCount: 9, tags: scratch),
        ])
        #expect(groups.map(\.label) == ["alpha", "Recents"])
        #expect(groups.map(\.scratch) == [false, true])
        #expect(groups[1].sessions.map(\.sessionId) == ["s2", "s1"])
    }

    @Test func aSessionInAScratchFolderWithoutTheTagStillGoesToRecents() {
        let groups = groupByWorkspace([
            summary("lost", cwd: "/u/.droi/chats/2026-10-02-5c4802", updatedAt: 70),
            summary("p", cwd: "/w/alpha", updatedAt: 10),
            summary("dated", cwd: "/w/2026-10-02-notes", updatedAt: 5),
        ])
        #expect(groups.map(\.label) == ["alpha", "2026-10-02-notes", "Recents"])
        #expect(groupIds(groups) == [["p"], ["dated"], ["lost"]])
    }

    @Test func recentsStaysLastEvenWhenPinned() {
        let groups = groupByWorkspace(
            [
                summary("s1", cwd: "/c/a", updatedAt: 50, tags: scratch),
                summary("s2", cwd: "/c/b", updatedAt: 60, tags: scratch),
                summary("p", cwd: "/w/alpha", updatedAt: 10),
            ],
            pins: Pins(workspaces: [recentsGroupKey], sessions: ["s1"]), pinnedApart: true)
        #expect(groups.map(\.label) == ["Pinned sessions", "alpha", "Recents"])
        #expect(groupIds(groups) == [["s1"], ["p"], ["s2"]])
    }
}

@Suite struct SessionListTests {
    @Test func workspaceLabelUsesTheLastPathSegment() {
        #expect(workspaceLabel("/Users/me/dev/droi") == "droi")
        #expect(workspaceLabel("/Users/me/dev/droi/") == "droi")
        #expect(workspaceLabel("C:\\code\\thing") == "thing")
    }

    let now = 1_800_000_000_000.0
    let day = 24 * 60 * 60.0
    var list: [SessionSummary] {
        [
            summary("today", updatedAt: now / 1000 - day),
            summary("lastWeek", updatedAt: now / 1000 - 7 * day),
            summary("lastMonth", updatedAt: now / 1000 - 30 * day),
        ]
    }

    @Test func showsRecentSessionsAndCountsTheRestAsHidden() {
        let shown = visibleSessions(list, revealed: 0, now: now)
        #expect(shown.visible == [list[0]])
        #expect(shown.hidden == 2)
        #expect(recentWindowMs == 3 * day * 1000)
    }

    @Test func revealsOlderSessionsNewestFirst() {
        let one = visibleSessions(list, revealed: 1, now: now)
        #expect(one.visible == [list[0], list[1]])
        #expect(one.hidden == 1)
        #expect(visibleSessions(list, revealed: 30, now: now).hidden == 0)
    }

    @Test func aPinnedSessionAlwaysShowsAndIsNotCountedAsHidden() {
        let shown = visibleSessions(list, revealed: 0, now: now, pinned: ["lastMonth"])
        #expect(shown.visible == [list[0], list[2]])
        #expect(shown.hidden == 1)
    }

    @Test func aListedSessionReadsItsLinksAndWorktree() throws {
        let info = try ([
            "sessionId": "s", "updatedAt": 5, "title": "  ", "cwd": "/wt/app", "messagesCount": 3,
            "tags": [["name": "droi.continues", "metadata": ["parent": "p"]]],
            "worktree": ["path": "/wt/app", "branch": "droid/x", "lifecycle": "persistent", "repoRoot": "/w/app"],
            "callingSessionId": "caller",
        ] as JSONValue).decode(DaemonAvailableSessionInfo.self)
        let listed = SessionSummary(listed: info)
        #expect(listed.title == "Untitled session")
        #expect(listed.parentId == "p")
        #expect(listed.messagesCount == 3)
        #expect(listed.worktree == SessionWorktree(path: "/wt/app", branch: "droid/x", lifecycle: .persistent, repoRoot: "/w/app"))
        #expect(listed.callingSessionId == "caller")
        #expect(!isUnlisted(info))
        #expect(withActivity([listed], activeAt: ["s": 9])[0].updatedAt == 9)
        #expect(withActivity([listed], activeAt: ["s": 2])[0].updatedAt == 5)
    }
}

@Suite struct ContinuationTests {
    @Test func aChildFoldsItsParentOutOfTheListAndReadsTheLinkFromItsTag() {
        let parent = summary("old")
        let tags = continuationTags("old", inherited: [SessionTag(name: "team", metadata: ["id": "1"])])
        let child = summary("new", tags: tags, parentId: continuationParent(tags))
        #expect(child.parentId == "old")
        #expect(tags.map(\.name) == ["team", continuesTag])
        #expect(foldContinued([child, parent, summary("other")]).map(\.sessionId) == ["new", "other"])
    }

    @Test func aSessionThatNamesItselfAsItsParentStaysListed() {
        // Droi 1.10 and earlier tagged a Session compacted in place with itself.
        let same = summary("same", parentId: "same")
        #expect(foldContinued([same, summary("other")]).map(\.sessionId) == ["same", "other"])
    }

    @Test func continuationTagsReplacesAnOlderLink() {
        #expect(
            continuationTags("b", inherited: continuationTags("a", inherited: []))
                == [SessionTag(name: continuesTag, metadata: ["parent": "b"])])
    }

    @Test func walksBackThroughEveryCompactionNearestFirst() {
        let first = summary("a")
        let second = summary("b", parentId: "a")
        let third = summary("c", parentId: "b")
        #expect(continuationChain([third, first, second], of: third) == [second, first])
    }

    @Test func stopsWhereTheListNoLongerHasTheParentAndAtALoop() {
        let orphan = summary("b", parentId: "gone")
        #expect(continuationChain([orphan], of: orphan).isEmpty)
        let x = summary("x", parentId: "y")
        let y = summary("y", parentId: "x")
        #expect(continuationChain([x, y], of: x) == [y])
    }
}

@Suite struct AnnouncedSubagentsTests {
    private func announced(subagentType: String = "explorer", description: String = "Map the app") -> AnnouncedSubagent {
        AnnouncedSubagent(
            sessionId: "child", callingSessionId: "main", callingToolUseId: "toolu_1", subagentType: subagentType,
            description: description, cwd: nil, announcedAt: 50)
    }

    @Test func addsARowForASubagentTheListLacksInItsCallersWorkspace() {
        let main = summary("main", cwd: "/w/app/sub", repoRoot: "/w/app")
        let rows = withAnnouncedSubagents([main], [announced()])
        #expect(Array(rows.dropFirst()) == [main])
        let row = rows[0]
        #expect(row.sessionId == "child")
        #expect(row.title == "Explorer: Map the app")
        #expect(row.cwd == "/w/app/sub")
        #expect(row.repoRoot == "/w/app")
        #expect(row.updatedAt == 50)
        #expect(row.callingSessionId == "main")
        #expect(row.callingToolUseId == "toolu_1")
    }

    @Test func theListedRowWinsOnceTheDaemonListsTheSubagent() {
        let listed = [summary("main"), summary("child", title: "x")]
        #expect(withAnnouncedSubagents(listed, [announced()]) == listed)
    }

    @Test func withoutATypeOrDescriptionTheRowIsStillNamed() {
        #expect(withAnnouncedSubagents([], [announced(subagentType: "", description: "")])[0].title == "Subagent")
    }
}

@Suite struct NewSessionTests {
    @Test func recentWorkspacesDeduplicateByPathAndOrderByMostRecentUse() {
        let recent = recentWorkspaces([
            summary(cwd: "/w/alpha", updatedAt: 10),
            summary(cwd: "/w/beta/sub", repoRoot: "/w/beta", updatedAt: 30),
            summary(cwd: "/w/alpha", updatedAt: 50),
            summary(),
        ])
        #expect(recent.map(\.label) == ["alpha", "beta"])
        #expect(recent.map(\.lastUsedAt) == [50, 30])
    }

    @Test func offersTheMainCheckoutNeverAWorktreesFolder() {
        let recent = recentWorkspaces([
            summary(
                cwd: "/wt/ab12cd34/app", repoRoot: "/w/app",
                worktree: SessionWorktree(path: "/wt/ab12cd34/app", branch: "droid/x", lifecycle: .persistent, repoRoot: "/w/app"),
                updatedAt: 40)
        ])
        #expect(recent.map(\.path) == ["/w/app"])
    }

    @Test func leavesScratchWorkspacesOut() {
        #expect(
            recentWorkspaces([
                summary(cwd: "/u/.droi/chats/2026-09-26-aaaaaa", updatedAt: 90, tags: [SessionTag(name: "droi.scratch")]),
                summary(cwd: "/w/alpha", updatedAt: 10),
            ]).map(\.label) == ["alpha"])
        #expect(
            recentWorkspaces([
                summary(cwd: "/u/.droi/chats/2026-10-02-5c4802", updatedAt: 90), summary(cwd: "/w/alpha", updatedAt: 10),
            ]).map(\.label) == ["alpha"])
    }

    @Test func defaultPickIsTheMostRecentWorkspaceNoneWhenThereIsNoneNothingWhileLoading() {
        #expect(defaultPick([RecentWorkspace(path: "/w/app", label: "app", lastUsedAt: 1)]) == .recent(path: "/w/app"))
        #expect(defaultPick([]) == .scratch)
        #expect(defaultPick(nil) == nil)
    }

    @Test func worktreeParamsAskWithTheLifecycleAndTheFirstMessage() {
        var params = InitializeSessionParams(machineId: "local", cwd: "/w", token: Gateway.apiKeyPlaceholder)
        applyWorktree(WorktreeRequest(lifecycle: .persistent, promptSlug: " Fix the login bug "), to: &params)
        #expect(params.worktree == true)
        #expect(params.worktreeLifecycle == .persistent)
        #expect(params.worktreePromptSlug == "Fix the login bug")
        #expect(params.worktreeBaseBranch == nil)

        var bare = InitializeSessionParams(machineId: "local", cwd: "/w", token: Gateway.apiKeyPlaceholder)
        applyWorktree(WorktreeRequest(lifecycle: .ephemeral, promptSlug: "  ", baseBranch: "develop"), to: &bare)
        #expect(bare.worktreePromptSlug == nil)
        #expect(bare.worktreeBaseBranch == "develop")
        #expect(bare.worktreeBranchMode == .copy)
    }

    @Test func archivingDeletesAnEphemeralWorktreeOnly() {
        let tree = { (lifecycle: WorktreeLifecycle?) in
            SessionWorktree(path: "/wt", branch: "b", lifecycle: lifecycle, repoRoot: "/w")
        }
        #expect(archiveDeletesWorktree(tree(.ephemeral)))
        #expect(!archiveDeletesWorktree(tree(.persistent)))
        #expect(!archiveDeletesWorktree(tree(nil)))
        #expect(!archiveDeletesWorktree(nil))
        #expect(isWorktreeLifecycle("ephemeral") && isWorktreeLifecycle("persistent"))
        #expect(!isWorktreeLifecycle("other") && !isWorktreeLifecycle(nil))
    }
}
