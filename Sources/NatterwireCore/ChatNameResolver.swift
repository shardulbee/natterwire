import Foundation

public struct ChatNameResolver: Sendable {
    public static let unavailable = ChatNameResolver(
        emailLookup: { _ in nil },
        phoneLookup: { _ in nil })

    private let emailLookup: @Sendable (String) -> String?
    private let phoneLookup: @Sendable (String) -> String?

    public init(
        emailLookup: @escaping @Sendable (String) -> String?,
        phoneLookup: @escaping @Sendable (String) -> String?
    ) {
        self.emailLookup = emailLookup
        self.phoneLookup = phoneLookup
    }

    public func name(for handle: String) -> String? {
        let handle = handle.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !handle.isEmpty else { return nil }
        let match: String?
        if handle.contains("@") {
            match = emailLookup(handle.lowercased())
        } else if let phone = Self.normalizedPhone(handle) {
            match = phoneLookup(phone)
        } else {
            match = nil
        }
        guard let name = match?.trimmingCharacters(in: .whitespacesAndNewlines),
              !name.isEmpty else { return nil }
        return name
    }

    private static func normalizedPhone(_ value: String) -> String? {
        let digits = value.filter(\.isNumber)
        guard digits.count >= 7 else { return nil }
        return value.trimmingCharacters(in: .whitespaces).hasPrefix("+") ? "+\(digits)" : digits
    }
}
