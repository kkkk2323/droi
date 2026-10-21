import DroidProtocol
import Foundation
import Testing

@testable import DroiKit

@Suite struct MiscTests {
    @Test func theSameReleaseIsNoMismatchAVPrefixAside() {
        #expect(!versionMismatch("1.2.0", "1.2.0"))
        #expect(!versionMismatch("1.2.0", "v1.2.0"))
        #expect(versionMismatch("1.2.0", "1.3.0"))
        #expect(!versionMismatch("1.2.0", ""))
        #expect(versionNoticeKey(computerId: "c", appVersion: "1.2.0", computerVersion: "1.3.0") == "c 1.2.0 1.3.0")
    }

    @Test func readsTheExpiryOutOfAProvisioningProfile() {
        var profile = Data([0x30, 0x82, 0x0b, 0x01, 0x06, 0x09, 0x2a, 0x86, 0x48])
        profile.append(
            Data(
                """
                <?xml version="1.0" encoding="UTF-8"?>
                <plist version="1.0"><dict>
                  <key>CreationDate</key>
                  <date>2026-09-23T04:10:00Z</date>
                  <key>ExpirationDate</key>
                  <date>2026-09-30T04:10:00Z</date>
                </dict></plist>
                """.utf8))
        profile.append(contentsOf: [0x00, 0xa0, 0x82])
        #expect(profileExpiry(profile) == Date(timeIntervalSince1970: 1_790_741_400))
        #expect(profileExpiry(Data("<plist></plist>".utf8)) == nil)
    }

    @Test func daysLeftRoundsAPartDayUpAndStopsAtZero() throws {
        let expiry = try Date("2026-09-30T04:10:00Z", strategy: .iso8601)
        #expect(daysLeft(expiry, now: try Date("2026-09-23T04:10:00Z", strategy: .iso8601)) == 7)
        #expect(daysLeft(expiry, now: try Date("2026-09-29T20:00:00Z", strategy: .iso8601)) == 1)
        #expect(daysLeft(expiry, now: try Date("2026-10-01T00:00:00Z", strategy: .iso8601)) == 0)
    }

    @Test func contextUsagePrefersTheLastCall() {
        let measured = ContextUsage(lastCallTokens: 50_000, estimatedTokens: 900_000, budgetTokens: 200_000)
        #expect(measured.usedTokens == 50_000)
        #expect(measured.ratio == 0.25)
        #expect(ContextUsage(lastCallTokens: nil, estimatedTokens: 300_000, budgetTokens: 200_000).ratio == 1)
        #expect(ContextUsage(lastCallTokens: nil, estimatedTokens: 1, budgetTokens: 0).ratio == 0)
        #expect(formatTokens(1_250_000) == "1.3M")
        #expect(formatTokens(45_400) == "45k")
        #expect(formatTokens(1_250) == "1.3k")
        #expect(formatTokens(999) == "999")
    }

    @Test func sessionDetailsListTheTitleIdWorkspaceAndTranscript() {
        #expect(
            sessionDetails(
                sessionId: "a1b2c3", title: "Fix the login bug", cwd: "/Users/dev/acme-web",
                file: "/Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl")
                == """
                Title: Fix the login bug
                Session ID: a1b2c3
                Workspace: /Users/dev/acme-web
                Transcript: /Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl
                """)
        #expect(
            sessionDetails(sessionId: "a1b2c3", title: "Fix the login bug", cwd: nil, file: nil)
                == "Title: Fix the login bug\nSession ID: a1b2c3")
    }

    @Test func keysAreNumberedWhenANameRepeats() {
        #expect(withKeys(["a", "b", "a", "a"]) { $0 }.map(\.key) == ["a", "b", "a#1", "a#2"])
    }

    @Test func jsonTextWritesAsJavaScriptDoes() {
        let value: JSONValue = ["b": [1, 2.5, "x\n\"y\""], "a": nil, "c": ["d": true], "e": [], "f": [:]]
        #expect(JSONText.stringify(value) == #"{"a":null,"b":[1,2.5,"x\n\"y\""],"c":{"d":true},"e":[],"f":{}}"#)
        #expect(
            JSONText.stringify(["k": [1]], indent: 2) == """
                {
                  "k": [
                    1
                  ]
                }
                """)
        #expect(JSONText.number(1e21) == "1e+21")
        #expect(JSONText.number(-3) == "-3")
        #expect(JSONText.number(0.1) == "0.1")
        #expect(JSONText.parse("{oops}") == nil)
        #expect(JSONText.parse("3") == 3)
    }

    @Test func linesSplitOnNewlinesAloneAsJavaScriptDoes() {
        #expect("a\r\nb\n\nc".lines() == ["a\r", "b", "", "c"])
        #expect("".lines() == [""])
    }
}

@Suite struct PreferenceTests {
    @Test func preferencesReadTheirFallbackUntilSetAndRoundTrip() {
        let storage = MemoryPreferenceStorage()
        #expect(Preferences.favoriteModels.get(storage) == [])
        Preferences.favoriteModels.set(["gpt-5", "opus"], in: storage)
        #expect(storage.string(forKey: "droi.favoriteModels") == #"["gpt-5","opus"]"#)
        toggleListed(Preferences.favoriteModels, "gpt-5", in: storage)
        toggleListed(Preferences.favoriteModels, "auto", in: storage)
        #expect(Preferences.favoriteModels.get(storage) == ["opus", "auto"])

        Preferences.showArchivedSessions.set(true, in: storage)
        #expect(storage.string(forKey: "droi.showArchived") == "true")
        #expect(Preferences.showArchivedSessions.get(storage))

        #expect(Preferences.theme.get(storage) == .system)
        Preferences.theme.set(.solarizedLight, in: storage)
        #expect(storage.string(forKey: "droi.phoneTheme") == "solarized-light")
        #expect(ThemeChoice.system.theme(systemIsDark: true) == .dark)
        #expect(ThemeName.solarizedLight.scheme == .light)

        #expect(Preferences.worktreeLifecycle.get(storage) == .ephemeral)
        storage.set("weird", forKey: "droi.worktreeLifecycle")
        #expect(Preferences.worktreeLifecycle.get(storage) == .ephemeral)
        Preferences.worktreeLifecycle.set(.persistent, in: storage)
        #expect(Preferences.worktreeLifecycle.get(storage) == .persistent)

        Preferences.lastSession("c").set("s", in: storage)
        #expect(storage.string(forKey: "droi.lastSession.c") == "s")
        Preferences.lastSession("c").set(nil, in: storage)
        #expect(Preferences.lastSession("c").get(storage) == nil)
    }

    @Test func malformedValuesReadAsTheFallback() {
        let storage = MemoryPreferenceStorage([
            "droi.phoneAlerts": #"{"finishedSound":false}"#, "droi.pinnedSessions": "{nope", "droi.textSize": "large",
        ])
        #expect(Preferences.alertSwitches.get(storage) == AlertSwitches(finishedSound: false))
        #expect(Preferences.pinnedSessions.get(storage) == [])
        #expect(Preferences.textSize.get(storage) == .large)
    }
}

@Suite struct PairedComputerTests {
    let home = PairedComputer(id: "c-home", name: "Home Mac", address: "http://192.168.1.10:41417")
    let office = PairedComputer(id: "c-office", name: "Office", address: "http://10.0.0.5:41417")

    @Test func aKnownIdUpdatesTheAddressAndKeepsTheGivenName() {
        let renamed = PairedComputer(id: "c-home", name: "My desk", address: home.address)
        let moved = mergeComputer(
            [renamed], PairedComputer(id: "c-home", name: "Home Mac", address: "http://mac.tail1.ts.net:41417"))
        #expect(moved == [PairedComputer(id: "c-home", name: "My desk", address: "http://mac.tail1.ts.net:41417")])
        #expect(tokenKey("c-home") == "pairingToken.c-home")
    }

    @Test func anotherIdIsAnotherComputer() {
        #expect(mergeComputer([home], office) == [home, office])
    }

    @Test func theSelectionFallsBackToTheFirstComputer() {
        #expect(selectedComputer([home], selectedId: "gone") == home)
        #expect(selectedComputer([], selectedId: nil) == nil)
    }

    @Test func removingTheSelectedComputerSelectsAnother() {
        let removed = removingComputer("c-office", from: [home, office], selectedId: "c-office")
        #expect(removed.computers == [home])
        #expect(removed.selectedId == "c-home")
        #expect(removingComputer("c-office", from: [home, office], selectedId: "c-home").selectedId == "c-home")
    }

    @Test func theListAndTheCachedSessionsAreKeptWithoutTheToken() {
        let storage = MemoryPreferenceStorage()
        Preferences.pairedComputers.set([home], in: storage)
        #expect(Preferences.pairedComputers.get(storage) == [home])
        #expect(storage.string(forKey: "droi.pairedComputers")?.contains("pairingToken") == false)

        let cached = SessionSummaryCache(summary("s", title: "T", cwd: "/x", updatedAt: 1))
        Preferences.sessionSummaries("c-office").set([cached], in: storage)
        #expect(Preferences.sessionSummaries("c-office").get(storage).map(\.summary.sessionId) == ["s"])
        // Caches written before worktrees and subagents still read.
        storage.set(
            #"[{"sessionId":"s","title":"T","cwd":null,"repoRoot":null,"updatedAt":1,"archivedAt":null,"parentId":null}]"#,
            forKey: "droi.sessions.c-home")
        #expect(Preferences.sessionSummaries("c-home").get(storage).map(\.summary.title) == ["T"])
    }
}

@Suite struct ThemeTests {
    @Test func tokensMatchTheWebClient() {
        #expect(ThemeColors.light.background.description == "#ffffff")
        #expect(ThemeColors.dark.attention.description == "#ffb900")
        #expect(ThemeColors.solarizedLight.addedBackground.description == "rgba(133, 153, 0, 0.12)")
        #expect(Radius.composer == 18)
        #expect(Space.xl == 24)
        #expect(FontSize.base == 15)
    }
}
