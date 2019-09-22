# niaucchi4: the 3rd generation Geph obfuscation protocol

## Architecture

Unlike previous iterations, `niaucchi4` presents an unreliable transport to upper layers. This allows it to work around environments where TCP sessions are unreliable (mobile network switching etc), letting hours-long upper-layer sessions become practical. It can also use multiple UDP sessions, automatically converging on the best path.

```
[Authentication, encryption]
[        niaucchi4         ]
[  UDP  ][  UDP   ][  UDP  ]
```

Like previous versions, though, intermediate hosts are still identified by `host:port` and `cookie`.

## Opening a tunnel

To open a new tunnel the client sends the intermediary a packet like the following from a fresh source port:

```
[32-byte nonce][encrypted pubkey || rhost:rport][arbitrary garbage]
```

The ed25519 pubkey can either be encrypted with chacha20-poly1305 or aes256-gcm. In either case the encryption nonce is null, the tag precedes the ciphertext, and the key is `HMAC-SHA256(cookie, nonce)`.

The server should respond with a similar message. A shared secret `sharedsec` is derived.

## Tunnel wire format

The tunnel wire format is simply RLP:

```
[body padding]
```

encrypted with `HMAC-SHA256(sharedsec, "up")` or `"down"` depending on the direction. Undecodeable packets should be silently discarded and can be used as padding.

An empty-padding packet is to be considered a ping packet, and should be returned to the sender unchanged. This allows measuring latency. Ping packets should be created at a randomly sampled size.

## Rekeying

Every `2^16` packets we send, we rekey by setting `sharedsec = SHA256(sharedsec)` and recompute the sending key. This ensures that we can safely use AEADs with short nonces. When receivers get an undecryptable packet, they try to decode it with the "next" key too.

## What does the intermediary do

The intermediary keeps a NAT-like table of incoming tunnels by `host:port` of the client, mapping them to tunnel state structures and outgoing UDP sockets. The tunnels are cleared only under memory pressure and should not be timed out unless absolutely necessary.

## End-to-end wire format

The end-to-end format is simply a 64-bit sessid and then the body.