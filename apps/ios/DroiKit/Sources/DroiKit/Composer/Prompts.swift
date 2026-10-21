// Answering Prompts: a permission request with one of its options, an
// ask-user question one question at a time. Whichever Client answers first
// wins; the others see the request resolve.
import Foundation

/// A rejection that only says another Client answered first; it is not shown.
public func isAlreadyResolved(_ message: String) -> Bool {
    let lower = message.lowercased()
    return lower.contains("no pending") || lower.contains("not found")
}

/// One answer as `daemon.ask_user`'s result carries it.
public struct AskUserAnswer: Codable, Sendable, Hashable {
    public var index: Int
    public var question: String
    public var answer: String

    public init(index: Int, question: String, answer: String) {
        self.index = index
        self.question = question
        self.answer = answer
    }
}

/// The answer to one question: what was typed wins over the options chosen,
/// which are joined with ", ".
public func askUserAnswer(index: Int, question: String, chosen: [String], typed: String) -> AskUserAnswer {
    let own = typed.trimmed
    return AskUserAnswer(index: index, question: question, answer: own.isEmpty ? chosen.joined(separator: ", ") : own)
}
