## 更改

以下简述对迷雾通功能的一些调整

1.加入前置代理(upstream proxy)支持，目的是为了在迷雾通网桥失效时，可以通过不安全的机场服务器连接安全的出口服务器。前置代理须是一个SOCKS5代理(如Shadowsocks/如ShadowsocksR)。这里的出口服务器可以是迷雾通运营者提供的（以下简称为官方服务器），也可以是您自建的，此选项均有效。当使用前置代理时，所有的流量均从前置代理转发，客户端不会尝试通过迷雾通自身的网桥(geph-bridge)进行流量转发。

2.部分网络情况下，网桥被封时不能切换到紧急通道，因此加入开关，打开后强制启用紧急通道(warpfront)，若不打开(默认不打开)，则在连接网桥失败时自动启用。

3.允许添加额外网桥(additional bridge)的支持。用户可以在命令行中添加网桥。

4.允许私有网桥，该选项只在使用官方服务器时起效，目的是当使用迷雾通官方服务时，在不降低安全性前提下提高服务的可靠性。公开网桥通过迷雾通授权服务器(binder)获得，可能被GFW屏蔽。私有网桥是指不能通过迷雾通的授权服务器获取的网桥，它不能被GFW通过穷举检测到并封锁。私有网桥可以运行在不可靠的境外服务器上，如阿里云香港；也可以运行在境内某台电脑上，程序支持通过SS/SSR等SOCKS5代理连接出口服务器。私有网桥也可以与客户端(geph-client)运行在同一台机器上。自建模式下此选项无效，若要通过代理连接自建服务器，请使用前置代理(upstream proxy)。私有网桥使用时，作为额外网桥添加至命令列表中。

**请将下面的 星号 改成你的用户名和密码。**

正常使用（未指定exitName exitKey参数，此时使用美国出口服务器）：

geph-client.exe -username *** -password ***

正常使用（指定加拿大出口服务器）：

geph-client.exe -username *** -password *** -exitName ca-mtl-01.exits.geph.io -exitKey 8d848fe736ead2f0b78cd13e81293944936fe840ea449b8f87438340ecdaaaf6

强制打开紧急通道（日内瓦服务器）：

geph-client.exe -username *** -password *** -exitName ch-gva-01.exits.geph.io -exitKey c1b74b5d47286d97dd6a56ec574488775210ca7e44da506c011b17764660a34a -forceWarpfront

**前置代理的使用方法：**

启用前置代理，通过机场连接迷雾通香港服务器（SS/SSR代理为本机SOCKS5 1080端口）：

geph-client.exe -username *** -password *** -upstreamProxy 127.0.0.1:1080 -exitName hk-hkg-01.exits.geph.io -exitKey 816802fd21f8689897c2abf33a95db23cc2a8d4f5cb996a29a3d85a4919c86b8

启用前置代理，通过机场连接迷雾通日本服务器（SS/SSR代理为本机SOCKS5 1080端口）：

geph-client.exe -username *** -password *** -upstreamProxy 127.0.0.1:1080 -exitName jp-tyo-01.exits.geph.io -exitKey 107b64be61eef80a863362b84c7ebc730f81e903697c6e632f2908a62a60163d

启用前置代理，通过机场连接自建服务器（机场代理为本机SOCKS5 1080端口）：

geph-client -singleHop server_key@ip:port -upstreamProxy 127.0.0.1:1080

注意，使用迷雾通官方服务器时，如果要指定出口节点，请指定exitName exitKey两个参数，参数名前需要短横线-，参数值与下一个参数名之间需要有一个空格

exitName是迷雾通出口服务器的地址，exitKey是出口服务器的公钥，用于服务器身份校验。

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

**额外网桥的使用方法：**

额外网桥的一个目的是开发测试时使用，另外额外网桥可以是私有的，也就是说，你可以搭建私有网桥，并作为额外的网桥告诉迷雾通客户端，迷雾通客户端在通过官方服务器获取网桥后，会把你告诉它的网桥添加到既有网桥列表之中。私有网桥无法被GFW枚举并封锁，这可以用来提高迷雾通的可靠性。网桥是迷雾通客户端用于连接出口服务器所用，数量不嫌少，只要有一个网桥可以连通出口服务器，那么迷雾通服务就可以正常使用。

示例1：
你是迷雾通用户，同时还在使用机场，希望利用机场提高迷雾通的可靠性。即使迷雾通遭受GFW攻击影响速度，你依然可以通过机场快速连接。

你的机器运行着SSR，SS/SSR默认监听1080端口。

在这种情况下，私有网桥充当迷雾通客户端与机场连接的媒介，迷雾通客户端、私有网桥、机场客户端运行在同一台机器上。

运行私有网桥：

geph-privatebridge.exe -bridgeKey *** -listenAddr 127.0.0.1:32541 -backproxy 127.0.0.1:1080

*** 是网桥密钥，若要连接此网桥，客户端必须拥有此密钥。是十六进制数，形如fcada3e2033e188f4f99b3c3db12495b8b8cc7af41755742810c71e95b4bd1e8

该密钥可以用 geph-privatebridge -generatecookie 生成。

运行客户端并连接至日本服务器：

geph-client.exe -username *** -password *** -exitName jp-tyo-01.exits.geph.io -exitKey 107b64be61eef80a863362b84c7ebc730f81e903697c6e632f2908a62a60163d -privatebridge ***@127.0.0.1:32541

***疑问：既然迷雾通自带的网桥无法使用时可以使用前置代理连接出口服务器，为何还需要私有网桥？***


***回答：私有网桥主要是针对部分特殊情况，最典型的是在软路由上使用迷雾通，搭建透明代理局域网共享翻墙，此时当然希望连接尽可能稳定，尽可能减少维护的次数，甚至不便于维护，例如你在外地求学/工作，父母在老家不懂怎么弄。除此以外，私有网桥允许多个机场同时作为网桥大大提高了可靠性。而前置代理模式所有流量都走同一个代理，如果这个机场失效了，那所有流量都走不通。***


示例2：
你是迷雾通用户，同时有一台境外VPS，希望利用此VPS提高迷雾通的可靠性。

假设VPS的IP为1.2.3.4。在VPS上运行geph-privatebridge，监听1578端口。

geph-privatebridge.exe -bridgeKey *** -listenAddr :1578

*** 是网桥密钥，若要连接此网桥，客户端必须拥有此密钥。是十六进制数，形如fcada3e2033e188f4f99b3c3db12495b8b8cc7af41755742810c71e95b4bd1e8

该密钥可以用 geph-privatebridge -generatecookie 生成。

客户端使用方法：

geph-client.exe -username *** -password *** -exitName jp-tyo-01.exits.geph.io -exitKey 107b64be61eef80a863362b84c7ebc730f81e903697c6e632f2908a62a60163d -privatebridge ***@1.2.3.4:1578


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
