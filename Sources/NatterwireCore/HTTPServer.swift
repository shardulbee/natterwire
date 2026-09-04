import Darwin
import Foundation

public final class HTTPServer: @unchecked Sendable {
    private let host: String
    private let port: UInt16
    private let api: NatterwireAPI
    private let lifecycleLock = NSLock()
    private var listener: Int32 = -1
    private var stopping = false

    public init(host: String, port: UInt16, api: NatterwireAPI) {
        self.host = host
        self.port = port
        self.api = api
    }

    public func run(onReady: @Sendable () -> Void = {}) throws {
        let descriptor = socket(AF_INET, SOCK_STREAM, 0)
        guard descriptor >= 0 else { throw ServerError.current("socket") }
        lifecycleLock.lock()
        stopping = false
        listener = descriptor
        lifecycleLock.unlock()
        defer {
            lifecycleLock.lock()
            let ownsDescriptor = listener == descriptor
            if ownsDescriptor { listener = -1 }
            lifecycleLock.unlock()
            if ownsDescriptor { close(descriptor) }
        }
        var reuse: Int32 = 1
        setsockopt(descriptor, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout.size(ofValue: reuse)))
        var address = sockaddr_in()
        address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = port.bigEndian
        guard inet_pton(AF_INET, host, &address.sin_addr) == 1 else {
            throw ServerError.invalidHost
        }
        let bindResult = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.bind(descriptor, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bindResult == 0 else { throw ServerError.current("bind") }
        guard listen(descriptor, 16) == 0 else { throw ServerError.current("listen") }
        onReady()
        while true {
            let client = accept(descriptor, nil, nil)
            if client < 0 {
                lifecycleLock.lock()
                let shouldStop = stopping
                lifecycleLock.unlock()
                if shouldStop { return }
                if errno == EINTR { continue }
                throw ServerError.current("accept")
            }
            autoreleasepool {
                handle(client)
                close(client)
            }
        }
    }

    public func stop() {
        lifecycleLock.lock()
        stopping = true
        let descriptor = listener
        listener = -1
        lifecycleLock.unlock()
        if descriptor >= 0 { close(descriptor) }
    }

    private func handle(_ client: Int32) {
        var timeout = timeval(tv_sec: 5, tv_usec: 0)
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4_096)
        while data.range(of: Data("\r\n\r\n".utf8)) == nil && data.count < 16_384 {
            let count = recv(client, &buffer, min(buffer.count, 16_384 - data.count), 0)
            if count <= 0 { break }
            data.append(contentsOf: buffer[..<count])
        }
        guard let request = String(data: data, encoding: .utf8),
              let headerEnd = request.range(of: "\r\n\r\n") else {
            write(client, response: HTTPResponse(status: 400, body: Data(#"{"error":"bad request"}"#.utf8)))
            return
        }
        let lines = request[..<headerEnd.lowerBound].components(separatedBy: "\r\n")
        let requestLine = lines.first?.split(separator: " ").map(String.init) ?? []
        guard requestLine.count == 3 else {
            write(client, response: HTTPResponse(status: 400, body: Data(#"{"error":"bad request"}"#.utf8)))
            return
        }
        var authorization: String?
        for line in lines.dropFirst() {
            guard let colon = line.firstIndex(of: ":") else { continue }
            if line[..<colon].trimmingCharacters(in: .whitespaces).lowercased() == "authorization" {
                authorization = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            }
        }
        write(client, response: api.respond(method: requestLine[0], target: requestLine[1], authorization: authorization))
    }

    private func write(_ client: Int32, response: HTTPResponse) {
        let reason = [200: "OK", 400: "Bad Request", 401: "Unauthorized", 404: "Not Found", 405: "Method Not Allowed", 500: "Internal Server Error"][response.status] ?? "Error"
        var data = Data("HTTP/1.1 \(response.status) \(reason)\r\nContent-Type: application/json\r\nContent-Length: \(response.body.count)\r\nConnection: close\r\n\r\n".utf8)
        data.append(response.body)
        data.withUnsafeBytes { bytes in
            var sent = 0
            while sent < bytes.count {
                let result = Darwin.send(client, bytes.baseAddress!.advanced(by: sent), bytes.count - sent, 0)
                if result <= 0 { break }
                sent += result
            }
        }
    }
}

private enum ServerError: Error, CustomStringConvertible {
    case invalidHost
    case system(String, Int32)

    static func current(_ operation: String) -> ServerError {
        .system(operation, errno)
    }

    var description: String {
        switch self {
        case .invalidHost: "host must be an IPv4 address"
        case .system(let operation, let code): "\(operation) failed: \(String(cString: strerror(code)))"
        }
    }
}
