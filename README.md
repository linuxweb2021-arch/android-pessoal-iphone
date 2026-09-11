# PNHX — Android pessoal no iPhone

Aplicativo nativo para acessar um único Android persistente hospedado na VPS pelo iPhone. A API autentica o usuário e autoriza uma sessão; o gateway roda na mesma VPS do Android e envia H.264/Opus diretamente ao iPhone por WebRTC. O controle usa um DataChannel confiável e o protocolo binário do scrcpy 3.3.4.

## Estado

- API Go/SQLite com Argon2id, JWT curto, refresh rotativo, rate limit e uma sessão ativa.
- Sinalização WebSocket autenticada, com uma conexão de cliente e uma de executor.
- Gateway Go/Pion com H.264, Opus, ICE configurável e integração scrcpy 3.3.4.
- Cliente SwiftUI/UIKit para iOS 16+, vídeo proporcional, áudio, teclado por clipboard e multitoque.
- GitHub Actions para testar Go, compilar Swift e gerar uma IPA `arm64` sem assinatura.
- ReDroid Android 14 fixado por digest, persistente e restrito ao loopback na VPS, com uma camada mínima e reproduzível da Google Play para x86_64.

O Android 14 base já inicializa na VPS. O build macOS gera uma IPA `arm64` válida sem assinatura. Os testes no iPhone, áudio, apps e metas de latência só podem ser aprovados após a instalação e a integração física.

A imagem Google Play é gerada na VPS por `scripts/build-redroid-gapps.sh`. O script verifica o SHA-256 do pacote antes de criar `local/redroid:14-gapps-minimal-20250330`; credenciais Google e dados do Android permanecem somente no volume privado da VPS.

## Componentes

```text
iPhone ── HTTPS/WSS ── VPS: API Go/SQLite
   └──────── WebRTC direto/TURN ── VPS: Gateway Go/Pion ── scrcpy ── ReDroid
```

O fechamento da IPA encerra a transmissão e preserva o Android. A API e o gateway não oferecem shell remoto ou ADB público.

## Testes locais

Use Go 1.27.1:

```powershell
go test ./...
go vet ./...
go build ./cmd/api ./cmd/gateway
```

O workflow Linux também executa `go test -race ./...`.

## API

Copie `.env.example` para `.env`, substitua os três segredos e inicie:

```powershell
go run ./cmd/api
```

Rotas externas: `/v1/login`, `/v1/refresh`, `/v1/logout`, `/v1/device`, `/v1/sessions` e o WebSocket de sinalização. As rotas do executor exigem uma credencial diferente da senha do usuário. Em produção, exponha somente a API por HTTPS; a configuração padrão escuta em `127.0.0.1:18080`.

## Gateway e Android

Requisitos do executor:

- Android 11 ou posterior, com ADB privado e autorizado;
- aceleração gráfica e codec H.264 comprovados no dispositivo;
- `adb` no PATH ou em `ANDROID_ADB_PATH`;
- servidor scrcpy 3.3.4 verificado.

Baixe o servidor fixado:

```powershell
./scripts/download-scrcpy-server.ps1
```

O script verifica SHA-256 `8588238c9a5a00aa542906b6ec7e6d5541d9ffb9b5d0f6e1bc0e365e2303079e`. Configure `ANDROID_API_BASE_URL`, `ANDROID_EXECUTOR_TOKEN`, `ANDROID_SCRCPY_SERVER_PATH`, o serial ADB e ICE/TURN. Depois execute:

```powershell
go run ./cmd/gateway
```

Os perfis são aplicados no início da sessão:

| Perfil | Lado máximo | FPS | Bitrate |
|---|---:|---:|---:|
| Economia | 960 | 30 | 2,5 Mbps |
| Equilibrado | 1280 | 60 | 6 Mbps |
| Qualidade | 1920 | 60 | 10 Mbps |

## IPA pelo GitHub

O workflow `.github/workflows/ci.yml` usa `macos-15`, Xcode 16.4, XcodeGen 2.46.0 e WebRTC 152.0.0. O artifact contém:

- `PNHX.ipa`;
- `PNHX.ipa.sha256`;
- `manifest.json` com bundle, versão, arquitetura e frameworks.

A IPA não possui assinatura. Faça a assinatura e instalação local pelo Sideloadly. Nenhum Apple ID, certificado, senha, chave SSH ou segredo de servidor entra no repositório ou workflow.

## Segurança operacional

- Mantenha o repositório privado e o arquivo `.env` fora do Git.
- Use HTTPS/WSS com certificado válido antes do acesso pelo iPhone.
- Configure credenciais TURN temporárias e uma implantação separada dos serviços existentes.
- Mantenha ADB e a porta local do scrcpy inacessíveis pela internet.
- O logout e a exclusão de sessão revogam a sessão ativa e fecham a sinalização.

## Referências fixadas

- scrcpy `v3.3.4`, código no commit `fb6381f5b9bb96f3fa823d899f4c32de2ec84ab3` e protocolo sem compatibilidade entre versões;
- Pion WebRTC `v4.2.20` no `go.sum`;
- pacote comunitário `stasel/WebRTC` `152.0.0`, checksum Swift Package Manager `115cb9944248a3302c0c8af17462e2576a28ccc7adef9f6a1fe66ee75d9e1cc8`.
