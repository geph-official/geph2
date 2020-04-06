## 更改

1.命令行指令加入前置代理支持，可以通过机场连接迷雾通出口服务器。
2.是否走紧急通道由用户指定，否则不走。
请将下面的 星号 改成你的用户名和密码。

正常使用（未指定节点则为美国节点）：

geph-client.exe -username *** -password ***



紧急通道（日内瓦节点）：

geph-client.exe -username *** -password *** -exitName ch-gva-01.exits.geph.io -exitKey c1b74b5d47286d97dd6a56ec574488775210ca7e44da506c011b17764660a34a -forcefrontdomain

通过机场连接迷雾通香港服务器（机场代理为本机SOCKS5 1080端口）：

geph-client.exe -username *** -password *** -frontProxy 127.0.0.1:1080 -exitName hk-hkg-01.exits.geph.io -exitKey 816802fd21f8689897c2abf33a95db23cc2a8d4f5cb996a29a3d85a4919c86b8

通过机场连接自建服务器（机场代理为本机SOCKS5 1080端口）：

geph-client -singleHop server_key@ip:port -frontProxy 127.0.0.1:1080

注意，非自建模式中，如果要指定出口节点，请指定exitName exitKey两个参数，每两个参数之间需要有一个空格
以下是迷雾通各个出口服务器的name和key：

新加坡
sg-sgp-01.exits.geph.io 2f1d296bcc56cdd84e7276ebea91f131d64a4021a385ee0f6f0ecbdffe8b2342

香港
hk-hkg-01.exits.geph.io 816802fd21f8689897c2abf33a95db23cc2a8d4f5cb996a29a3d85a4919c86b8

日本
jp-tyo-01.exits.geph.io 107b64be61eef80a863362b84c7ebc730f81e903697c6e632f2908a62a60163d

瑞士
ch-gva-01.exits.geph.io c1b74b5d47286d97dd6a56ec574488775210ca7e44da506c011b17764660a34a

加拿大
ca-mtl-01.exits.geph.io 8d848fe736ead2f0b78cd13e81293944936fe840ea449b8f87438340ecdaaaf6

美国
us-sfo-01.exits.geph.io 2f8571e4795032433098af285c0ce9e43c973ac3ad71bf178e4f2aaa39794aec


## Download mirror / 免「翻牆」下載鏡像

https://waa.ai/getmiwutong

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
