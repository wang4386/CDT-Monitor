# CDT Monitor Android 小组件

基于 CDT Monitor API 的原生 Android 客户端（Kotlin + Jetpack Glance + Compose），随主仓库保存，目前处于 dev 验证阶段，测试通过后再纳入正式发行。它只调用接受 `widget:read` 的只读接口：

```text
GET /api/v1/widget/summary            小组件紧凑数据
GET /api/v1/status                    App 内实例看板（流量 / IP / 账单 / 定时）
GET /api/v1/accounts/{id}/history     近 7 日已用流量趋势
Authorization: Bearer <API Key>
```

App 内看板与桌面小组件是同一个应用：打开 App 即可看到每个实例的完整信息（公网 IPv4/IPv6、本月费用、账户余额、定时开关机、保活状态与近 7 日已用流量条形图），下拉即可刷新。

## 功能

- **三种尺寸自适应**（Glance `SizeMode.Responsive`）：小尺寸显示所选实例的用量概览；中尺寸显示完整状态、进度条与刷新按钮；大尺寸（4×3 以上）列出全部实例，点按任意一行即可把该实例置为当前显示。
- **刷新机制**：WorkManager 每 30 分钟联网刷新，小组件上的刷新图标可立即触发；刷新失败时保留上次数据并标注「刷新失败，显示上次数据」，不会白屏。
- **设置与配置页**：Jetpack Compose + Material 3 界面，与小组件共用同一套配色（含深色模式）。API Key 留空表示沿用已保存的 Key。
- **凭据安全**：站点地址明文保存，API Key 使用 Android Keystore（AES/GCM）加密后保存。

小组件的布局参照 Google 官方 Canonical Widget Layouts，刷新与状态管理遵循官方 `glance-appwidget-updates-and-memory` 技能的 WorkManager + DataStore 状态推送模式。

## 使用方式

1. 在 CDT Monitor Web 控制台的“设置 → API Key”创建一个只包含 `widget:read` 的 Key。Key 只显示一次。
2. 安装应用并打开“CDT Monitor 小组件”，填写站点地址和 Key，点击“保存并测试连接”。
3. 在 Android 桌面添加“CDT Monitor 实例”小组件。
4. 在配置页选择要显示的实例，点击“添加小组件”。

站点地址可以是 `http://` 或 `https://`，但公网部署必须使用 HTTPS，避免 API Key 在网络中暴露。

## 本地构建

需要 JDK 17 与 Android SDK 35，仓库已带 Gradle Wrapper（Gradle 8.10.2）：

```bash
cd android-widget
./gradlew testDebugUnitTest assembleRelease bundleRelease
```

产物位于 `app/build/outputs/`：

- `apk/debug/*universal*.apk`：可直接安装的通用 debug APK。
- `apk/release/*universal*.apk`、`*arm64-v8a*.apk`、`*armeabi-v7a*.apk`、`*x86*.apk`：通用与常见 ABI 分包（未签名时文件名会带 `-unsigned`）。
- `bundle/release/app-release.aab`：Google Play 或其他支持 AAB 的发行渠道使用。

## GitHub Actions

`.github/workflows/android-widget.yml` 在以下情况触发：手动 `workflow_dispatch`，或向 `dev` 分支推送 `android-widget/**` 的变更。它会先跑单元测试，再构建 debug APK、release APK、四种 ABI 分包以及 AAB，并将它们作为 workflow artifact 上传。构建不依赖 API Key，也不会把任何站点凭据写入仓库。

未配置签名密钥时，debug APK 使用 Android 调试签名，可以直接安装；release APK/AAB 是未签名发行产物。正式分发和后续覆盖升级需要在仓库 Actions Secrets 中配置：

- `ANDROID_KEYSTORE_BASE64`：JKS/PKCS12 文件的 Base64 内容。
- `ANDROID_KEYSTORE_PASSWORD`：keystore 密码。
- `ANDROID_KEY_ALIAS`：签名 Key 的 alias。
- `ANDROID_KEY_PASSWORD`：签名 Key 密码。

配置后，Actions 会使用同一份 keystore 签署 release APK 和 AAB。不要把 keystore 或密码提交到仓库。
