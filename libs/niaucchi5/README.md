# niaucchi5 --- an obfuscated, encrypted multiplexed unreliable channel

## Overview of architecture

Niaucchi5 provides an interface similar to that of QUIC, except that _streams are unreliable_. There's optional support for reliable streams based on a simple TCP-like protocol.

This maps neatly to Geph: proxy mode uses reliable streams, while VPN mode uses a single unreliable stream.

Niaucchi5 is a richly layered protocol:

```
|-----------------------|
|        streams        |
|-----------------------|
|  encryption protocol  |
|-----------------------|
| wire | wire | wire ...|
|-----------------------|
| obfs | obfs | obfs ...|
|-----------------------|
```

We take a bottom-down approach, specifying the lower layers before the upper layers.

##
