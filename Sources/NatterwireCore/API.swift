import Foundation

public struct HTTPResponse: Sendable {
    public let status: Int
    public let body: Data
    public let contentType: String

    public init(status: Int, body: Data, contentType: String = "application/json") {
        self.status = status
        self.body = body
        self.contentType = contentType
    }
}

public final class NatterwireAPI: Sendable {
    private let database: MessagesDatabase

    public init(database: MessagesDatabase) {
        self.database = database
    }

    public func respond(method: String, target: String) -> HTTPResponse {
        guard method == "GET" else { return json(status: 405, ErrorBody(error: "method not allowed")) }
        guard let components = URLComponents(string: target) else {
            return json(status: 400, ErrorBody(error: "invalid request target"))
        }
        let parts = components.path.split(separator: "/").map(String.init)
        var query: [String: String] = [:]
        for item in components.queryItems ?? [] where query[item.name] == nil {
            query[item.name] = item.value ?? ""
        }
        let limit: Int
        if let rawLimit = query["limit"] {
            guard let parsedLimit = Int(rawLimit), (1...100).contains(parsedLimit) else {
                return json(status: 400, ErrorBody(error: "limit must be an integer between 1 and 100"))
            }
            limit = parsedLimit
        } else {
            limit = 50
        }
        do {
            if parts.count == 2, parts[0] == "attachments" {
                guard let path = try database.attachmentPath(mediaID: parts[1]) else {
                    return json(status: 404, ErrorBody(error: "not found"))
                }
                return try database.media.response(id: parts[1], path: path, version: query["version"])
            }
            if parts == ["chats"] {
                return json(status: 200, try database.chats(limit: limit, before: query["before"]))
            }
            if parts.count == 3, parts[0] == "chats", parts[2] == "messages" {
                return json(status: 200, try database.messages(chatID: parts[1], limit: limit, before: query["before"], metadataOnly: query["media"] == "metadata"))
            }
            if parts.count == 2, parts[0] == "messages" {
                return json(status: 200, try database.messages(chatID: parts[1], limit: limit, before: query["before"], metadataOnly: query["media"] == "metadata"))
            }
            return json(status: 404, ErrorBody(error: "not found"))
        } catch AttachmentMediaError.notFound {
            return json(status: 404, ErrorBody(error: "not found"))
        } catch AttachmentMediaError.tooLarge {
            return json(status: 413, ErrorBody(error: "attachment too large"))
        } catch AttachmentMediaError.changed {
            return json(status: 409, ErrorBody(error: "attachment version changed"))
        } catch MessagesDatabaseError.invalidIdentifier {
            return json(status: 400, ErrorBody(error: "invalid chat identifier"))
        } catch MessagesDatabaseError.invalidCursor {
            return json(status: 400, ErrorBody(error: "invalid pagination cursor"))
        } catch {
            return json(status: 500, ErrorBody(error: "database query failed"))
        }
    }

    private func json<T: Encodable>(status: Int, _ value: T) -> HTTPResponse {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return HTTPResponse(status: status, body: (try? encoder.encode(value)) ?? Data(#"{"error":"encoding failed"}"#.utf8))
    }
}

private struct ErrorBody: Codable { let error: String }
