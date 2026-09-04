import Foundation
import SQLite3
import Testing
@testable import NatterwireCore

@Suite struct NatterwireTests {
    @Test func listsChatsAndPaginatesDecodedMessages() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let chats = try database.chats(limit: 10, before: nil)
        #expect(chats.items.count == 7)
        #expect(chats.items[0].displayName == "Fixture Chat")
        #expect(chats.items[0].messageCount == 3)

        let first = try database.messages(chatID: chats.items[0].id, limit: 1, before: nil)
        #expect(first.items.map(\.text) == ["third from archive"])
        #expect(first.nextBefore != nil)
        let second = try database.messages(chatID: chats.items[0].id, limit: 10, before: first.nextBefore)
        #expect(second.items.map(\.text) == ["second", "first"])
    }

    @Test func undecodableAndEmptyBodiesStillPaginate() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let chat = try #require(database.chats(limit: 10, before: nil).items.first { $0.displayName == "Body Edge Cases" })

        let first = try database.messages(chatID: chat.id, limit: 1, before: nil)
        #expect(first.items.map(\.text) == [""])
        let firstCursor = try #require(first.nextBefore)
        let second = try database.messages(chatID: chat.id, limit: 1, before: firstCursor)
        #expect(second.items.map(\.text) == [""])
        let secondCursor = try #require(second.nextBefore)
        let third = try database.messages(chatID: chat.id, limit: 1, before: secondCursor)
        #expect(third.items.count == 1)
        #expect(third.nextBefore == nil)
    }

    @Test func paginatesChatsWithoutRepeatingAChat() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let first = try database.chats(limit: 1, before: nil)
        #expect(first.items.map(\.displayName) == ["Fixture Chat"])
        let cursor = try #require(first.nextBefore)
        let second = try database.chats(limit: 1, before: cursor)
        #expect(second.items.map(\.displayName) == ["Older Chat"])
    }

    @Test func filtersReactionsActionsAndDeletedRows() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let chat = try #require(database.chats(limit: 1, before: nil).items.first)
        let messages = try database.messages(chatID: chat.id, limit: 100, before: nil)
        #expect(messages.items.count == 3)
    }

    @Test func enrichesChatNamesAndAlwaysProvidesFallbacks() throws {
        let fixture = try Fixture()
        let resolver = ChatNameResolver(
            emailLookup: { $0 == "friend@example.invalid" ? "Email Friend" : nil },
            phoneLookup: { $0 == "+14155550100" ? "Phone Friend" : nil })
        let resolved = try MessagesDatabase(path: fixture.path, nameResolver: resolver)
            .chats(limit: 20, before: nil).items.map(\.displayName)
        #expect(resolved.contains("Fixture Chat"))
        #expect(resolved.contains("Email Friend"))
        #expect(resolved.contains("Phone Friend"))
        #expect(resolved.contains("missing@example.invalid"))
        #expect(resolved.contains("Group chat"))
        #expect(resolved.allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty })

        let unavailable = try MessagesDatabase(path: fixture.path)
            .chats(limit: 20, before: nil).items.map(\.displayName)
        #expect(unavailable.contains("friend@example.invalid"))
        #expect(unavailable.contains("+1 (415) 555-0100"))
        #expect(unavailable.allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty })
    }

    @Test func supportsUnauthenticatedRoutesAndErrors() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let api = NatterwireAPI(database: database)
        let chatsResponse = api.respond(method: "GET", target: "/chats?limit=1")
        #expect(chatsResponse.status == 200)
        let chatID = try JSONDecoder().decode(Page<Chat>.self, from: chatsResponse.body).items[0].id
        #expect(api.respond(method: "GET", target: "/chats/\(chatID)/messages").status == 200)
        #expect(api.respond(method: "GET", target: "/messages/\(chatID)").status == 200)
        #expect(api.respond(method: "GET", target: "/chats?limit=101").status == 400)
        #expect(api.respond(method: "GET", target: "/chats?limit=nope").status == 400)
        #expect(api.respond(method: "GET", target: "/chats?limit=").status == 400)
        #expect(api.respond(method: "POST", target: "/chats").status == 405)
        #expect(api.respond(method: "GET", target: "/missing").status == 404)
    }

    @Test func serverStopsAndReleasesItsPort() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let api = NatterwireAPI(database: database)
        let port = UInt16.random(in: 20_000...50_000)
        try runAndStop(HTTPServer(host: "127.0.0.1", port: port, api: api))
        try runAndStop(HTTPServer(host: "127.0.0.1", port: port, api: api))
    }

    private func runAndStop(_ server: HTTPServer) throws {
        let ready = DispatchSemaphore(value: 0)
        let done = DispatchSemaphore(value: 0)
        let result = ServerResult()
        DispatchQueue.global().async {
            defer { done.signal() }
            do { try server.run { ready.signal() } } catch { result.set(error) }
        }
        #expect(ready.wait(timeout: .now() + 2) == .success)
        server.stop()
        #expect(done.wait(timeout: .now() + 2) == .success)
        if let error = result.error { throw error }
    }
}

private final class Fixture {
    let path: String

    init() throws {
        path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".db").path
        var db: OpaquePointer?
        guard sqlite3_open(path, &db) == SQLITE_OK, let db else { throw FixtureError.create }
        defer { sqlite3_close(db) }
        let schema = """
            CREATE TABLE chat (ROWID INTEGER PRIMARY KEY, guid TEXT, display_name TEXT, service_name TEXT, chat_identifier TEXT, style INTEGER, is_archived INTEGER, is_deleted INTEGER);
            CREATE TABLE message (ROWID INTEGER PRIMARY KEY, guid TEXT, text TEXT, attributedBody BLOB, date INTEGER, is_from_me INTEGER, handle_id INTEGER, service TEXT, item_type INTEGER, group_action_type INTEGER, associated_message_type INTEGER, is_deleted INTEGER);
            CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER);
            CREATE TABLE handle (ROWID INTEGER PRIMARY KEY, id TEXT);
            INSERT INTO chat VALUES (1, 'iMessage;-;fixture@example.invalid', 'Fixture Chat', 'iMessage', 'fixture@example.invalid', 43, 0, 0);
            INSERT INTO chat VALUES (2, 'iMessage;-;older@example.invalid', 'Older Chat', 'iMessage', 'older@example.invalid', 43, 0, 0);
            INSERT INTO chat VALUES (3, 'iMessage;-;body-edge-cases', 'Body Edge Cases', 'iMessage', 'body-edge-cases', 43, 0, 0);
            INSERT INTO chat VALUES (4, 'any;-;friend@example.invalid', NULL, 'iMessage', 'friend@example.invalid', 43, 0, 0);
            INSERT INTO chat VALUES (5, 'any;-;+1 (415) 555-0100', '', 'iMessage', '+1 (415) 555-0100', 43, 0, 0);
            INSERT INTO chat VALUES (6, 'any;-;missing@example.invalid', NULL, 'iMessage', NULL, 43, 0, 0);
            INSERT INTO chat VALUES (7, 'iMessage;+;group-id', '   ', 'iMessage', 'group-id', 45, 0, 0);
            INSERT INTO handle VALUES (1, 'fixture@example.invalid');
            INSERT INTO message VALUES (1, 'm1', 'first', NULL, 100, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (2, 'm2', 'second', NULL, 200, 1, NULL, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (4, 'reaction', 'liked', NULL, 400, 0, 1, 'iMessage', 0, 0, 2000, 0);
            INSERT INTO message VALUES (5, 'action', 'joined', NULL, 500, 0, 1, 'iMessage', 0, 1, 0, 0);
            INSERT INTO message VALUES (6, 'deleted', 'deleted', NULL, 600, 0, 1, 'iMessage', 0, 0, 0, 1);
            INSERT INTO chat_message_join VALUES (1, 1), (1, 2), (1, 4), (1, 5), (1, 6);
            INSERT INTO message VALUES (7, 'older', 'older', NULL, 50, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO chat_message_join VALUES (2, 7);
            INSERT INTO message VALUES (8, 'invalid-body', NULL, X'010203', 25, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (9, 'empty-body', NULL, X'', 20, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (10, 'body-tail', 'tail', NULL, 10, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO chat_message_join VALUES (3, 8), (3, 9), (3, 10);
            INSERT INTO message VALUES (11, 'email-chat', 'email', NULL, 40, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (12, 'phone-chat', 'phone', NULL, 35, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (13, 'missing-chat', 'missing', NULL, 30, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (14, 'group-chat', 'group', NULL, 5, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO chat_message_join VALUES (4, 11), (5, 12), (6, 13), (7, 14);
            """
        guard sqlite3_exec(db, schema, nil, nil, nil) == SQLITE_OK else { throw FixtureError.create }
        let archive = NSArchiver.archivedData(withRootObject: NSAttributedString(string: "third from archive"))
        var statement: OpaquePointer?
        sqlite3_prepare_v2(db, "INSERT INTO message VALUES (3, 'm3', NULL, ?, 300, 0, 1, 'iMessage', 0, 0, 0, 0)", -1, &statement, nil)
        _ = archive.withUnsafeBytes { sqlite3_bind_blob(statement, 1, $0.baseAddress, Int32($0.count), unsafeBitCast(-1, to: sqlite3_destructor_type.self)) }
        guard sqlite3_step(statement) == SQLITE_DONE else { throw FixtureError.create }
        sqlite3_finalize(statement)
        guard sqlite3_exec(db, "INSERT INTO chat_message_join VALUES (1, 3)", nil, nil, nil) == SQLITE_OK else { throw FixtureError.create }
    }

    deinit { try? FileManager.default.removeItem(atPath: path) }
}

private enum FixtureError: Error { case create }

private final class ServerResult: @unchecked Sendable {
    private let lock = NSLock()
    private var storedError: Error?

    var error: Error? {
        lock.lock(); defer { lock.unlock() }
        return storedError
    }

    func set(_ error: Error) {
        lock.lock(); storedError = error; lock.unlock()
    }
}
