//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <Contacts/Contacts.h>

@interface NatterwireDelegate : NSObject <NSApplicationDelegate>
@property(strong) NSWindow *window;
@property(strong) NSTextField *status;
@property(strong) NSTextField *detail;
@property(strong) NSTextField *messagesAccess;
@property(strong) NSTextField *contactsAccess;
@end

@implementation NatterwireDelegate
- (void)showWindow {
    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}
- (void)applicationDidFinishLaunching:(NSNotification *)note { [self showWindow]; }
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app { return NO; }
- (void)refreshContactsStatus {
    if (![[NSBundle mainBundle] objectForInfoDictionaryKey:@"NSContactsUsageDescription"]) {
        self.contactsAccess.stringValue = @"○ Contacts: unavailable";
        return;
    }
    CNAuthorizationStatus status = [CNContactStore authorizationStatusForEntityType:CNEntityTypeContacts];
    BOOL allowed = status != CNAuthorizationStatusNotDetermined && status != CNAuthorizationStatusDenied && status != CNAuthorizationStatusRestricted;
    self.contactsAccess.stringValue = allowed ? @"✓ Contacts: approved" : @"○ Contacts: not approved (optional)";
    self.contactsAccess.textColor = allowed ? [NSColor systemGreenColor] : [NSColor secondaryLabelColor];
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)app hasVisibleWindows:(BOOL)visible {
    [self showWindow];
    return YES;
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)app {
    [NSApp stop:nil];
    [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint
        modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0] atStart:NO];
    return NSTerminateCancel;
}
- (void)openPrivacy:(id)sender {
    [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"]];
}
- (void)openContacts:(id)sender {
    [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_Contacts"]];
}
@end

static NatterwireDelegate *delegate;

int nw_app_prepare(void) {
    @autoreleasepool {
        if (![[[NSBundle mainBundle] objectForInfoDictionaryKey:@"CFBundlePackageType"] isEqual:@"APPL"]) return 0;
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        delegate = [NatterwireDelegate new];
        NSApp.delegate = delegate;
        NSMenu *menu = [NSMenu new];
        NSMenuItem *item = [NSMenuItem new];
        [menu addItem:item];
        NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Natterwire"];
        [appMenu addItemWithTitle:@"Quit Natterwire" action:@selector(terminate:) keyEquivalent:@"q"];
        item.submenu = appMenu;
        NSApp.mainMenu = menu;

        delegate.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,480,360)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
            backing:NSBackingStoreBuffered defer:NO];
        delegate.window.title = @"Natterwire";
        delegate.window.releasedWhenClosed = NO;
        [delegate.window center];
        NSView *content = delegate.window.contentView;
        NSImageView *icon = [[NSImageView alloc] initWithFrame:NSMakeRect(24,268,64,64)];
        icon.image = [NSApp applicationIconImage];
        [content addSubview:icon];
        delegate.status = [NSTextField labelWithString:@"Starting Natterwire"];
        delegate.status.font = [NSFont boldSystemFontOfSize:20];
        delegate.status.frame = NSMakeRect(104,288,350,28);
        [content addSubview:delegate.status];
        delegate.detail = [NSTextField wrappingLabelWithString:@"Checking Messages access and requesting Contacts permission."];
        delegate.detail.frame = NSMakeRect(24,196,432,60);
        [content addSubview:delegate.detail];
        delegate.messagesAccess = [NSTextField labelWithString:@"○ Full Disk Access: checking Messages…"];
        delegate.messagesAccess.frame = NSMakeRect(24,157,432,24);
        [content addSubview:delegate.messagesAccess];
        delegate.contactsAccess = [NSTextField labelWithString:@"○ Contacts: checking…"];
        delegate.contactsAccess.frame = NSMakeRect(24,125,432,24);
        [content addSubview:delegate.contactsAccess];
        [delegate refreshContactsStatus];
        [NSTimer scheduledTimerWithTimeInterval:1 repeats:YES block:^(NSTimer *timer) {
            [delegate refreshContactsStatus];
        }];
        NSButton *privacy = [NSButton buttonWithTitle:@"Open Full Disk Access" target:delegate action:@selector(openPrivacy:)];
        privacy.frame = NSMakeRect(20,65,215,32);
        [content addSubview:privacy];
        NSButton *contacts = [NSButton buttonWithTitle:@"Contacts Settings" target:delegate action:@selector(openContacts:)];
        contacts.frame = NSMakeRect(250,65,205,32);
        [content addSubview:contacts];
        NSTextField *note = [NSTextField labelWithString:@"Contacts names are optional. You can keep using raw handles."];
        note.font = [NSFont systemFontOfSize:11];
        note.textColor = [NSColor secondaryLabelColor];
        note.frame = NSMakeRect(24,24,432,20);
        [content addSubview:note];
        return 1;
    }
}

void nw_app_run(void) { @autoreleasepool { [NSApp run]; } }
void nw_app_stop(void) { dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; }); }
void nw_app_update(const char *title, const char *detail, int messagesAvailable) {
    @autoreleasepool {
        NSString *t = [NSString stringWithUTF8String:title];
        NSString *d = [NSString stringWithUTF8String:detail];
        dispatch_async(dispatch_get_main_queue(), ^{
            delegate.status.stringValue = t;
            delegate.detail.stringValue = d;
            delegate.messagesAccess.stringValue = messagesAvailable ? @"✓ Full Disk Access: Messages readable" : @"○ Full Disk Access: Messages unavailable";
            delegate.messagesAccess.textColor = messagesAvailable ? [NSColor systemGreenColor] : [NSColor secondaryLabelColor];
        });
    }
}
