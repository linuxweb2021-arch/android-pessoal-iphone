import SwiftUI

struct LoginView: View {
    @EnvironmentObject private var model: AppModel
    @State private var password = ""
    @State private var showingSettings = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 22) {
                Spacer()
                Image(systemName: "iphone.gen3.radiowaves.left.and.right")
                    .font(.system(size: 58))
                    .foregroundStyle(.cyan)
                Text("Android Pessoal")
                    .font(.largeTitle.bold())
                Text("Acesse seu Android persistente pelo iPhone.")
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                TextField("Usuário", text: $model.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .textContentType(.username)
                    .textFieldStyle(.roundedBorder)
                SecureField("Senha", text: $password)
                    .textContentType(.password)
                    .textFieldStyle(.roundedBorder)
                Picker("Qualidade", selection: $model.quality) {
                    ForEach(StreamQuality.allCases) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                Button {
                    Task { await model.signIn(password: password) }
                } label: {
                    Text("Entrar")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(password.isEmpty)
                Button("Configurar servidor") { showingSettings = true }
                    .buttonStyle(.borderless)
                Spacer()
            }
            .padding(28)
            .sheet(isPresented: $showingSettings) {
                NavigationStack {
                    Form { TextField("https://…", text: $model.serverURL).textInputAutocapitalization(.never).autocorrectionDisabled() }
                        .navigationTitle("Servidor")
                        .toolbar { Button("Concluir") { showingSettings = false } }
                }
            }
        }
    }
}
