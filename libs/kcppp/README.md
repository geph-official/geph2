# KCP++: KCP but better (NOT FINISHED!)

## Why?

KCP is pretty well known, but:

- It's really poorly documented
- `kcp-go`'s code quality is appalling
- It's a massive layering violation

KCP++ is a simplified, improved, and partially backwards-compatible reliable transport protocol on top of UDP or any other unreliable datagram transport. Some features include:

- Well-tuned bandwidth-probing congestion control based on BBR.
- Only handle reliable transport. ECC, encryption, etc belong on different layers.
- Better performance in really lossy environments through SACK.
- RST mechanism to terminate dead connections without timeout.

## Packet header

- **4 bytes**: conversation ID. Not really important except as a sanity check.
- **1 byte**: command. One of
  - PUSH (81)
  - ACK (82)
  - WASK (83)
  - WINS (84)
  - SACK (91)
  - RST (0)
- **1 byte**: _Reserved, must be 0_
- **2 bytes**: receive window advertisement (in packets)
- **4 bytes**: timestamp in ms
- **4 bytes**: segment number
- **4 bytes**: acknowledgement number
