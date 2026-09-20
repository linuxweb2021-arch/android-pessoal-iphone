import SwiftUI

struct RemoteSessionView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingKeyboard = false
    @State private var textToSend = ""
    @State private var showDiagnostic = true

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.ignoresSafeArea()
            if let client = model.webRTC {
                RemoteVideoView(client: client)
                    .ignoresSafeArea(edges: .horizontal)
            } else {
                ProgressView("Preparando sessão…")
            }
            HStack(spacing: 18) {
                Button { model.webRTC?.sendAndroidKey("back") } label: { Image(systemName: "chevron.backward") }
                Button { model.webRTC?.sendAndroidKey("home") } label: { Image(systemName: "circle") }
                Button { model.webRTC?.sendAndroidKey("recents") } label: { Image(systemName: "square.on.square") }
                Spacer()
                Button { showingKeyboard = true } label: { Image(systemName: "keyboard") }
                Image(systemName: "slider.horizontal.3")
                    .accessibilityLabel("Perfil \(model.quality.rawValue)")
                Button { model.muted.toggle(); model.webRTC?.setMuted(model.muted) } label: {
                    Image(systemName: model.muted ? "speaker.slash.fill" : "speaker.wave.2.fill")
                }
                Button(role: .destructive) { Task { await model.disconnect() } } label: { Image(systemName: "xmark.circle.fill") }
            }
            .font(.title3)
            .padding(.horizontal, 20)
            .padding(.vertical, 12)
            .background(.ultraThinMaterial, in: Capsule())
            .padding(.bottom, 8)
        }
        .overlay(alignment: .top) {
            if showDiagnostic {
                Text(model.diagnostic)
                    .font(.caption)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(.ultraThinMaterial, in: Capsule())
                    .padding(.top, 6)
                    .onTapGesture { showDiagnostic = false }
            }
        }
        .onChange(of: model.diagnostic) { _, newValue in
            if newValue == "Conectado" {
                // Hide the pill shortly after connecting so video is
                // unobstructed. It reappears on any status change.
                Task {
                    try? await Task.sleep(for: .seconds(3))
                    showDiagnostic = false
                }
            } else {
                showDiagnostic = true
            }
        }
        .overlay {
            if case .failed(let message) = model.phase {
                VStack(spacing: 14) {
                    Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                    Text(message).multilineTextAlignment(.center)
                    Button("Voltar") { Task { await model.disconnect() } }
                        .buttonStyle(.borderedProminent)
                }
                .padding(24)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 18))
                .padding(28)
            }
        }
        .sheet(isPresented: $showingKeyboard) {
            NavigationStack {
                Form {
                    TextField("Texto, acentos e emoji", text: $textToSend, axis: .vertical)
                        .lineLimit(3...8)
                }
                .navigationTitle("Enviar texto")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancelar") { showingKeyboard = false } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Colar no Android") {
                            model.webRTC?.sendText(textToSend)
                            textToSend = ""
                            showingKeyboard = false
                        }
                        .disabled(textToSend.isEmpty)
                    }
                }
            }
            .presentationDetents([.medium])
        }
    }
}
