import Foundation
import SQLite3

public enum MessagesDatabaseError: Error, CustomStringConvertible {
    case sqlite(String)
    case invalidIdentifier
    case invalidCursor

    public var description: String {
        switch self {
        case .sqlite(let message): message
        case .invalidIdentifier: "invalid chat identifier"
        case .invalidCursor: "invalid pagination cursor"
        }
    }
}

public struct Chat: Codable, Sendable {
    public let id: String
    public let displayName: String
    public let service: String?
    public let lastMessageAt: String?
    public let messageCount: Int64
}

public struct Message: Codable, Sendable {
    public let id: String
    public let text: String
    public let sentAt: String?
    public let isFromMe: Bool
    public let sender: String?
    public let service: String?
}

public struct Page<Element: Codable & Sendable>: Codable, Sendable {
    public let items: [Element]
    public let nextBefore: String?

    private enum CodingKeys: String, CodingKey { case items, nextBefore }

    public init(items: [Element], nextBefore: String?) {
        self.items = items
        self.nextBefore = nextBefore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = try container.decode([Element].self, forKey: .items)
        nextBefore = try container.decodeIfPresent(String.self, forKey: .nextBefore)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(items, forKey: .items)
        if let nextBefore {
            try container.encode(nextBefore, forKey: .nextBefore)
        } else {
            try container.encodeNil(forKey: .nextBefore)
        }
    }
}

public final class MessagesDatabase: @unchecked Sendable {
    private let db: OpaquePointer
    private let messageColumns: Set<String>
    private let chatColumns: Set<String>
    private let nameResolver: ChatNameResolver
    private let pinnedChatIdentifiers: [String]
    private static let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    public init(
        path: String,
        nameResolver: ChatNameResolver = .unavailable,
        pinnedChatIdentifiers: [String] = []
    ) throws {
        var connection: OpaquePointer?
        let flags = SQLITE_OPEN_READONLY | SQLITE_OPEN_FULLMUTEX
        guard sqlite3_open_v2(path, &connection, flags, nil) == SQLITE_OK, let connection else {
            let message = connection.map { String(cString: sqlite3_errmsg($0)) } ?? "unable to open database"
            if let connection { sqlite3_close(connection) }
            throw MessagesDatabaseError.sqlite(message)
        }
        db = connection
        sqlite3_busy_timeout(db, 3_000)
        guard sqlite3_exec(db, "PRAGMA query_only = ON", nil, nil, nil) == SQLITE_OK else {
            let message = String(cString: sqlite3_errmsg(db))
            sqlite3_close(db)
            throw MessagesDatabaseError.sqlite(message)
        }
        messageColumns = try Self.columns(in: "message", db: db)
        chatColumns = try Self.columns(in: "chat", db: db)
        self.nameResolver = nameResolver
        self.pinnedChatIdentifiers = pinnedChatIdentifiers.reduce(into: []) {
            if !$1.isEmpty, !$0.contains($1) { $0.append($1) }
        }
    }

    deinit { sqlite3_close(db) }

    public func chats(limit: Int, before: String?) throws -> Page<Chat> {
        let bounded = Self.bounded(limit)
        let cursor = try before.map(Self.decodeChatCursor)
        let filters = messageFilters(alias: "m") + " AND " + chatFilters(alias: "c")
        let cursorClause: String
        let ordering: String
        switch cursor {
        case .legacy?:
            cursorClause = "HAVING MAX(m.date) < ? OR (MAX(m.date) = ? AND c.ROWID < ?)"
            ordering = "MAX(m.date) DESC, c.ROWID DESC"
        case .ranked?:
            cursorClause = "HAVING pin_order > ? OR (pin_order = ? AND (MAX(m.date) < ? OR (MAX(m.date) = ? AND c.ROWID < ?)))"
            ordering = "pin_order ASC, MAX(m.date) DESC, c.ROWID DESC"
        case nil:
            cursorClause = ""
            ordering = "pin_order ASC, MAX(m.date) DESC, c.ROWID DESC"
        }
        let display = chatColumns.contains("display_name") ? "c.display_name" : "NULL"
        let service = chatColumns.contains("service_name") ? "c.service_name" : "NULL"
        let identifier = chatColumns.contains("chat_identifier") ? "c.chat_identifier" : "NULL"
        let participantCount = Self.tableExists("chat_handle_join", db: db)
            ? "(SELECT COUNT(*) FROM chat_handle_join chj WHERE chj.chat_id = c.ROWID)"
            : "0"
        let pinColumns = [
            chatColumns.contains("chat_identifier") ? "c.chat_identifier" : nil,
            chatColumns.contains("group_id") ? "c.group_id" : nil,
        ].compactMap { $0 }
        let pinOrder = pinColumns.isEmpty ? "" : pinnedChatIdentifiers.enumerated().map { offset, _ in
            "WHEN " + pinColumns.map { "\($0) = ?" }.joined(separator: " OR ") + " THEN \(offset)"
        }.joined(separator: " ")
        let pinExpression = pinOrder.isEmpty ? "0" : "CASE \(pinOrder) ELSE \(pinnedChatIdentifiers.count) END"
        let sql = """
            SELECT c.guid, \(display), \(service), MAX(m.date), COUNT(m.ROWID), c.ROWID,
                   \(identifier), \(participantCount), \(pinExpression) AS pin_order
            FROM chat c
            JOIN chat_message_join cmj ON cmj.chat_id = c.ROWID
            JOIN message m ON m.ROWID = cmj.message_id
            WHERE c.guid IS NOT NULL AND \(filters)
            GROUP BY c.ROWID
            \(cursorClause)
            ORDER BY \(ordering)
            LIMIT ?
            """
        let statement = try prepare(sql)
        defer { sqlite3_finalize(statement) }
        var index: Int32 = 1
        for pin in pinnedChatIdentifiers {
            for _ in pinColumns { bind(pin, to: index, in: statement); index += 1 }
        }
        if let cursor {
            switch cursor {
            case .legacy(let date, let rowID):
                bind(date, to: index, in: statement); index += 1
                bind(date, to: index, in: statement); index += 1
                bind(rowID, to: index, in: statement); index += 1
            case .ranked(let pinOrder, let date, let rowID):
                bind(pinOrder, to: index, in: statement); index += 1
                bind(pinOrder, to: index, in: statement); index += 1
                bind(date, to: index, in: statement); index += 1
                bind(date, to: index, in: statement); index += 1
                bind(rowID, to: index, in: statement); index += 1
            }
        }
        bind(Int64(bounded + 1), to: index, in: statement)

        var rows: [(Chat, Int64, Int64, Int64)] = []
        while try step(statement) {
            guard let guid = text(statement, 0) else { continue }
            let date = sqlite3_column_int64(statement, 3)
            let rowID = sqlite3_column_int64(statement, 5)
            let pinOrder = sqlite3_column_int64(statement, 8)
            rows.append((Chat(
                id: Self.encodeOpaque(guid),
                displayName: chatDisplayName(
                    explicit: text(statement, 1),
                    guid: guid,
                    identifier: text(statement, 6),
                    participantCount: sqlite3_column_int64(statement, 7)),
                service: text(statement, 2),
                lastMessageAt: Self.dateString(date),
                messageCount: sqlite3_column_int64(statement, 4)
            ), date, rowID, pinOrder))
        }
        let hasMore = rows.count > bounded
        if hasMore { rows.removeLast() }
        let next = hasMore ? rows.last.map {
            if case .legacy? = cursor { return Self.encodeLegacyCursor(date: $0.1, rowID: $0.2) }
            return Self.encodeCursor(pinOrder: $0.3, date: $0.1, rowID: $0.2)
        } : nil
        return Page(items: rows.map(\.0), nextBefore: next)
    }

    private func chatDisplayName(
        explicit: String?,
        guid: String,
        identifier: String?,
        participantCount: Int64
    ) -> String {
        let explicit = explicit.flatMap {
            $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : $0
        }
        if guid.contains(";+;") || participantCount > 1 { return explicit ?? "Group chat" }
        let handle = identifier?.trimmingCharacters(in: .whitespacesAndNewlines)
        let fallback = handle.flatMap { $0.isEmpty ? nil : $0 } ?? Self.directHandle(from: guid)
        if let fallback, let resolved = nameResolver.name(for: fallback) { return resolved }
        return explicit ?? fallback ?? "Chat"
    }

    private static func directHandle(from guid: String) -> String? {
        guard let separator = guid.range(of: ";-;") else { return nil }
        let handle = guid[separator.upperBound...].trimmingCharacters(in: .whitespacesAndNewlines)
        return handle.isEmpty ? nil : handle
    }

    public func messages(chatID: String, limit: Int, before: String?) throws -> Page<Message> {
        guard let guid = Self.decodeOpaque(chatID) else { throw MessagesDatabaseError.invalidIdentifier }
        let bounded = Self.bounded(limit)
        let cursor = try before.map(Self.decodeLegacyCursor)
        let cursorClause = cursor == nil ? "" : "AND (m.date < ? OR (m.date = ? AND m.ROWID < ?))"
        let body = messageColumns.contains("attributedBody") ? "m.attributedBody" : "NULL"
        let sender = messageColumns.contains("handle_id") ? "h.id" : "NULL"
        let handleJoin = messageColumns.contains("handle_id") ? "LEFT JOIN handle h ON h.ROWID = m.handle_id" : ""
        let service = messageColumns.contains("service") ? "m.service" : "NULL"
        let sql = """
            SELECT m.guid, m.text, \(body), m.date, m.is_from_me, \(sender), \(service), m.ROWID
            FROM chat c
            JOIN chat_message_join cmj ON cmj.chat_id = c.ROWID
            JOIN message m ON m.ROWID = cmj.message_id
            \(handleJoin)
            WHERE c.guid = ? AND \(messageFilters(alias: "m")) \(cursorClause)
            ORDER BY m.date DESC, m.ROWID DESC
            LIMIT ?
            """
        let statement = try prepare(sql)
        defer { sqlite3_finalize(statement) }
        var index: Int32 = 1
        bind(guid, to: index, in: statement); index += 1
        if let cursor {
            bind(cursor.date, to: index, in: statement); index += 1
            bind(cursor.date, to: index, in: statement); index += 1
            bind(cursor.rowID, to: index, in: statement); index += 1
        }
        bind(Int64(bounded + 1), to: index, in: statement)

        var rows: [(Message, Int64, Int64)] = []
        while try step(statement) {
            let plainText = text(statement, 1)
            let attributed = blob(statement, 2).flatMap(AttributedBodyDecoder.decode)
            let value = plainText ?? attributed ?? ""
            let date = sqlite3_column_int64(statement, 3)
            let rowID = sqlite3_column_int64(statement, 7)
            rows.append((Message(
                id: text(statement, 0) ?? String(rowID),
                text: value,
                sentAt: Self.dateString(date),
                isFromMe: sqlite3_column_int(statement, 4) != 0,
                sender: text(statement, 5),
                service: text(statement, 6)
            ), date, rowID))
        }
        let hasMore = rows.count > bounded
        if hasMore { rows.removeLast() }
        let next = hasMore ? rows.last.map { Self.encodeLegacyCursor(date: $0.1, rowID: $0.2) } : nil
        return Page(items: rows.map(\.0), nextBefore: next)
    }

    private func messageFilters(alias: String) -> String {
        var filters = ["\(alias).date IS NOT NULL"]
        if messageColumns.contains("attributedBody") {
            filters.append("(\(alias).text IS NOT NULL OR \(alias).attributedBody IS NOT NULL)")
        } else {
            filters.append("\(alias).text IS NOT NULL")
        }
        if messageColumns.contains("item_type") { filters.append("COALESCE(\(alias).item_type, 0) = 0") }
        if messageColumns.contains("group_action_type") { filters.append("COALESCE(\(alias).group_action_type, 0) = 0") }
        if messageColumns.contains("associated_message_type") { filters.append("COALESCE(\(alias).associated_message_type, 0) = 0") }
        if messageColumns.contains("is_deleted") { filters.append("COALESCE(\(alias).is_deleted, 0) = 0") }
        return filters.joined(separator: " AND ")
    }

    private func chatFilters(alias: String) -> String {
        var filters: [String] = []
        if chatColumns.contains("is_archived") { filters.append("COALESCE(\(alias).is_archived, 0) = 0") }
        if chatColumns.contains("is_deleted") { filters.append("COALESCE(\(alias).is_deleted, 0) = 0") }
        return filters.isEmpty ? "1" : filters.joined(separator: " AND ")
    }

    private static func columns(in table: String, db: OpaquePointer) throws -> Set<String> {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, "PRAGMA table_info(\(table))", -1, &statement, nil) == SQLITE_OK,
              let statement else { throw MessagesDatabaseError.sqlite(String(cString: sqlite3_errmsg(db))) }
        defer { sqlite3_finalize(statement) }
        var result: Set<String> = []
        while sqlite3_step(statement) == SQLITE_ROW {
            if let value = sqlite3_column_text(statement, 1) { result.insert(String(cString: value)) }
        }
        guard !result.isEmpty else { throw MessagesDatabaseError.sqlite("missing required table: \(table)") }
        return result
    }

    private static func tableExists(_ table: String, db: OpaquePointer) -> Bool {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(
            db,
            "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?",
            -1,
            &statement,
            nil
        ) == SQLITE_OK, let statement else { return false }
        defer { sqlite3_finalize(statement) }
        sqlite3_bind_text(statement, 1, table, -1, transient)
        return sqlite3_step(statement) == SQLITE_ROW
    }

    private func prepare(_ sql: String) throws -> OpaquePointer {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK, let statement else {
            throw MessagesDatabaseError.sqlite(String(cString: sqlite3_errmsg(db)))
        }
        return statement
    }

    private func step(_ statement: OpaquePointer) throws -> Bool {
        let result = sqlite3_step(statement)
        if result == SQLITE_ROW { return true }
        if result == SQLITE_DONE { return false }
        throw MessagesDatabaseError.sqlite(String(cString: sqlite3_errmsg(db)))
    }

    private func bind(_ value: String, to index: Int32, in statement: OpaquePointer) {
        sqlite3_bind_text(statement, index, value, -1, Self.transient)
    }

    private func bind(_ value: Int64, to index: Int32, in statement: OpaquePointer) {
        sqlite3_bind_int64(statement, index, value)
    }

    private func text(_ statement: OpaquePointer, _ index: Int32) -> String? {
        guard sqlite3_column_type(statement, index) != SQLITE_NULL,
              let value = sqlite3_column_text(statement, index) else { return nil }
        return String(cString: value)
    }

    private func blob(_ statement: OpaquePointer, _ index: Int32) -> Data? {
        guard sqlite3_column_type(statement, index) == SQLITE_BLOB,
              let bytes = sqlite3_column_blob(statement, index) else { return nil }
        return Data(bytes: bytes, count: Int(sqlite3_column_bytes(statement, index)))
    }

    private static func bounded(_ limit: Int) -> Int { min(max(limit, 1), 100) }

    private static func dateString(_ raw: Int64) -> String? {
        guard raw > 0 else { return nil }
        let cocoaSeconds = Double(raw) / (raw > 10_000_000_000 ? 1_000_000_000 : 1)
        return ISO8601DateFormatter().string(from: Date(timeIntervalSinceReferenceDate: cocoaSeconds))
    }

    private static func encodeOpaque(_ value: String) -> String {
        Data(value.utf8).base64URLEncodedString()
    }

    private static func decodeOpaque(_ value: String) -> String? {
        Data(base64URLEncoded: value).flatMap { String(data: $0, encoding: .utf8) }
    }

    private enum Cursor {
        case legacy(date: Int64, rowID: Int64)
        case ranked(pinOrder: Int64, date: Int64, rowID: Int64)
    }

    private static func encodeCursor(pinOrder: Int64, date: Int64, rowID: Int64) -> String {
        encodeOpaque("v2:\(pinOrder):\(date):\(rowID)")
    }

    private static func encodeLegacyCursor(date: Int64, rowID: Int64) -> String {
        encodeOpaque("\(date):\(rowID)")
    }

    private static func decodeChatCursor(_ value: String) throws -> Cursor {
        guard let decoded = decodeOpaque(value) else { throw MessagesDatabaseError.invalidCursor }
        let parts = decoded.split(separator: ":", omittingEmptySubsequences: false)
        if parts.count == 2, let date = Int64(parts[0]), let rowID = Int64(parts[1]) {
            return .legacy(date: date, rowID: rowID)
        }
        if parts.count == 4, parts[0] == "v2", let pinOrder = Int64(parts[1]),
           let date = Int64(parts[2]), let rowID = Int64(parts[3]) {
            return .ranked(pinOrder: pinOrder, date: date, rowID: rowID)
        }
        throw MessagesDatabaseError.invalidCursor
    }

    private static func decodeLegacyCursor(_ value: String) throws -> (date: Int64, rowID: Int64) {
        guard let decoded = decodeOpaque(value) else { throw MessagesDatabaseError.invalidCursor }
        let parts = decoded.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2, let date = Int64(parts[0]), let rowID = Int64(parts[1]) else {
            throw MessagesDatabaseError.invalidCursor
        }
        return (date, rowID)
    }
}

private extension Data {
    func base64URLEncodedString() -> String {
        base64EncodedString().replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    init?(base64URLEncoded value: String) {
        var value = value.replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        value += String(repeating: "=", count: (4 - value.count % 4) % 4)
        self.init(base64Encoded: value)
    }
}
