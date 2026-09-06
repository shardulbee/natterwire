//go:build darwin && cgo

#import <Contacts/Contacts.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

static CNContactStore *store;
static NSLock *lock;
static id observer;
static BOOL dirty = YES;
static CNAuthorizationStatus previousStatus = CNAuthorizationStatusNotDetermined;

static void changed(void) {
    [lock lock];
    dirty = YES;
    [lock unlock];
}

int nw_contacts_start(void) {
    @autoreleasepool {
        if (![[NSBundle mainBundle] objectForInfoDictionaryKey:@"NSContactsUsageDescription"]) return 0;
        static dispatch_once_t once;
        dispatch_once(&once, ^{
            lock = [NSLock new];
            store = [CNContactStore new];
            observer = [[NSNotificationCenter defaultCenter]
                addObserverForName:CNContactStoreDidChangeNotification object:nil queue:nil
                usingBlock:^(NSNotification *note) { changed(); }];
            if ([CNContactStore authorizationStatusForEntityType:CNEntityTypeContacts] == CNAuthorizationStatusNotDetermined) {
                [store requestAccessForEntityType:CNEntityTypeContacts completionHandler:^(BOOL granted, NSError *error) {
                    changed();
                }];
            }
        });
        return 1;
    }
}

// NULL means unchanged. An empty dictionary clears names after access is revoked.
// Clear dirty before reading so changes during enumeration trigger another read.
char *nw_contacts_snapshot(void) {
    @autoreleasepool {
        CNAuthorizationStatus status = [CNContactStore authorizationStatusForEntityType:CNEntityTypeContacts];
        [lock lock];
        BOOL reload = dirty || status != previousStatus;
        dirty = NO;
        previousStatus = status;
        [lock unlock];
        if (!reload) return NULL;
        if (status == CNAuthorizationStatusNotDetermined || status == CNAuthorizationStatusRestricted ||
            status == CNAuthorizationStatusDenied) return strdup("{}");
        NSMutableDictionary<NSString *, NSString *> *names = [NSMutableDictionary new];
        NSArray *keys = @[[CNContactFormatter descriptorForRequiredKeysForStyle:CNContactFormatterStyleFullName],
                          CNContactEmailAddressesKey, CNContactPhoneNumbersKey];
        CNContactFetchRequest *request = [[CNContactFetchRequest alloc] initWithKeysToFetch:keys];
        NSError *error = nil;
        BOOL success = [store enumerateContactsWithFetchRequest:request error:&error usingBlock:^(CNContact *contact, BOOL *stop) {
            NSString *name = [CNContactFormatter stringFromContact:contact style:CNContactFormatterStyleFullName];
            if (!name.length) return;
            for (CNLabeledValue<NSString *> *email in contact.emailAddresses) {
                if (!names[email.value]) names[email.value] = name;
            }
            for (CNLabeledValue<CNPhoneNumber *> *phone in contact.phoneNumbers) {
                NSString *number = phone.value.stringValue;
                if (!names[number]) names[number] = name;
            }
        }];
        if (!success) { changed(); return strdup("{}"); }
        NSData *json = [NSJSONSerialization dataWithJSONObject:names options:0 error:NULL];
        NSString *text = [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding];
        return strdup(text.UTF8String ?: "{}");
    }
}
