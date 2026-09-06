// Run interactively with osascript -l JavaScript. macOS may ask the terminal
// for permission to automate Contacts. Output is private; store it with mode 600.
const contacts = Application('Contacts');
const result = {};
for (const person of contacts.people()) {
    const name = person.name();
    if (!name) continue;
    for (const handle of person.emails.value().concat(person.phones.value())) {
        if (!Object.prototype.hasOwnProperty.call(result, handle)) result[handle] = name;
    }
}
JSON.stringify(result);
