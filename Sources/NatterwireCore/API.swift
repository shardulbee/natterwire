import Foundation

public struct HTTPResponse: Sendable {
    public let status: Int
    public let body: Data

    public init(status: Int, body: Data) {
        self.status = status
        self.body = body
    }
}

public final class NatterwireAPI: Sendable {
    private let database: MessagesDatabase
    private let encoder: JSONEncoder

    public init(database: MessagesDatabase) {
        self.database = database
        encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
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
            if parts == ["chats"] {
                return json(status: 200, try database.chats(limit: limit, before: query["before"]))
            }
            if parts.count == 3, parts[0] == "chats", parts[2] == "messages" {
                return json(status: 200, try database.messages(chatID: parts[1], limit: limit, before: query["before"]))
            }
            if parts.count == 2, parts[0] == "messages" {
                return json(status: 200, try database.messages(chatID: parts[1], limit: limit, before: query["before"]))
            }
            return json(status: 404, ErrorBody(error: "not found"))
        } catch MessagesDatabaseError.invalidIdentifier {
            return json(status: 400, ErrorBody(error: "invalid chat identifier"))
        } catch MessagesDatabaseError.invalidCursor {
            return json(status: 400, ErrorBody(error: "invalid pagination cursor"))
        } catch {
            return json(status: 500, ErrorBody(error: "database query failed"))
        }
    }

    private func json<T: Encodable>(status: Int, _ value: T) -> HTTPResponse {
        HTTPResponse(status: status, body: (try? encoder.encode(value)) ?? Data(#"{"error":"encoding failed"}"#.utf8))
    }
}

private struct ErrorBody: Codable { let error: String }
