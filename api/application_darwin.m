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
- (void)quitFromStatusMenu:(id)sender { [NSApp terminate:sender]; }
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
    label.frame = NSMakeRect(24,y+36,244,20);
    [content addSubview:label];
    NSTextField *status = [NSTextField labelWithString:@"○ Checking access…"];
    status.font = [NSFont systemFontOfSize:12];
    status.textColor = [NSColor secondaryLabelColor];
    status.frame = NSMakeRect(24,y+13,244,20);
    [content addSubview:status];
    NSButton *settings = [NSButton buttonWithTitle:@"Settings…" target:delegate action:action];
    settings.accessibilityLabel = [title stringByAppendingString:@" settings"];
    settings.frame = NSMakeRect(272,y+20,104,28);
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
        delegate.statusItem.button.toolTip = @"Natterwire";
        delegate.statusItem.button.accessibilityLabel = @"Natterwire";
        NSMenu *statusMenu = [[NSMenu alloc] initWithTitle:@"Natterwire"];
        [statusMenu addItemWithTitle:@"Open Natterwire" action:@selector(openWindow:) keyEquivalent:@""].target = delegate;
        [statusMenu addItem:[NSMenuItem separatorItem]];
        // AppKit adds a Quit icon to terminate:; keep only this menu item text-only.
        [statusMenu addItemWithTitle:@"Quit Natterwire" action:@selector(quitFromStatusMenu:) keyEquivalent:@"q"].target = delegate;
        delegate.statusItem.menu = statusMenu;
        NSMenu *menu = [NSMenu new];
        NSMenuItem *item = [NSMenuItem new];
        [menu addItem:item];
        NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Natterwire"];
        [appMenu addItemWithTitle:@"Quit Natterwire" action:@selector(terminate:) keyEquivalent:@"q"];
        item.submenu = appMenu;
        NSApp.mainMenu = menu;

        delegate.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,400,224)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable
            backing:NSBackingStoreBuffered defer:NO];
        delegate.window.title = @"Natterwire";
        delegate.window.releasedWhenClosed = NO;
        [delegate.window center];
        NSView *content = delegate.window.contentView;
        NSImageView *icon = [[NSImageView alloc] initWithFrame:NSMakeRect(24,152,48,48)];
        icon.image = [NSApp applicationIconImage];
        icon.autoresizingMask = NSViewMinYMargin;
        [content addSubview:icon];
        delegate.status = [NSTextField labelWithString:@"Starting…"];
        delegate.status.font = [NSFont boldSystemFontOfSize:20];
        delegate.status.frame = NSMakeRect(88,164,288,26);
        delegate.status.autoresizingMask = NSViewMinYMargin;
        [content addSubview:delegate.status];
        delegate.detail = [NSTextField wrappingLabelWithString:@""];
        delegate.detail.textColor = [NSColor secondaryLabelColor];
        delegate.detail.frame = NSMakeRect(24,144,352,40);
        delegate.detail.hidden = YES;
        [content addSubview:delegate.detail];
        delegate.messagesAccess = permissionRow(content, @"Full Disk Access", 76, @selector(openPrivacy:));
        NSBox *separator = [[NSBox alloc] initWithFrame:NSMakeRect(24,76,352,1)];
        separator.boxType = NSBoxSeparator;
        [content addSubview:separator];
        delegate.contactsAccess = permissionRow(content, @"Contacts", 8, @selector(openContacts:));
        [delegate refreshContactsStatus];
        [NSTimer scheduledTimerWithTimeInterval:1 repeats:YES block:^(NSTimer *timer) {
            [delegate refreshContactsStatus];
        }];
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
            delegate.detail.hidden = d.length == 0;
            // Keep the header at the top and permission rows below any error detail.
            NSRect frame = delegate.window.frame;
            CGFloat height = [delegate.window frameRectForContentRect:NSMakeRect(0,0,400,d.length ? 272 : 224)].size.height;
            frame.origin.y += frame.size.height - height;
            frame.size.height = height;
            [delegate.window setFrame:frame display:YES];
            delegate.messagesAccess.stringValue = messagesAvailable ? @"✓ Messages access available" : @"○ Messages access unavailable";
            delegate.messagesAccess.textColor = messagesAvailable ? [NSColor systemGreenColor] : [NSColor secondaryLabelColor];
        });
    }
}
