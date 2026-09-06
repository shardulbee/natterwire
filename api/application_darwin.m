//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <Contacts/Contacts.h>

@interface NatterwireDelegate : NSObject <NSApplicationDelegate>
@property(strong) NSWindow *window;
@property(strong) NSStatusItem *statusItem;
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
- (void)openWindow:(id)sender { [self showWindow]; }
- (void)applicationDidFinishLaunching:(NSNotification *)note { [self showWindow]; }
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app { return NO; }
- (void)refreshContactsStatus {
    if (![[NSBundle mainBundle] objectForInfoDictionaryKey:@"NSContactsUsageDescription"]) {
        self.contactsAccess.stringValue = @"○ Contacts access unavailable";
        return;
    }
    CNAuthorizationStatus status = [CNContactStore authorizationStatusForEntityType:CNEntityTypeContacts];
    BOOL allowed = status != CNAuthorizationStatusNotDetermined && status != CNAuthorizationStatusDenied && status != CNAuthorizationStatusRestricted;
    self.contactsAccess.stringValue = allowed ? @"✓ Contacts access available" : @"○ Contacts access unavailable";
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

// Fox profile from the app artwork, simplified for an 18-point template image.
static NSImage *statusIcon(void) {
    NSImage *image = [NSImage imageWithSize:NSMakeSize(18,18) flipped:NO drawingHandler:^BOOL(NSRect rect) {
        NSBezierPath *fox = [NSBezierPath bezierPath];
        [fox moveToPoint:NSMakePoint(2,17)];
        [fox lineToPoint:NSMakePoint(7,13)];
        [fox lineToPoint:NSMakePoint(12,17)];
        [fox lineToPoint:NSMakePoint(11,11)];
        [fox lineToPoint:NSMakePoint(14,8)];
        [fox lineToPoint:NSMakePoint(17,7)];
        [fox curveToPoint:NSMakePoint(11,4) controlPoint1:NSMakePoint(16,4) controlPoint2:NSMakePoint(13,4)];
        [fox lineToPoint:NSMakePoint(7,1)];
        [fox curveToPoint:NSMakePoint(2,9) controlPoint1:NSMakePoint(3,2) controlPoint2:NSMakePoint(1,5)];
        [fox closePath];
        [fox appendBezierPathWithOvalInRect:NSMakeRect(10,8,1.5,1.5)];
        fox.windingRule = NSEvenOddWindingRule;
        [[NSColor blackColor] setFill];
        [fox fill];
        return YES;
    }];
    image.template = YES;
    return image;
}

static NSTextField *permissionRow(NSView *content, NSString *title, CGFloat y, SEL action) {
    NSTextField *label = [NSTextField labelWithString:title];
    label.font = [NSFont systemFontOfSize:13 weight:NSFontWeightSemibold];
    label.frame = NSMakeRect(40,y+36,280,20);
    [content addSubview:label];
    NSTextField *status = [NSTextField labelWithString:@"○ Checking access…"];
    status.font = [NSFont systemFontOfSize:12];
    status.textColor = [NSColor secondaryLabelColor];
    status.frame = NSMakeRect(40,y+13,280,20);
    [content addSubview:status];
    NSButton *settings = [NSButton buttonWithTitle:@"Settings…" target:delegate action:action];
    settings.accessibilityLabel = [title stringByAppendingString:@" settings"];
    settings.frame = NSMakeRect(336,y+20,104,28);
    [content addSubview:settings];
    return status;
}

int nw_app_prepare(void) {
    @autoreleasepool {
        if (![[[NSBundle mainBundle] objectForInfoDictionaryKey:@"CFBundlePackageType"] isEqual:@"APPL"]) return 0;
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        delegate = [NatterwireDelegate new];
        NSApp.delegate = delegate;
        delegate.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
        delegate.statusItem.button.image = statusIcon();
        delegate.statusItem.button.toolTip = @"Natterwire — Open status and permissions";
        delegate.statusItem.button.accessibilityLabel = @"Natterwire status and permissions";
        delegate.statusItem.button.target = delegate;
        delegate.statusItem.button.action = @selector(openWindow:);
        NSMenu *menu = [NSMenu new];
        NSMenuItem *item = [NSMenuItem new];
        [menu addItem:item];
        NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Natterwire"];
        [appMenu addItemWithTitle:@"Quit Natterwire" action:@selector(terminate:) keyEquivalent:@"q"];
        item.submenu = appMenu;
        NSApp.mainMenu = menu;

        delegate.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,480,350)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable
            backing:NSBackingStoreBuffered defer:NO];
        delegate.window.title = @"Natterwire";
        delegate.window.releasedWhenClosed = NO;
        [delegate.window center];
        NSView *content = delegate.window.contentView;
        NSImageView *icon = [[NSImageView alloc] initWithFrame:NSMakeRect(24,270,48,48)];
        icon.image = [NSApp applicationIconImage];
        [content addSubview:icon];
        delegate.status = [NSTextField labelWithString:@"Starting Natterwire"];
        delegate.status.font = [NSFont boldSystemFontOfSize:20];
        delegate.status.frame = NSMakeRect(88,294,368,26);
        [content addSubview:delegate.status];
        delegate.detail = [NSTextField wrappingLabelWithString:@"Checking access. Contacts access is optional."];
        delegate.detail.textColor = [NSColor secondaryLabelColor];
        delegate.detail.frame = NSMakeRect(88,244,368,42);
        [content addSubview:delegate.detail];
        delegate.messagesAccess = permissionRow(content, @"Full Disk Access", 155, @selector(openPrivacy:));
        delegate.contactsAccess = permissionRow(content, @"Contacts", 79, @selector(openContacts:));
        [delegate refreshContactsStatus];
        [NSTimer scheduledTimerWithTimeInterval:1 repeats:YES block:^(NSTimer *timer) {
            [delegate refreshContactsStatus];
        }];
        NSTextField *note = [NSTextField wrappingLabelWithString:@"Closing this window leaves the API running.\nClick the menu bar fox to reopen it."];
        note.font = [NSFont systemFontOfSize:11];
        note.textColor = [NSColor secondaryLabelColor];
        note.frame = NSMakeRect(24,20,292,36);
        [content addSubview:note];
        NSButton *quit = [NSButton buttonWithTitle:@"Quit Natterwire" target:NSApp action:@selector(terminate:)];
        quit.keyEquivalent = @"q";
        quit.keyEquivalentModifierMask = NSEventModifierFlagCommand;
        quit.frame = NSMakeRect(324,24,132,28);
        [content addSubview:quit];
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
            delegate.messagesAccess.stringValue = messagesAvailable ? @"✓ Messages access available" : @"○ Messages access unavailable";
            delegate.messagesAccess.textColor = messagesAvailable ? [NSColor systemGreenColor] : [NSColor secondaryLabelColor];
        });
    }
}
