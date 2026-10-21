import DroidProtocol
import Foundation

@testable import DroiKit

/// A message as the Daemon sends it, from JSON, with a fresh id unless given.
func message(
    _ role: String, _ content: [JSONValue], _ extra: [String: JSONValue] = [:]
) -> FactoryDroidMessage {
    var object: [String: JSONValue] = [
        "id": .string("\(role)-\(UUID().uuidString)"),
        "role": .string(role),
        "content": .array(content),
        "createdAt": 1,
        "updatedAt": 1,
    ]
    for (key, value) in extra { object[key] = value }
    return try! JSONValue.object(object).decode(FactoryDroidMessage.self)
}

func text(_ text: String) -> JSONValue {
    ["type": "text", "text": .string(text)]
}

func toolUse(_ id: String, _ name: String, _ input: [String: JSONValue] = [:], extra: [String: JSONValue] = [:])
    -> JSONValue
{
    var object: [String: JSONValue] = ["type": "tool_use", "id": .string(id), "name": .string(name), "input": .object(input)]
    for (key, value) in extra { object[key] = value }
    return .object(object)
}

func toolResult(_ id: String, _ content: JSONValue, isError: Bool? = nil) -> JSONValue {
    var object: [String: JSONValue] = ["type": "tool_result", "toolUseId": .string(id), "content": content]
    if let isError { object["isError"] = .bool(isError) }
    return .object(object)
}

/// A tool call built directly, as the TypeScript tests cast one.
func call(
    _ name: String, _ input: [String: JSONValue] = [:], result content: JSONValue? = nil, isError: Bool = false,
    id: String = "c1", nested: [ToolCall]? = nil
) -> ToolCall {
    ToolCall(
        use: ToolUse(id: id, input: input, name: name),
        result: content.map { ToolResult(toolUseId: id, content: $0, isError: isError) },
        nested: nested)
}

func summary(
    _ sessionId: String = UUID().uuidString, title: String = "t", cwd: String? = nil, repoRoot: String? = nil,
    worktree: SessionWorktree? = nil, updatedAt: Double = 0, messagesCount: Int? = nil, archivedAt: String? = nil,
    tags: [SessionTag] = [], parentId: String? = nil, callingSessionId: String? = nil,
    callingToolUseId: String? = nil
) -> SessionSummary {
    SessionSummary(
        sessionId: sessionId, title: title, cwd: cwd, repoRoot: repoRoot, worktree: worktree, updatedAt: updatedAt,
        messagesCount: messagesCount, archivedAt: archivedAt, tags: tags, parentId: parentId,
        callingSessionId: callingSessionId, callingToolUseId: callingToolUseId)
}

func json(_ text: String) -> JSONValue {
    JSONText.parse(text)!
}
