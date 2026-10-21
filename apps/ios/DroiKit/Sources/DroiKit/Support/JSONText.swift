import DroidProtocol
import Foundation

/// JSON text as JavaScript's `JSON.stringify` writes it, so what the Clients
/// show of a tool's input or a returned value reads the same everywhere.
/// `JSONValue` keeps no key order, so object keys come sorted.
public enum JSONText {
    public static func stringify(_ value: JSONValue, indent: Int? = nil) -> String {
        var out = ""
        write(value, into: &out, indent: indent.map { String(repeating: " ", count: $0) }, depth: 0)
        return out
    }

    public static func stringify(_ object: [String: JSONValue], indent: Int? = nil) -> String {
        stringify(.object(object), indent: indent)
    }

    /// The value a JSON text holds, or nil when it does not parse.
    public static func parse(_ text: String) -> JSONValue? {
        try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))
    }

    /// A number as JavaScript prints it: `3`, `0.25`, `1e+21`.
    public static func number(_ value: Double) -> String {
        if value.isNaN || value.isInfinite { return "null" }
        if value == value.rounded(), abs(value) < 1e21 {
            return String(Int64(value))
        }
        var text = "\(value)"
        if let e = text.firstIndex(of: "e") {
            let exponent = text[text.index(after: e)...]
            if !exponent.hasPrefix("-") && !exponent.hasPrefix("+") {
                text.insert("+", at: text.index(after: e))
            }
        }
        return text
    }

    private static func write(_ value: JSONValue, into out: inout String, indent: String?, depth: Int) {
        switch value {
        case .null: out += "null"
        case .bool(let bool): out += bool ? "true" : "false"
        case .number(let number): out += self.number(number)
        case .string(let string): quote(string, into: &out)
        case .array(let items):
            if items.isEmpty {
                out += "[]"
                return
            }
            out += "["
            for (index, item) in items.enumerated() {
                if index > 0 { out += "," }
                newline(into: &out, indent: indent, depth: depth + 1)
                write(item, into: &out, indent: indent, depth: depth + 1)
            }
            newline(into: &out, indent: indent, depth: depth)
            out += "]"
        case .object(let object):
            if object.isEmpty {
                out += "{}"
                return
            }
            out += "{"
            for (index, key) in object.keys.sorted().enumerated() {
                if index > 0 { out += "," }
                newline(into: &out, indent: indent, depth: depth + 1)
                quote(key, into: &out)
                out += indent == nil ? ":" : ": "
                write(object[key]!, into: &out, indent: indent, depth: depth + 1)
            }
            newline(into: &out, indent: indent, depth: depth)
            out += "}"
        }
    }

    private static func newline(into out: inout String, indent: String?, depth: Int) {
        guard let indent else { return }
        out += "\n"
        for _ in 0..<depth { out += indent }
    }

    private static func quote(_ string: String, into out: inout String) {
        out += "\""
        for scalar in string.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case "\u{08}": out += "\\b"
            case "\u{0C}": out += "\\f"
            default:
                if scalar.value < 0x20 {
                    out += "\\u" + String(format: "%04x", scalar.value)
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        out += "\""
    }
}

extension JSONValue {
    /// The value as JavaScript's `String(value)` reads a string, number or boolean; nil otherwise.
    var scalarText: String? {
        switch self {
        case .string(let string): string
        case .number(let number): JSONText.number(number)
        case .bool(let bool): bool ? "true" : "false"
        default: nil
        }
    }
}
