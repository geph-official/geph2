## Tech support / 客戶服務

Please contact us at contact@geph.io or use the [official Telegram group](https://t.me/joinchat/Pc6C1hMBREf-8_TZM5z6_g) for tech support questions about the Geph service. **Use GitHub only for bug reports!**

「客戶服務」請給我們發郵件（contact@geph.io）或使用[官方 Telegram 群](https://t.me/joinchat/Pc6C1hMBREf-8_TZM5z6_g)。 **GitHub 請用於錯誤報告，而不是一切迷霧通相關的討論。**

## Official repository for Geph daemons

This repository contains the source code for the platform-independent, headless daemons `geph-client`, `geph-binder`, `geph-exit`, and `geph-bridge`.

- `geph-client` is a command-line Geph client, and it's is embedded in the official Geph app.
- `geph-binder` is a command-and-control server that manages user authentication and assigns users to both bridges and exits
- `geph-exit` runs on highly secure exit nodes such as `us-sfo-01.exits.geph.io`, and handles exit traffic.
- `geph-bridge` runs on bridge nodes, which relay client-to-exit encrypted traffic across harsh firewalls. For bridges, we often use untrusted infrastructure such as random VPS providers that have good connections to China, since they never handle any sensitive information.

The GUI, along with build scripts for creating the desktop app, is found at https://github.com/geph-official/gephgui.

The Android app is at https://github.com/geph-official/geph-android. It is a hybrid app embedding a compiled version of "gephgui"

To prevent confusion, **all bug reports should be posted on this repository**, even if the issue appears to be platform-specific.
