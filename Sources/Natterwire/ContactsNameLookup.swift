import Contacts
import Foundation
import NatterwireCore

final class ContactsNameLookup: @unchecked Sendable {
    typealias Snapshot = (emails: [String: String], phones: [String: String])

    private let store: CNContactStore?
    private let authorizationStatus: () -> Bool
    private let snapshotLoader: () -> Snapshot
    private let lock = NSLock()
    private var snapshot: Snapshot?

    init() {
        let store = CNContactStore()
        self.store = store
        authorizationStatus = { CNContactStore.authorizationStatus(for: .contacts) == .authorized }
        snapshotLoader = { Self.loadSnapshot(from: store) }
    }

    init(
        authorizationStatus: @escaping () -> Bool,
        snapshotLoader: @escaping () -> Snapshot
    ) {
        store = nil
        self.authorizationStatus = authorizationStatus
        self.snapshotLoader = snapshotLoader
    }

    func requestAccessIfNeeded() {
        lock.lock(); snapshot = nil; lock.unlock()
        guard CNContactStore.authorizationStatus(for: .contacts) == .notDetermined else { return }
        store?.requestAccess(for: .contacts) { _, _ in }
    }

    var resolver: ChatNameResolver {
        ChatNameResolver(
            emailLookup: { [weak self] email in
                self?.currentSnapshot()?.emails[email]
            },
            phoneLookup: { [weak self] phone in
                guard let phones = self?.currentSnapshot()?.phones else { return nil }
                return Self.phoneKeys(phone).lazy.compactMap { phones[$0] }.first
            })
    }

    private func currentSnapshot() -> Snapshot? {
        guard authorizationStatus() else { return nil }
        lock.lock()
        defer { lock.unlock() }
        if let snapshot { return snapshot }
        let loaded = snapshotLoader()
        snapshot = loaded
        return loaded
    }

    private static func loadSnapshot(from store: CNContactStore) -> Snapshot {
        let keys: [CNKeyDescriptor] = [
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
            CNContactEmailAddressesKey as CNKeyDescriptor,
            CNContactPhoneNumbersKey as CNKeyDescriptor,
        ]
        let request = CNContactFetchRequest(keysToFetch: keys)
        var emails: [String: String] = [:]
        var phones: [String: String] = [:]
        try? store.enumerateContacts(with: request) { contact, _ in
            guard let name = CNContactFormatter.string(from: contact, style: .fullName)?.trimmingCharacters(
                in: .whitespacesAndNewlines), !name.isEmpty else { return }
            for address in contact.emailAddresses {
                let key = String(address.value).trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                if !key.isEmpty, emails[key] == nil { emails[key] = name }
            }
            for number in contact.phoneNumbers {
                for key in phoneKeys(number.value.stringValue) where phones[key] == nil {
                    phones[key] = name
                }
            }
        }
        return (emails, phones)
    }

    private static func normalizedPhone(_ value: String) -> String? {
        let digits = value.filter(\.isNumber)
        guard digits.count >= 7 else { return nil }
        return value.trimmingCharacters(in: .whitespaces).hasPrefix("+") ? "+\(digits)" : digits
    }

    private static func phoneKeys(_ value: String) -> [String] {
        guard let normalized = normalizedPhone(value) else { return [] }
        let digits = normalized.filter(\.isNumber)
        return [normalized, digits, digits.count > 10 ? String(digits.suffix(10)) : nil]
            .compactMap { $0 }.reduce(into: []) {
                if !$0.contains($1) { $0.append($1) }
            }
    }
}
