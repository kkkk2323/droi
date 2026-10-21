import Testing

@testable import DroiKit

@MainActor
@Suite struct CompactionLogTests {
    @Test func aSessionIsPendingFromStartToFinishThenRememberedWhereTheUserLanded() {
        let log = CompactionLog()
        var done: [CompactionFinished] = []
        log.onDone { done.append($0) }

        log.start("parent")
        #expect(log.pending.contains("parent"))

        log.finish("parent", shownIn: "child", removedCount: 12, now: 1000)
        #expect(!log.pending.contains("parent"))
        #expect(log.finished["child"] == CompactionDone(removedCount: 12, finishedAt: 1000))
        #expect(log.finished["parent"] == nil)
        #expect(
            done == [
                CompactionFinished(
                    sessionId: "parent", shownIn: "child", done: CompactionDone(removedCount: 12, finishedAt: 1000))
            ])
    }

    @Test func aFailureClearsPendingWithoutADoneEvent() {
        let log = CompactionLog()
        var done = 0
        log.onDone { _ in done += 1 }
        log.start("s")
        log.fail("s")
        #expect(log.pending.isEmpty)
        #expect(done == 0)
    }

    @Test func aStoppedListenerHearsNothingMore() {
        let log = CompactionLog()
        var done = 0
        let stop = log.onDone { _ in done += 1 }
        log.finish("a", shownIn: "a", removedCount: 1)
        stop()
        log.finish("b", shownIn: "b", removedCount: 1)
        #expect(done == 1)
    }

    @Test func theNoticeLastsEightSeconds() {
        let log = CompactionLog()
        log.finish("s", shownIn: "s", removedCount: 3, now: 1_000)
        #expect(log.notice(for: "s", now: 8_999)?.removedCount == 3)
        #expect(log.notice(for: "s", now: 9_000) == nil)
        #expect(log.notice(for: "other", now: 1_000) == nil)
    }

    @Test func compactIsReadWithItsInstructions() {
        #expect(compactCommand("/compact") == CompactCommand(instructions: nil))
        #expect(compactCommand("/compact  keep the plan ") == CompactCommand(instructions: "keep the plan"))
        #expect(compactCommand("/compact\nline") == CompactCommand(instructions: "line"))
        #expect(compactCommand("/compacting") == nil)
        #expect(compactCommand("please /compact") == nil)
    }
}
