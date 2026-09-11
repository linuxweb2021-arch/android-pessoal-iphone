import SwiftUI

@main
struct AndroidPessoalApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(model)
        }
    }
}
struct RootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Group {
            switch model.phase {
            case .signedOut:
                LoginView()
            case .connecting, .connected, .failed:
                RemoteSessionView()
            }
        }
        .preferredColorScheme(.dark)
    }
}
