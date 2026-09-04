import SwiftUI

struct NatterwirePopoverView: View {
    @ObservedObject var state: AppState
    let copyURL: (String, String) -> Void
    let openFullDiskAccess: () -> Void
    let retry: () -> Void
    let quit: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 9) {
                Image(systemName: statusSymbol)
                    .renderingMode(.template)
                    .foregroundStyle(statusColor)
                    .frame(width: 18, height: 18)
                VStack(alignment: .leading, spacing: 3) {
                    Text("Natterwire")
                        .font(.system(size: 14, weight: .semibold))
                    Text(state.title)
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(statusColor)
                }
                Spacer()
            }
            Text(state.detail)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .padding(.top, 7)
                .padding(.bottom, 13)

            Divider()

            VStack(alignment: .leading, spacing: 12) {
                endpoint("Local", display: state.localURL, value: state.localURL)
                endpoint(
                    "Tailnet",
                    display: "mac.example.ts.net:8741",
                    value: state.tailnetURL)
            }
            .padding(.vertical, 14)

            if state.messagesAccessRequired {
                Button(action: openFullDiskAccess) {
                    Label("Open Full Disk Access…", systemImage: "lock.open")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.regular)
                .focusEffectDisabled()
                .padding(.bottom, 12)
            }

            if let notice = state.notice {
                Label(notice, systemImage: "doc.on.clipboard")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 10)
            }

            Divider()

            HStack(spacing: 12) {
                if case .failed = state.phase {
                    Button("Retry", action: retry)
                        .buttonStyle(.plain)
                        .focusEffectDisabled()
                }
                Spacer()
                Button("Quit", action: quit)
                    .buttonStyle(.plain)
                    .focusEffectDisabled()
            }
            .font(.system(size: 12))
            .padding(.top, 12)
        }
        .padding(16)
        .frame(width: 320)
        .background(.regularMaterial)
    }

    private func endpoint(_ name: String, display: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(name)
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(.secondary)
            HStack(spacing: 8) {
                Text(display)
                    .font(.system(size: 11, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                Spacer(minLength: 4)
                Button { copyURL(value, name) } label: {
                    Image(systemName: "doc.on.doc")
                }
                .buttonStyle(.borderless)
                .focusEffectDisabled()
                .focusable(false)
                .help("Copy \(name) URL")
                .accessibilityLabel("Copy \(name) URL")
            }
        }
    }

    private var statusSymbol: String {
        switch state.phase {
        case .starting: "ellipsis.circle"
        case .running: "checkmark.circle.fill"
        case .failed: "exclamationmark.triangle.fill"
        }
    }

    private var statusColor: Color {
        switch state.phase {
        case .starting: .secondary
        case .running: .green
        case .failed: .orange
        }
    }
}
