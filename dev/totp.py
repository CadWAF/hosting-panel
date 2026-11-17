#!/usr/bin/env python3
"""Current TOTP code for the dev admin secret (dev only)."""
import base64, hmac, hashlib, struct, time, sys
s = sys.argv[1] if len(sys.argv) > 1 else "JBSWY3DPEHPK3PXP"
k = base64.b32decode(s)
c = struct.pack(">Q", int(time.time()) // 30)
h = hmac.new(k, c, hashlib.sha1).digest()
o = h[-1] & 15
print("%06d" % ((struct.unpack(">I", h[o:o+4])[0] & 0x7fffffff) % 1000000))
