import Darwin
import Foundation
import ImageIO
import SQLite3
import Testing
@testable import Natterwire
@testable import NatterwireCore

@Suite struct NatterwireTests {
    @Test func suppliesFullResolutionJPEGForHEICWithoutChangingOriginal() throws {
        let context = try #require(CGContext(
            data: nil, width: 2048, height: 1536, bitsPerComponent: 8, bytesPerRow: 0,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue))
        context.setFillColor(red: 1, green: 0.25, blue: 0, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: 2048, height: 1536))
        let image = try #require(context.makeImage())
        let original = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(original, "public.heic" as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, [kCGImagePropertyOrientation: 6] as CFDictionary)
        #expect(CGImageDestinationFinalize(destination))
        let path = FileManager.default.temporaryDirectory.appendingPathComponent("\(UUID()).heic")
        try (original as Data).write(to: path)
        defer { try? FileManager.default.removeItem(at: path) }
        let fixture = try Fixture(attachmentPaths: [path.path])
        let database = try MessagesDatabase(path: fixture.path)
        let chat = try #require(database.chats(limit: 1, before: nil).items.first)
        let api = NatterwireAPI(database: database)
        let metadata = api.respond(method: "GET", target: "/messages/\(chat.id)?limit=1&media=metadata")
        let metadataPage = try JSONDecoder().decode(Page<Message>.self, from: metadata.body)
        let info = try #require(metadataPage.items.first?.attachments.first)
        #expect(info.width == 1536)
        #expect(info.height == 2048)
        #expect(info.dataBase64 == nil && info.displayDataBase64 == nil)
        #expect(database.media.cachedBytes == 0)
        let mediaID = try #require(info.mediaID)
        let version = try #require(info.version)
        let binary = api.respond(method: "GET", target: "/attachments/\(mediaID)?version=\(version)")
        #expect(binary.status == 200)
        #expect(binary.contentType == "image/jpeg")
        #expect(database.media.cachedBytes == binary.body.count)
        let response = api.respond(method: "GET", target: "/messages/\(chat.id)?limit=1")
        #expect(response.status == 200)
        let page = try JSONDecoder().decode(Page<Message>.self, from: response.body)
        let attachment = try #require(page.items.first?.attachments.first)
        #expect(attachment.dataBase64 == (original as Data).base64EncodedString())
        let display = try #require(attachment.displayDataBase64.flatMap { Data(base64Encoded: $0) })
        #expect(display == binary.body)
        let source = try #require(CGImageSourceCreateWithData(display as CFData, nil))
        #expect(CGImageSourceGetType(source) as String? == "public.jpeg")
        let decoded = try #require(CGImageSourceCreateImageAtIndex(source, 0, nil))
        #expect(decoded.width == 1536)
        #expect(decoded.height == 2048)
        #expect(Attachment.displayData(display) == nil)
    }

    @Test func metadataBinaryVersionsBudgetAndPathSafety() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let path = directory.appendingPathComponent("image.png")
        let missing = directory.appendingPathComponent("missing.png")
        let large = directory.appendingPathComponent("large.png")
        let context = try #require(CGContext(data: nil, width: 12, height: 8, bitsPerComponent: 8,
                                            bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(),
                                            bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue))
        let image = try #require(context.makeImage())
        let output = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(output, "public.png" as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, nil)
        #expect(CGImageDestinationFinalize(destination))
        let png = output as Data
        try png.write(to: path)
        try Data().write(to: large)
        let file = try FileHandle(forWritingTo: large)
        try file.truncate(atOffset: UInt64(AttachmentMedia.byteLimit + 1))
        try file.close()
        let fixture = try Fixture(attachmentPaths: [path.path, path.path, missing.path, large.path, nil])
        let database = try MessagesDatabase(path: fixture.path)
        let api = NatterwireAPI(database: database)
        let chat = try #require(database.chats(limit: 1, before: nil).items.first)
        var attachments: [Attachment] = []
        for route in ["/messages/\(chat.id)", "/chats/\(chat.id)/messages"] {
            let response = api.respond(method: "GET", target: route + "?media=metadata&limit=1")
            #expect(response.status == 200)
            let json = String(decoding: response.body, as: UTF8.self)
            #expect(!json.contains("dataBase64") && !json.contains("displayDataBase64"))
            #expect(!json.contains(directory.path))
            attachments = try #require(JSONDecoder().decode(Page<Message>.self, from: response.body).items.first).attachments
            #expect(attachments.allSatisfy { $0.mediaID != nil && $0.version != nil })
            #expect(attachments[0].width == 12 && attachments[0].height == 8)
            #expect(attachments[2].version == "missing")
        }
        let id = try #require(attachments[0].mediaID)
        let version = try #require(attachments[0].version)
        let target = "/attachments/\(id)?version=\(version)"
        let binary = api.respond(method: "GET", target: target)
        #expect(binary.status == 200 && binary.contentType == "image/png")
        #expect(binary.body == png)
        #expect(api.respond(method: "GET", target: target).body == png)
        #expect(database.media.cachedBytes == png.count)
        let port = UInt16.random(in: 20_000...50_000)
        let server = HTTPServer(host: "127.0.0.1", port: port, api: api)
        let ready = DispatchSemaphore(value: 0)
        let done = DispatchSemaphore(value: 0)
        let serverResult = ServerResult()
        DispatchQueue.global().async {
            defer { done.signal() }
            do { try server.run { ready.signal() } } catch { serverResult.set(error) }
        }
        defer { server.stop(); _ = done.wait(timeout: .now() + 2) }
        try #require(ready.wait(timeout: .now() + 2) == .success)
        // A stalled peer must not block another connection, unlike the old serial server.
        let stalled = try connect(port: port)
        defer { close(stalled) }
        let wire = try request(port: port, target: target)
        let headerEnd = try #require(wire.range(of: Data("\r\n\r\n".utf8)))
        let header = String(decoding: wire[..<headerEnd.lowerBound], as: UTF8.self)
        #expect(header.contains("HTTP/1.1 200 OK"))
        #expect(header.contains("Content-Type: image/png"))
        #expect(header.contains("Content-Length: \(png.count)"))
        #expect(Data(wire[headerEnd.upperBound...]) == png)
        let textWire = try request(port: port, target: "/chats?limit=1")
        #expect(String(decoding: textWire, as: UTF8.self).contains("HTTP/1.1 200 OK"))
        #expect(serverResult.error == nil)
        #expect(api.respond(method: "GET", target: "/attachments/\(id)").status == 409)
        for badID in ["1", "..%2Fetc%2Fpasswd", Data(path.path.utf8).base64EncodedString(), "YXR0YWNobWVudDo5OTk5"] {
            let response = api.respond(method: "GET", target: "/attachments/\(badID)?version=\(version)")
            #expect(response.status == 404)
            #expect(!String(decoding: response.body, as: UTF8.self).contains(directory.path))
        }
        for index in [2, 3, 4] {
            let mediaID = try #require(attachments[index].mediaID)
            let stamp = try #require(attachments[index].version)
            #expect(api.respond(method: "GET", target: "/attachments/\(mediaID)?version=\(stamp)").status == (index == 3 ? 413 : 404))
        }
        let cache = AttachmentMedia(budget: png.count)
        _ = try cache.response(id: id, path: path.path, version: version)
        _ = try cache.response(id: "second", path: path.path, version: version)
        #expect(cache.cachedBytes == png.count)
        let noCache = AttachmentMedia(budget: png.count - 1)
        #expect(try noCache.response(id: id, path: path.path, version: version).body == png)
        #expect(noCache.cachedBytes == 0)
        try (png + Data([0])).write(to: path)
        #expect(api.respond(method: "GET", target: target).status == 409)
        #expect(database.media.cachedBytes == 0)
        let updated = try #require(database.messages(chatID: chat.id, limit: 1, before: nil, metadataOnly: true).items.first?.attachments.first)
        let newVersion = try #require(updated.version)
        #expect(newVersion != version)
        #expect(api.respond(method: "GET", target: "/attachments/\(id)?version=\(newVersion)").body == png + Data([0]))
        try png.write(to: missing)
        let recovered = try database.messages(chatID: chat.id, limit: 1, before: nil, metadataOnly: true).items[0].attachments[2]
        let recoveredID = try #require(recovered.mediaID)
        let recoveredVersion = try #require(recovered.version)
        #expect(api.respond(method: "GET", target: "/attachments/\(recoveredID)?version=\(recoveredVersion)").status == 200)
        try FileManager.default.removeItem(at: path)
        #expect(api.respond(method: "GET", target: target).status == 404)
    }

    @Test func concurrentMetadataAndChatQueriesKeepStatementsAndEncodersIndependent() throws {
        let fixture = try Fixture(attachmentPaths: [nil])
        let database = try MessagesDatabase(path: fixture.path)
        let api = NatterwireAPI(database: database)
        let id = try #require(database.chats(limit: 1, before: nil).items.first?.id)
        DispatchQueue.concurrentPerform(iterations: 60) { index in
            let target = index.isMultiple(of: 2) ? "/chats?limit=1" : "/messages/\(id)?media=metadata&limit=1"
            let response = api.respond(method: "GET", target: target)
            #expect(response.status == 200)
            if index.isMultiple(of: 2) {
                #expect((try? JSONDecoder().decode(Page<Chat>.self, from: response.body).items.first?.id) == id)
            } else {
                #expect((try? JSONDecoder().decode(Page<Message>.self, from: response.body).items.first?.id) == "image")
            }
        }
    }

    @Test func sendsAttachmentsAsBase64AndKeepsAttachmentOnlyMessages() throws {
        let directory = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("natterwire-test-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let image = Data([0x89, 0x50, 0x4e, 0x47, 0x00, 0xff])
        try image.write(to: directory.appendingPathComponent("image.png"))
        try Data(repeating: 1, count: 10 * 1024 * 1024 + 1)
            .write(to: directory.appendingPathComponent("large.png"))
        let fixture = try Fixture(attachmentPaths: [
            "~/\(directory.lastPathComponent)/image.png",
            directory.appendingPathComponent("missing.png").path,
            directory.appendingPathComponent("large.png").path,
            nil,
        ])
        let database = try MessagesDatabase(path: fixture.path)
        let chat = try #require(database.chats(limit: 1, before: nil).items.first)
        #expect(chat.messageCount == 2)
        let api = NatterwireAPI(database: database)
        for route in ["/chats/\(chat.id)/messages", "/messages/\(chat.id)"] {
            let response = api.respond(method: "GET", target: route + "?limit=1")
            #expect(response.status == 200)
            let page = try JSONDecoder().decode(Page<Message>.self, from: response.body)
            let message = try #require(page.items.first)
            #expect(message.text == "\u{fffc}")
            #expect(message.attachments.map(\.id) == ["a1", "a2", "a3", "a4"])
            #expect(message.attachments[0].filename == "image.png")
            #expect(message.attachments[0].mimeType == "image/png")
            let base64 = try #require(message.attachments[0].dataBase64)
            #expect(Data(base64Encoded: base64) == image)
            #expect(message.attachments.dropFirst().allSatisfy { $0.dataBase64 == nil })
            #expect(!String(decoding: response.body, as: UTF8.self).contains(directory.path))
            let cursor = try #require(page.nextBefore)
            let next = try database.messages(chatID: chat.id, limit: 1, before: cursor)
            #expect(next.items.map(\.text) == [""])
            #expect(next.items.first?.attachments.count == 1)
            #expect(next.nextBefore == nil)
        }
    }

    @Test func listsChatsAndPaginatesDecodedMessages() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let chats = try database.chats(limit: 10, before: nil)
        #expect(chats.items.count == 10)
        #expect(chats.items[0].displayName == "Fixture Chat")
        #expect(chats.items[0].messageCount == 3)

        let first = try database.messages(chatID: chats.items[0].id, limit: 1, before: nil)
        #expect(first.items.map(\.text) == ["third from archive"])
        #expect(first.items[0].attachments.isEmpty)
        #expect(first.nextBefore != nil)
        let second = try database.messages(chatID: chats.items[0].id, limit: 10, before: first.nextBefore)
        #expect(second.items.map(\.text) == ["second", "first"])
    }

    @Test func resolvesMessageSendersAndFallsBackToHandles() throws {
        let fixture = try Fixture()
        let resolver = ChatNameResolver(
            emailLookup: { $0 == "fixture@example.invalid" ? "Fixture Friend" : nil },
            phoneLookup: { $0 == "+14155550100" ? "Phone Friend" : nil })
        let database = try MessagesDatabase(path: fixture.path, nameResolver: resolver)
        let fallback = try MessagesDatabase(path: fixture.path)
        for chat in try database.chats(limit: 100, before: nil).items {
            let messages = try database.messages(chatID: chat.id, limit: 100, before: nil).items
            let original = try fallback.messages(chatID: chat.id, limit: 100, before: nil).items
            for (message, raw) in zip(messages, original) {
                #expect(message.sender == raw.sender.map { resolver.name(for: $0) ?? $0 })
                if message.id == "m1" { #expect(message.sender == "Fixture Friend") }
                if message.id == "m2" { #expect(message.sender == nil) }
                if message.id == "phone-chat" { #expect(message.sender == "Phone Friend") }
                if message.id == "group-chat" { #expect(message.sender == "group-one@example.invalid") }
            }
        }
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

    @Test func paginatesPinsInSavedOrderThenUsesRecency() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(
            path: fixture.path,
            pinnedChatIdentifiers: ["+1 (415) 555-0100", "group-id"])
        let first = try database.chats(limit: 1, before: nil)
        let firstCursor = try #require(first.nextBefore)
        let second = try database.chats(limit: 1, before: firstCursor)
        let secondCursor = try #require(second.nextBefore)
        let third = try database.chats(limit: 1, before: secondCursor)

        #expect(first.items.map(\.displayName) == ["+1 (415) 555-0100"])
        #expect(second.items.map(\.displayName) == ["group-one@example.invalid"])
        #expect(third.items.map(\.displayName) == ["Fixture Chat"])
        #expect(Set(first.items.map(\.id) + second.items.map(\.id) + third.items.map(\.id)).count == 3)
    }

    @Test func acceptsLegacyRecencyCursor() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path, pinnedChatIdentifiers: ["group-id"])
        let legacy = Data("300:1".utf8).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
        let page = try database.chats(limit: 1, before: legacy)
        #expect(page.items.map(\.displayName) == ["Older Chat"])
    }

    @Test func filtersReactionsActionsAndDeletedRows() throws {
        let fixture = try Fixture()
        let database = try MessagesDatabase(path: fixture.path)
        let chat = try #require(database.chats(limit: 1, before: nil).items.first)
        let messages = try database.messages(chatID: chat.id, limit: 100, before: nil)
        #expect(messages.items.count == 3)
    }

    @Test func classifiesLiveChatFormsAndResolvesDirectNames() throws {
        let fixture = try Fixture()
        let resolver = ChatNameResolver(
            emailLookup: {
                [
                    "friend@example.invalid": "Email Friend",
                    "group-one@example.invalid": "First Participant",
                    "group-two@example.invalid": "Second Participant",
                ][$0]
            },
            phoneLookup: { $0 == "+14155550100" ? "Phone Friend" : nil })
        let resolved = try MessagesDatabase(path: fixture.path, nameResolver: resolver)
            .chats(limit: 20, before: nil).items.map(\.displayName)
        #expect(resolved.contains("Fixture Chat"))
        #expect(resolved.contains("Email Friend"))
        #expect(!resolved.contains("Stale Email Name"))
        #expect(resolved.contains("Phone Friend"))
        #expect(resolved.contains("missing@example.invalid"))
        #expect(resolved.contains("First Participant"))
        #expect(resolved.contains("First Participant, Second Participant"))
        #expect(resolved.contains("Named Group"))
        #expect(resolved.allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty })

        let unavailable = try MessagesDatabase(path: fixture.path)
            .chats(limit: 20, before: nil).items.map(\.displayName)
        #expect(unavailable.contains("Stale Email Name"))
        #expect(unavailable.contains("+1 (415) 555-0100"))
        #expect(unavailable.contains("denied@example.invalid"))
        #expect(unavailable.contains("group-one@example.invalid"))
        #expect(unavailable.contains("group-one@example.invalid, group-two@example.invalid"))
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

    @Test func contactsSnapshotLoadsOnlyOnceForBulkNaming() {
        let loads = Counter()
        let lookup = ContactsNameLookup(
            authorizationStatus: { true },
            snapshotLoader: {
                loads.increment()
                return (["friend@example.invalid": "Email Friend"], ["+14155550100": "Phone Friend"])
            })
        let resolver = lookup.resolver

        for _ in 0..<100 {
            #expect(resolver.name(for: "friend@example.invalid") == "Email Friend")
            #expect(resolver.name(for: "+1 (415) 555-0100") == "Phone Friend")
        }
        #expect(loads.value == 1)
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

    private func connect(port: UInt16) throws -> Int32 {
        let client = socket(AF_INET, SOCK_STREAM, 0)
        try #require(client >= 0)
        var timeout = timeval(tv_sec: 2, tv_usec: 0)
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        var noSigPipe: Int32 = 1
        setsockopt(client, SOL_SOCKET, SO_NOSIGPIPE, &noSigPipe, socklen_t(MemoryLayout.size(ofValue: noSigPipe)))
        var address = sockaddr_in()
        address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = port.bigEndian
        inet_pton(AF_INET, "127.0.0.1", &address.sin_addr)
        let result = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(client, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        if result != 0 { close(client) }
        try #require(result == 0)
        return client
    }

    private func request(port: UInt16, target: String) throws -> Data {
        let client = try connect(port: port)
        defer { close(client) }
        let request = Data("GET \(target) HTTP/1.1\r\nHost: localhost\r\n\r\n".utf8)
        let sent = request.withUnsafeBytes { Darwin.send(client, $0.baseAddress!, $0.count, 0) }
        try #require(sent == request.count)
        var response = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while true {
            let count = recv(client, &buffer, buffer.count, 0)
            try #require(count >= 0)
            if count == 0 { return response }
            response.append(contentsOf: buffer[..<count])
        }
    }
}

private final class Fixture {
    let path: String

    init(attachmentPaths: [String?] = []) throws {
        path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".db").path
        var db: OpaquePointer?
        guard sqlite3_open(path, &db) == SQLITE_OK, let db else { throw FixtureError.create }
        defer { sqlite3_close(db) }
        let schema = """
            CREATE TABLE chat (ROWID INTEGER PRIMARY KEY, guid TEXT, display_name TEXT, service_name TEXT, chat_identifier TEXT, style INTEGER, is_archived INTEGER, is_deleted INTEGER);
            CREATE TABLE message (ROWID INTEGER PRIMARY KEY, guid TEXT, text TEXT, attributedBody BLOB, date INTEGER, is_from_me INTEGER, handle_id INTEGER, service TEXT, item_type INTEGER, group_action_type INTEGER, associated_message_type INTEGER, is_deleted INTEGER);
            CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER);
            CREATE TABLE chat_handle_join (chat_id INTEGER, handle_id INTEGER);
            CREATE TABLE handle (ROWID INTEGER PRIMARY KEY, id TEXT);
            INSERT INTO chat VALUES (1, 'iMessage;-;fixture@example.invalid', 'Fixture Chat', 'iMessage', 'fixture@example.invalid', 43, 0, 0);
            INSERT INTO chat VALUES (2, 'iMessage;-;older@example.invalid', 'Older Chat', 'iMessage', 'older@example.invalid', 43, 0, 0);
            INSERT INTO chat VALUES (3, 'iMessage;-;body-edge-cases', 'Body Edge Cases', 'iMessage', 'body-edge-cases', 43, 0, 0);
            INSERT INTO chat VALUES (4, 'any;-;friend@example.invalid', 'Stale Email Name', 'iMessage', 'friend@example.invalid', 45, 0, 0);
            INSERT INTO chat VALUES (5, 'any;-;+1 (415) 555-0100', '', 'iMessage', '+1 (415) 555-0100', 45, 0, 0);
            INSERT INTO chat VALUES (6, 'any;-;missing@example.invalid', NULL, 'iMessage', NULL, 43, 0, 0);
            INSERT INTO chat VALUES (7, 'iMessage;+;group-id', '   ', 'iMessage', 'group-id', 45, 0, 0);
            INSERT INTO chat VALUES (8, 'iMessage;+;named-group-id', 'Named Group', 'iMessage', 'named-group-id', 43, 0, 0);
            INSERT INTO chat VALUES (9, 'any;-;denied@example.invalid', NULL, 'iMessage', 'denied@example.invalid', 45, 0, 0);
            INSERT INTO chat VALUES (10, 'iMessage;-;multi-participant-id', '', 'iMessage', 'multi-participant-id', 43, 0, 0);
            INSERT INTO handle VALUES (1, 'fixture@example.invalid');
            INSERT INTO handle VALUES (2, 'second@example.invalid');
            INSERT INTO handle VALUES (3, 'group-one@example.invalid');
            INSERT INTO handle VALUES (4, 'group-two@example.invalid');
            INSERT INTO handle VALUES (5, '+1 (415) 555-0100');
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
            INSERT INTO message VALUES (12, 'phone-chat', 'phone', NULL, 35, 0, 5, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (13, 'missing-chat', 'missing', NULL, 30, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (14, 'group-chat', 'group', NULL, 5, 0, 3, 'iMessage', 0, 0, 0, 0);
            INSERT INTO chat_message_join VALUES (4, 11), (5, 12), (6, 13), (7, 14);
            INSERT INTO message VALUES (15, 'named-group-chat', 'named group', NULL, 4, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (16, 'denied-chat', 'denied', NULL, 3, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO message VALUES (17, 'multi-chat', 'multi', NULL, 2, 0, 1, 'iMessage', 0, 0, 0, 0);
            INSERT INTO chat_message_join VALUES (8, 15), (9, 16), (10, 17);
            INSERT INTO chat_handle_join VALUES (1, 1), (2, 1), (3, 1), (4, 1), (5, 1), (6, 1), (7, 3), (8, 3), (9, 1), (10, 3), (10, 4);
            """
        guard sqlite3_exec(db, schema, nil, nil, nil) == SQLITE_OK else { throw FixtureError.create }
        let archive = NSArchiver.archivedData(withRootObject: NSAttributedString(string: "third from archive"))
        var statement: OpaquePointer?
        sqlite3_prepare_v2(db, "INSERT INTO message VALUES (3, 'm3', NULL, ?, 300, 0, 1, 'iMessage', 0, 0, 0, 0)", -1, &statement, nil)
        _ = archive.withUnsafeBytes { sqlite3_bind_blob(statement, 1, $0.baseAddress, Int32($0.count), unsafeBitCast(-1, to: sqlite3_destructor_type.self)) }
        guard sqlite3_step(statement) == SQLITE_DONE else { throw FixtureError.create }
        sqlite3_finalize(statement)
        guard sqlite3_exec(db, "INSERT INTO chat_message_join VALUES (1, 3)", nil, nil, nil) == SQLITE_OK else { throw FixtureError.create }
        if !attachmentPaths.isEmpty {
            let attachments = """
                CREATE TABLE attachment (ROWID INTEGER PRIMARY KEY, guid TEXT, filename TEXT, transfer_name TEXT, mime_type TEXT);
                CREATE TABLE message_attachment_join (message_id INTEGER, attachment_id INTEGER);
                INSERT INTO chat VALUES (11, 'iMessage;-;images', 'Images', 'iMessage', 'images', 43, 0, 0);
                INSERT INTO message VALUES (18, 'image', '\u{fffc}', NULL, 700, 0, 1, 'iMessage', 0, 0, 0, 0);
                INSERT INTO message VALUES (19, 'image-only', NULL, NULL, 650, 0, 1, 'iMessage', 0, 0, 0, 0);
                INSERT INTO chat_message_join VALUES (11, 18), (11, 19);
                INSERT INTO message_attachment_join VALUES (19, 1);
                """
            guard sqlite3_exec(db, attachments, nil, nil, nil) == SQLITE_OK else { throw FixtureError.create }
            for (index, path) in attachmentPaths.enumerated() {
                var attachment: OpaquePointer?
                sqlite3_prepare_v2(db, "INSERT INTO attachment VALUES (?, ?, ?, NULL, 'image/png')", -1, &attachment, nil)
                defer { sqlite3_finalize(attachment) }
                let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)
                sqlite3_bind_int64(attachment, 1, Int64(index + 1))
                sqlite3_bind_text(attachment, 2, "a\(index + 1)", -1, transient)
                if let path { sqlite3_bind_text(attachment, 3, path, -1, transient) }
                guard sqlite3_step(attachment) == SQLITE_DONE,
                      sqlite3_exec(db, "INSERT INTO message_attachment_join VALUES (18, \(index + 1))", nil, nil, nil) == SQLITE_OK
                else { throw FixtureError.create }
            }
        }
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

private final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0

    var value: Int {
        lock.lock(); defer { lock.unlock() }
        return count
    }

    func increment() {
        lock.lock(); count += 1; lock.unlock()
    }
}
