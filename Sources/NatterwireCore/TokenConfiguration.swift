import Darwin
import Foundation

public enum TokenConfigurationError: Error, CustomStringConvertible {
    case missing
    case invalid
    case unsafeFile

    public var description: String {
        switch self {
        case .missing: "set NATTERWIRE_API_TOKEN or NATTERWIRE_API_TOKEN_FILE"
        case .invalid: "API token must be valid UTF-8 and at least 24 bytes"
        case .unsafeFile: "token file must be a regular, owner-only file owned by the current user"
        }
    }
}

public enum TokenConfiguration {
    public static func load(
        environment: [String: String],
        defaultFilePath: String? = nil
    ) throws -> String {
        for key in ["NATTERWIRE_API_TOKEN", "MESSAGES_API_TOKEN"] {
            if let token = environment[key] { return try validate(token) }
        }
        let configuredPath = environment["NATTERWIRE_API_TOKEN_FILE"]
            ?? environment["MESSAGES_API_TOKEN_FILE"]
        let path: String
        if let configuredPath {
            guard !configuredPath.isEmpty else { throw TokenConfigurationError.missing }
            path = configuredPath
        } else {
            path = defaultFilePath ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support/natterwire/token").path
        }

        let descriptor = open(path, O_RDONLY | O_NOFOLLOW)
        guard descriptor >= 0 else { throw TokenConfigurationError.unsafeFile }
        defer { close(descriptor) }
        var status = stat()
        guard fstat(descriptor, &status) == 0,
              status.st_mode & S_IFMT == S_IFREG,
              status.st_uid == getuid(),
              status.st_mode & 0o077 == 0 else {
            throw TokenConfigurationError.unsafeFile
        }
        var data = try FileHandle(fileDescriptor: descriptor, closeOnDealloc: false).readToEnd() ?? Data()
        if data.last == 0x0a { data.removeLast() }
        guard let token = String(data: data, encoding: .utf8) else {
            throw TokenConfigurationError.invalid
        }
        return try validate(token)
    }

    private static func validate(_ token: String) throws -> String {
        guard token.utf8.count >= 24 else { throw TokenConfigurationError.invalid }
        return token
    }
}
