import AppKit
import Combine
import Foundation
import NatterwireCore
import SwiftUI

enum APIPhase: Equatable, Sendable {
    case starting
    case running
    case failed(reason: String, messagesAccessRequired: Bool)
}

@MainActor
final class AppState: ObservableObject {
    @Published var phase: APIPhase = .starting
    @Published var notice: String?

    let localURL = "http://127.0.0.1:8741"
    let tailnetURL = "https://mac.example.ts.net:8741"

    var title: String {
        switch phase {
        case .starting: "Starting…"
        case .running: "API running"
        case .failed(_, let required): required ? "Messages access required" : "API unavailable"
        }
    }

    var detail: String {
        switch phase {
        case .starting: "Opening the Messages database"
        case .running: "Available on this Mac and your tailnet"
        case .failed(let reason, let required): required ? "Allow Full Disk Access, then retry." : reason
        }
    }

    var messagesAccessRequired: Bool {
        if case .failed(_, let required) = phase { return required }
        return false
    }
}

final class ServerRunner: @unchecked Sendable {
    private let lock = NSLock()
    private let queue = DispatchQueue(label: "com.shardul.natterwire.server", qos: .utility)
    private var active = false
    private var server: HTTPServer?

    func start(update: @escaping @Sendable (APIPhase) -> Void) {
        lock.lock()
        guard !active else { lock.unlock(); return }
        active = true
        lock.unlock()
        update(.starting)

        queue.async { [self] in
            do {
                let environment = ProcessInfo.processInfo.environment
                let token = try TokenConfiguration.load(environment: environment)
                let configuredPath = environment["NATTERWIRE_DB_PATH"]
                    ?? environment["MESSAGES_DB_PATH"]
                    ?? "~/Library/Messages/chat.db"
                let path = NSString(string: configuredPath).expandingTildeInPath
                let database = try MessagesDatabase(path: path)
                let api = NatterwireAPI(database: database, token: token)
                let server = HTTPServer(host: "127.0.0.1", port: 8741, api: api)
                lock.lock(); self.server = server; lock.unlock()
                try server.run { update(.running) }
                finish()
            } catch {
                finish()
                let message = String(describing: error).lowercased()
                if message.contains("authorization denied") || message.contains("operation not permitted") {
                    update(.failed(reason: "Messages access required", messagesAccessRequired: true))
                } else if message.contains("address already in use") {
                    update(.failed(reason: "Port 8741 is already in use", messagesAccessRequired: false))
                } else if error is TokenConfigurationError {
                    update(.failed(reason: "Token configuration needs attention", messagesAccessRequired: false))
                } else {
                    update(.failed(reason: "Could not start the local API", messagesAccessRequired: false))
                }
                FileHandle.standardError.write(Data("natterwire: API startup failed\n".utf8))
            }
        }
    }

    func stop() {
        lock.lock()
        let current = server
        lock.unlock()
        current?.stop()
    }

    private func finish() {
        lock.lock()
        server = nil
        active = false
        lock.unlock()
    }
}

@main
@MainActor
struct NatterwireMain {
    static func main() {
        let application = NSApplication.shared
        let delegate = AppDelegate()
        application.delegate = delegate
        application.run()
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let state = AppState()
    private let runner = ServerRunner()
    private var statusItem: NSStatusItem!
    private var popover: NSPopover!
    private var observation: AnyCancellable?

    func applicationDidFinishLaunching(_ notification: Notification) {
        if let existing = NSRunningApplication.runningApplications(
            withBundleIdentifier: "com.shardul.natterwire"
        ).first(where: { $0.processIdentifier != ProcessInfo.processInfo.processIdentifier }) {
            existing.activate()
            NSApp.terminate(nil)
            return
        }
        NSApp.setActivationPolicy(.accessory)
        configureStatusItem()
        configurePopover()
        observation = state.objectWillChange.sink { [weak self] _ in
            DispatchQueue.main.async { self?.updateStatusItem() }
        }

        let environment = ProcessInfo.processInfo.environment
        if let preview = environment["NATTERWIRE_PREVIEW_STATUS"] {
            state.phase = preview == "running"
                ? .running
                : .failed(reason: "Messages access required", messagesAccessRequired: true)
        } else {
            startServer()
        }
        updateStatusItem()
        configureCaptureIfNeeded()
    }

    func applicationWillTerminate(_ notification: Notification) {
        runner.stop()
    }

    private func startServer() {
        state.notice = nil
        runner.start { [weak state] phase in
            Task { @MainActor in state?.phase = phase }
        }
    }

    private func configureStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        statusItem.autosaveName = "Natterwire"
        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(togglePopover)
        button.sendAction(on: [.leftMouseUp])
    }

    private func configurePopover() {
        let popover = NSPopover()
        popover.behavior = .transient
        popover.animates = true
        popover.contentSize = NSSize(width: 320, height: 294)
        popover.contentViewController = NSHostingController(rootView: NatterwirePopoverView(
            state: state,
            copyURL: copyURL,
            openFullDiskAccess: openFullDiskAccess,
            retry: startServer,
            quit: quit))
        self.popover = popover
    }

    private func updateStatusItem() {
        guard let button = statusItem?.button else { return }
        let symbol: String
        switch state.phase {
        case .starting: symbol = "ellipsis.circle"
        case .running: symbol = "point.3.connected.trianglepath.dotted"
        case .failed: symbol = "exclamationmark.triangle"
        }
        guard let symbolImage = NSImage(systemSymbolName: symbol, accessibilityDescription: "Natterwire: \(state.title)")
            ?? NSImage(systemSymbolName: "network", accessibilityDescription: "Natterwire: \(state.title)")
        else { return }
        symbolImage.isTemplate = true
        symbolImage.size = NSSize(width: 16, height: 16)
        let image = NSImage(size: symbolImage.size, flipped: false) { rect in
            symbolImage.draw(in: rect.offsetBy(dx: 0, dy: 0.5))
            return true
        }
        image.isTemplate = true
        button.image = image
        button.toolTip = "Natterwire — \(state.title)"
        button.setAccessibilityLabel("Natterwire: \(state.title)")
    }

    @objc private func togglePopover() {
        guard let button = statusItem.button else { return }
        if popover.isShown {
            popover.performClose(nil)
        } else {
            let view = popover.contentViewController!.view
            popover.contentSize = NSSize(width: 320, height: max(190, view.fittingSize.height))
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        }
    }

    private func copyURL(_ value: String, name: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
        state.notice = "Copied \(name) URL"
    }

    private func openFullDiskAccess() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles") else { return }
        NSWorkspace.shared.open(url)
    }

    private func quit() {
        runner.stop()
        NSApp.terminate(nil)
    }

    private func configureCaptureIfNeeded() {
        guard let directory = ProcessInfo.processInfo.environment["NATTERWIRE_CAPTURE_DIR"] else { return }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { [weak self] in
            self?.togglePopover()
            self?.popover.contentViewController?.view.window?.sharingType = .readOnly
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) {
                guard let self else { return }
                try? FileManager.default.createDirectory(atPath: directory, withIntermediateDirectories: true)
                self.capture(self.popover.contentViewController!.view, to: "\(directory)/popover.png")
                if let button = self.statusItem.button {
                    self.capture(button, to: "\(directory)/menu-icon.png", scale: 4)
                }
                NSApp.terminate(nil)
            }
        }
    }

    private func capture(_ view: NSView, to path: String, scale: CGFloat? = nil) {
        view.layoutSubtreeIfNeeded()
        view.displayIfNeeded()
        let representation: NSBitmapImageRep?
        if let scale {
            representation = NSBitmapImageRep(
                bitmapDataPlanes: nil,
                pixelsWide: Int(view.bounds.width * scale),
                pixelsHigh: Int(view.bounds.height * scale),
                bitsPerSample: 8,
                samplesPerPixel: 4,
                hasAlpha: true,
                isPlanar: false,
                colorSpaceName: .deviceRGB,
                bytesPerRow: 0,
                bitsPerPixel: 0)
            representation?.size = view.bounds.size
        } else {
            representation = view.bitmapImageRepForCachingDisplay(in: view.bounds)
        }
        guard let representation else { return }
        view.cacheDisplay(in: view.bounds, to: representation)
        guard let data = representation.representation(using: .png, properties: [:]) else { return }
        try? data.write(to: URL(fileURLWithPath: path), options: .atomic)
    }
}
