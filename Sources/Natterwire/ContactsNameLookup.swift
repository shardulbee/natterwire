import Contacts
import NatterwireCore

final class ContactsNameLookup: @unchecked Sendable {
    private let store = CNContactStore()

    func requestAccessIfNeeded() {
        guard CNContactStore.authorizationStatus(for: .contacts) == .notDetermined else { return }
        store.requestAccess(for: .contacts) { _, _ in }
    }

    var resolver: ChatNameResolver {
        ChatNameResolver(
            emailLookup: { [weak self] email in
                self?.name(matching: CNContact.predicateForContacts(matchingEmailAddress: email))
            },
            phoneLookup: { [weak self] phone in
                self?.name(matching: CNContact.predicateForContacts(
                    matching: CNPhoneNumber(stringValue: phone)))
            })
    }

    private func name(matching predicate: NSPredicate) -> String? {
        guard CNContactStore.authorizationStatus(for: .contacts) == .authorized else { return nil }
        let keys = [CNContactFormatter.descriptorForRequiredKeys(for: .fullName)]
        guard let contacts = try? store.unifiedContacts(matching: predicate, keysToFetch: keys) else {
            return nil
        }
        return contacts.lazy.compactMap {
            CNContactFormatter.string(from: $0, style: .fullName)
        }.first { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    }
}
